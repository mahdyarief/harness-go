package repositories

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"hsg/internal/harness/services"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
)

// RunLogRepository persists run metadata and its append-only event stream.
// Every method pins the caller's tenant before touching the tables, so RLS
// scopes a run and its events to that tenant: a query without app.tenant_id
// set sees nothing rather than everything.
type RunLogRepository struct{ db *sql.DB }

func NewRunLogRepository(db *sql.DB) *RunLogRepository {
	return &RunLogRepository{db: db}
}

// parseUUID maps the log's string identifiers to uuid columns, treating an
// empty string as uuid.Nil (used for the optional parent run).
func parseUUID(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(s)
}

// begin opens a tenant-pinned transaction. The tenant must be a valid uuid;
// the membership check is left to the caller's auth layer, but the role guard
// fails closed on a super-user role that can bypass RLS.
func (r *RunLogRepository) begin(ctx context.Context, tenant uuid.UUID) (*sql.Tx, error) {
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

// beginRun pins the caller's tenant from the context. After pinning, the run
// row is readable only inside its own tenant (RLS), so a foreign runID is not
// found: an outsider gets ErrNotFound, not the run's existence.
func (r *RunLogRepository) beginRun(ctx context.Context, runID uuid.UUID) (*sql.Tx, error) {
	if r == nil || r.db == nil {
		return nil, apperr.ErrForbidden
	}
	tenant, ok := lib.TenantFrom(ctx)
	if !ok || tenant == uuid.Nil {
		return nil, apperr.ErrForbidden
	}
	tx, err := r.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM runs WHERE id = $1)`, runID).Scan(&exists)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if !exists {
		tx.Rollback()
		return nil, apperr.ErrNotFound
	}
	return tx, nil
}

func (r *RunLogRepository) CreateRun(ctx context.Context, run services.RunRecord) error {
	tenant, err := parseUUID(run.TenantID)
	if err != nil {
		return err
	}
	tx, err := r.begin(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := parseUUID(run.ID)
	if err != nil {
		return err
	}
	if id == uuid.Nil {
		id = uuid.New()
	}
	parent, err := parseUUID(run.ParentRunID)
	if err != nil {
		return err
	}
	var parentAny any
	if run.ParentRunID != "" {
		parentAny = parent
	}
	status := run.Status
	if status == "" {
		status = "running"
	}
	var startedAt any
	if !run.StartedAt.IsZero() {
		startedAt = run.StartedAt
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO runs (id, tenant_id, parent_run_id, profile_name, profile_hash, model, config_hash, status, started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, tenant, parentAny,
		run.ProfileName, run.ProfileHash, run.Model, run.ConfigHash,
		status, startedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *RunLogRepository) AppendEvent(ctx context.Context, event services.RunLogEvent) error {
	runID, err := parseUUID(event.RunID)
	if err != nil {
		return err
	}
	tx, err := r.beginRun(ctx, runID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Assign the next seq under the run row lock so concurrent appends stay
	// contiguous. The run must exist in this tenant (RLS hides foreign runs).
	var seq int64
	err = tx.QueryRowContext(ctx, `
		UPDATE runs SET event_seq = event_seq + 1 WHERE id = $1 RETURNING event_seq`, runID).Scan(&seq)
	if err != nil {
		return err
	}
	at := event.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO run_events (run_id, seq, member, type, payload, at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		runID, seq, event.Member, event.Type, event.Payload, at)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *RunLogRepository) FinishRun(ctx context.Context, runID string, completion services.RunCompletion) error {
	id, err := parseUUID(runID)
	if err != nil {
		return err
	}
	tx, err := r.beginRun(ctx, id)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ended := completion.EndedAt
	if ended.IsZero() {
		ended = time.Now().UTC()
	}
	status := completion.Status
	if status == "" {
		status = "finished"
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE runs SET status=$2, prompt_tokens=$3, completion_tokens=$4, cost_usd=$5, ended_at=$6 WHERE id=$1`,
		id, status, completion.PromptTokens, completion.CompletionTokens, completion.CostUSD, ended); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *RunLogRepository) Events(ctx context.Context, runID string) ([]services.RunLogEvent, error) {
	id, err := parseUUID(runID)
	if err != nil {
		return nil, err
	}
	tx, err := r.beginRun(ctx, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT seq, member, type, payload, at FROM run_events WHERE run_id = $1 ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]services.RunLogEvent, 0, 8)
	for rows.Next() {
		var event services.RunLogEvent
		var payload []byte
		if err = rows.Scan(&event.Seq, &event.Member, &event.Type, &payload, &event.At); err != nil {
			return nil, err
		}
		event.RunID = runID
		event.Payload = payload
		out = append(out, event)
	}
	return out, rows.Err()
}
