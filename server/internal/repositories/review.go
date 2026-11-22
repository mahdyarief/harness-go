package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"hsg/internal/harness/review"
	"hsg/internal/harness/schema"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
)

// ReviewRepository persists the review inbox: items and their decision
// history. Every method pins the caller's tenant, so RLS scopes an item to
// its tenant; the (tenant, run, subject) unique index makes a re-run replace
// the open item instead of duplicating it.
type ReviewRepository struct{ db *sql.DB }

func NewReviewRepository(db *sql.DB) *ReviewRepository {
	return &ReviewRepository{db: db}
}

func (r *ReviewRepository) begin(ctx context.Context, tenant uuid.UUID) (*sql.Tx, error) {
	if r == nil || r.db == nil || tenant == uuid.Nil {
		return nil, apperr.ErrForbidden
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	unsafe, err := TenantRoleUnsafe(ctx, tx)
	if err != nil || unsafe {
		tx.Rollback()
		if err != nil {
			return nil, err
		}
		return nil, apperr.ErrForbidden
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (r *ReviewRepository) beginTenant(ctx context.Context) (*sql.Tx, uuid.UUID, error) {
	tenant, ok := lib.TenantFrom(ctx)
	if !ok || tenant == uuid.Nil {
		return nil, uuid.Nil, apperr.ErrForbidden
	}
	tx, err := r.begin(ctx, tenant)
	return tx, tenant, err
}

// Create inserts an item. When an open item for the same (tenant, run,
// subject) exists it is replaced, so a re-run does not duplicate it.
func (r *ReviewRepository) Create(ctx context.Context, item review.Item) (review.Item, error) {
	tx, _, err := r.beginTenant(ctx)
	if err != nil {
		return review.Item{}, err
	}
	defer tx.Rollback()

	if item.ID == "" {
		item.ID = "rev-" + uuid.New().String()
	}
	if item.Status == "" {
		item.Status = review.StatusOpen
	}
	evidenceJSON, err := json.Marshal(item.Evidence)
	if err != nil {
		return review.Item{}, err
	}
	var schemaJSON any
	if len(item.Schema) > 0 {
		schemaJSON = []byte(item.Schema)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO review_items (id, run_id, subject, result, schema, evidence, reason, required_role, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, run_id, subject) DO UPDATE SET
			result = EXCLUDED.result, schema = EXCLUDED.schema, evidence = EXCLUDED.evidence,
			reason = EXCLUDED.reason, required_role = EXCLUDED.required_role, status = EXCLUDED.status
	`, item.ID, item.RunID, item.Subject, []byte(item.Result), schemaJSON, evidenceJSON, item.Reason, item.Role, item.Status)
	if err != nil {
		return review.Item{}, err
	}
	if err = tx.Commit(); err != nil {
		return review.Item{}, err
	}
	return item, nil
}

// Get loads one item with its full decision history, ordered by time.
func (r *ReviewRepository) Get(ctx context.Context, id string) (review.Item, error) {
	tx, _, err := r.beginTenant(ctx)
	if err != nil {
		return review.Item{}, err
	}
	defer tx.Rollback()
	return scanItem(ctx, tx, id)
}

// List returns items with the given status (empty = all) newest first.
func (r *ReviewRepository) List(ctx context.Context, status string, limit, offset int) ([]review.Item, int, error) {
	tx, _, err := r.beginTenant(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	var total int
	countQuery := `SELECT count(*) FROM review_items`
	args := []any{}
	if status != "" {
		countQuery += ` WHERE status = $1`
		args = append(args, status)
	}
	if err = tx.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listQuery := `SELECT id FROM review_items`
	if status != "" {
		listQuery += ` WHERE status = $1`
	}
	listQuery += ` ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := tx.QueryContext(ctx, listQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	items := make([]review.Item, 0, len(ids))
	for _, id := range ids {
		item, err := scanItem(ctx, tx, id)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, nil
}

// Decide appends the decision to the item's history and closes it. The role
// must be at least the item's required role and the result must pass the
// item's schema, mirroring the memory store's rules.
func (r *ReviewRepository) Decide(ctx context.Context, id string, decision review.Decision) (review.Item, error) {
	tx, _, err := r.beginTenant(ctx)
	if err != nil {
		return review.Item{}, err
	}
	defer tx.Rollback()

	item, err := scanItem(ctx, tx, id)
	if err != nil {
		return review.Item{}, err
	}
	if item.Status != review.StatusOpen {
		return review.Item{}, review.ErrConflict
	}
	if review.RoleRank[decision.Role] < review.RoleRank[item.Role] {
		return review.Item{}, review.ErrForbidden
	}
	if item.Schema != nil {
		compiled, err := schema.Compile(item.Schema)
		if err != nil {
			return review.Item{}, err
		}
		if len(compiled.Validate(decision.Result)) > 0 {
			return review.Item{}, review.ErrSchema
		}
	}
	overrides, err := json.Marshal(decision.Overrides)
	if err != nil {
		return review.Item{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO review_decisions (item_id, reviewer, role, result, overrides, reason)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, id, decision.Reviewer, decision.Role, []byte(decision.Result), overrides, decision.Reason); err != nil {
		return review.Item{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE review_items SET status = $1 WHERE id = $2`, review.StatusDecided, id); err != nil {
		return review.Item{}, err
	}
	if err = tx.Commit(); err != nil {
		return review.Item{}, err
	}
	fresh, _, err := r.beginTenant(ctx)
	if err != nil {
		return review.Item{}, err
	}
	defer fresh.Rollback()
	return scanItem(ctx, fresh, id)
}

// Await polls the item until it is decided or the context ends. The database
// store has no push channel; a short poll is bounded by the caller's timeout.
func (r *ReviewRepository) Await(ctx context.Context, id string) (review.Decision, error) {
	for {
		item, err := r.Get(ctx, id)
		if err != nil {
			return review.Decision{}, err
		}
		if item.Status == review.StatusDecided && len(item.Decisions) > 0 {
			return item.Decisions[len(item.Decisions)-1], nil
		}
		select {
		case <-ctx.Done():
			return review.Decision{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func scanItem(ctx context.Context, tx *sql.Tx, id string) (review.Item, error) {
	var item review.Item
	var evidenceJSON []byte
	var schemaJSON []byte
	err := tx.QueryRowContext(ctx, `
		SELECT id, run_id, subject, result, schema, evidence, reason, required_role, status, created_at
		FROM review_items WHERE id = $1
	`, id).Scan(&item.ID, &item.RunID, &item.Subject, &item.Result, &schemaJSON, &evidenceJSON, &item.Reason, &item.Role, &item.Status, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return review.Item{}, review.ErrNotFound
	}
	if err != nil {
		return review.Item{}, err
	}
	if schemaJSON != nil {
		item.Schema = schemaJSON
	}
	if len(evidenceJSON) > 0 {
		_ = json.Unmarshal(evidenceJSON, &item.Evidence)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT reviewer, role, result, overrides, reason, at
		FROM review_decisions WHERE item_id = $1 ORDER BY at, id
	`, id)
	if err != nil {
		return review.Item{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var decision review.Decision
		var overrides []byte
		if err = rows.Scan(&decision.Reviewer, &decision.Role, &decision.Result, &overrides, &decision.Reason, &decision.At); err != nil {
			return review.Item{}, err
		}
		if len(overrides) > 0 {
			_ = json.Unmarshal(overrides, &decision.Overrides)
		}
		item.Decisions = append(item.Decisions, decision)
	}
	return item, rows.Err()
}
