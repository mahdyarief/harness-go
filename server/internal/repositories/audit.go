package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

// Audit action names. They are stable identifiers, not display copy, so a
// dashboard or export can group by them without parsing prose.
const (
	ActionTenantCreated        = "tenant.created"
	ActionTenantMemberAdded    = "tenant.member_assigned"
	ActionTokenTopUp           = "token.topup"
	ActionLLMConfigUpdated     = "llm_config.updated"
	ActionExaConfigUpdated     = "exa_config.updated"
	ActionUserCreated          = "user.created"
	ActionUserUpdated          = "user.updated"
	ActionUserRoleChanged      = "user.role_changed"
	ActionUserPasswordChanged  = "user.password_changed"
	ActionKnowledgeUploaded    = "knowledge.uploaded"
	ActionKnowledgeDeleted     = "knowledge.deleted"
	ActionKnowledgePublished   = "knowledge.published"
	ActionKnowledgeUnpublished = "knowledge.unpublished"
)

// AuditRepository reads and appends the audit trail. Every write is an INSERT:
// the table's trigger rejects UPDATE/DELETE/TRUNCATE, so there is no update or
// delete method here by design.
//
// It is built over the non-owner TenantDB like ChatRepository, because
// audit_logs is RLS-forced and the owner connection would bypass the policy.
type AuditRepository struct{ db *sql.DB }

func NewAuditRepository(db *sql.DB) *AuditRepository { return &AuditRepository{db: db} }

// AuditFilters narrows the trail. Nil UUID pointers mean "any". From/To are
// already validated date or RFC3339 strings from the DTO layer.
type AuditFilters struct {
	ListParams
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	ActorID    *uuid.UUID
	TenantID   *uuid.UUID
	From       string
	To         string
}

const auditColumns = `a.id, a.occurred_at, a.actor_id, a.actor_role, COALESCE(u.name, ''),
	a.action, a.entity_type, a.entity_id, a.tenant_id, a.request_id, a.ip,
	a.user_agent, a.metadata`

// auditSource is shared by List and Count so both filter over the same rows.
// The users join only supplies the actor's display name; it never widens scope.
const auditSource = `audit_logs a LEFT JOIN users u ON u.id = a.actor_id`

var auditSortColumns = map[string]string{
	"occurred_at": "a.occurred_at",
	"action":      "a.action",
	"entity_type": "a.entity_type",
	"actor_role":  "a.actor_role",
	"id":          "a.id",
}

// auditMaxMetadataBytes matches the CHECK on audit_logs.metadata. An entry that
// would exceed it is replaced with a marker rather than truncated, because a
// half-written JSON object is not valid jsonb.
const auditMaxMetadataBytes = 8192

// Insert appends one entry inside the caller's transaction, so the audit row
// commits or rolls back with the mutation it records. Callers are responsible
// for having set app.user_id (and app.tenant_id for tenant-scoped actions);
// the RLS insert policy verifies both.
func (r *AuditRepository) Insert(ctx context.Context, tx *sql.Tx, entry models.AuditLog) error {
	if r == nil {
		return apperr.ErrForbidden
	}
	return insertAudit(ctx, tx, entry)
}

// insertAudit is the package-level form used by the other repositories. They
// hold no AuditRepository reference, and audit writes must ride in their
// existing transaction rather than open a second connection.
func insertAudit(ctx context.Context, tx *sql.Tx, entry models.AuditLog) error {
	if tx == nil || entry.ActorID == uuid.Nil {
		return apperr.ErrForbidden
	}
	if entry.Action == "" || entry.EntityType == "" {
		return apperr.New(apperr.KindBadRequest, "Aksi atau tipe entitas audit tidak valid")
	}
	// Callers that did not go through auditAppend still get request provenance
	// when the context came from an HTTP request.
	if meta := lib.RequestMetaFrom(ctx); meta.RequestID != "" || meta.IP != "" {
		if entry.RequestID == "" {
			entry.RequestID = meta.RequestID
		}
		if entry.IP == "" {
			entry.IP = meta.IP
		}
		if entry.UserAgent == "" {
			entry.UserAgent = meta.UserAgent
		}
	}
	metadata := entry.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if len(metadata) > auditMaxMetadataBytes {
		metadata = json.RawMessage(`{"truncated":true}`)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (actor_id, actor_role, action, entity_type, entity_id,
			tenant_id, request_id, ip, user_agent, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		entry.ActorID, clip(entry.ActorRole, 40), entry.Action, entry.EntityType,
		entry.EntityID, entry.TenantID, clip(entry.RequestID, 100), clip(entry.IP, 64),
		clip(entry.UserAgent, 512), metadata)
	return err
}

// auditAppend records a mutation with the request provenance already attached
// to ctx by middlewares.RequestMeta. Metadata is optional.
//
// A failure here aborts the caller's transaction on purpose: an audit trail
// that can silently lose rows is worse than a mutation that reports an error.
func auditAppend(ctx context.Context, tx *sql.Tx, actor uuid.UUID, actorRole, action, entityType string, entityID, tenantID *uuid.UUID, metadata map[string]any) error {
	entry := models.AuditLog{
		ActorID:    actor,
		ActorRole:  actorRole,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		TenantID:   tenantID,
	}
	meta := lib.RequestMetaFrom(ctx)
	entry.RequestID = meta.RequestID
	entry.IP = meta.IP
	entry.UserAgent = meta.UserAgent
	if len(metadata) > 0 {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		entry.Metadata = raw
	}
	return insertAudit(ctx, tx, entry)
}

// clip shortens s to at most n runes, keeping the column CHECK from failing on
// an oversized user agent or forwarded header.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Record appends an entry in its own transaction. Prefer Insert when the write
// belongs with a mutation; Record exists for actions that have no other row to
// share a transaction with, and is therefore not atomic with them.
func (r *AuditRepository) Record(ctx context.Context, actor uuid.UUID, entry models.AuditLog) error {
	entry.ActorID = actor
	tx, err := r.beginPlatform(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = r.Insert(ctx, tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordAction appends one entry in its own transaction, for mutations whose
// own write does not share a pool with audit_logs (the Authula user mirror and
// the LLM singleton run on the owner connection). It re-proves the super-user
// role before inserting.
func (r *AuditRepository) RecordAction(ctx context.Context, actor uuid.UUID, actorRole, action, entityType string, entityID, tenantID *uuid.UUID, metadata map[string]any) error {
	entry := models.AuditLog{
		ActorID:    actor,
		ActorRole:  actorRole,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		TenantID:   tenantID,
	}
	meta := lib.RequestMetaFrom(ctx)
	entry.RequestID = meta.RequestID
	entry.IP = meta.IP
	entry.UserAgent = meta.UserAgent
	if len(metadata) > 0 {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		entry.Metadata = raw
	}
	return r.Record(ctx, actor, entry)
}

// List returns one page of the trail. The route is super-user only, but the
// repository re-proves the role inside the transaction so hiding the route is
// not the security boundary.
func (r *AuditRepository) List(ctx context.Context, actor uuid.UUID, f AuditFilters) ([]models.AuditLog, error) {
	tx, err := r.beginPlatform(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	where, args, limit, offset := auditFilters(f).page(f.ListParams)
	query := fmt.Sprintf(`
		SELECT %s FROM %s
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		auditColumns, auditSource, where,
		resolveOrder(f.Sorted, auditSortColumns, "a.occurred_at DESC, a.id DESC"), limit, offset)

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result, err := collectRows(rows, scanAuditLog)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Count returns the total matching rows for the list metadata.
func (r *AuditRepository) Count(ctx context.Context, actor uuid.UUID, f AuditFilters) (int64, error) {
	tx, err := r.beginPlatform(ctx, actor)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	w := auditFilters(f)
	var total int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s`, auditSource, strings.Join(w.conds, " AND "))
	if err = tx.QueryRowContext(ctx, query, w.args...).Scan(&total); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

// Get returns one entry by id, or ErrNotFound when it is hidden from the
// session.
func (r *AuditRepository) Get(ctx context.Context, actor, id uuid.UUID) (models.AuditLog, error) {
	var entry models.AuditLog
	tx, err := r.beginPlatform(ctx, actor)
	if err != nil {
		return entry, err
	}
	defer tx.Rollback()

	query := fmt.Sprintf(`SELECT %s FROM %s WHERE a.id = $1`, auditColumns, auditSource)
	entry, err = scanAuditLog(tx.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return entry, apperr.ErrNotFound
	}
	if err != nil {
		return entry, err
	}
	if err = tx.Commit(); err != nil {
		return entry, err
	}
	return entry, nil
}

func auditFilters(f AuditFilters) *whereBuilder {
	w := newWhere("TRUE")
	if f.Action != "" {
		w.add("a.action = $%d", f.Action)
	}
	if f.EntityType != "" {
		w.add("a.entity_type = $%d", f.EntityType)
	}
	if f.EntityID != nil {
		w.add("a.entity_id = $%d", *f.EntityID)
	}
	if f.ActorID != nil {
		w.add("a.actor_id = $%d", *f.ActorID)
	}
	if f.TenantID != nil {
		w.add("a.tenant_id = $%d", *f.TenantID)
	}
	if f.From != "" {
		w.add("a.occurred_at >= $%d::timestamptz", f.From)
	}
	if f.To != "" {
		// A plain date means "through the end of that day"; a full timestamp is
		// treated as an inclusive upper bound.
		if len(f.To) == 10 {
			w.add("a.occurred_at < ($%d::date + interval '1 day')", f.To)
		} else {
			w.add("a.occurred_at <= $%d::timestamptz", f.To)
		}
	}
	w.search(f.Search, "a.action", "a.entity_type", "a.request_id", "a.ip", "u.name")
	return w
}

func scanAuditLog(row rowScanner) (models.AuditLog, error) {
	var entry models.AuditLog
	err := row.Scan(&entry.ID, &entry.OccurredAt, &entry.ActorID, &entry.ActorRole,
		&entry.ActorName, &entry.Action, &entry.EntityType, &entry.EntityID,
		&entry.TenantID, &entry.RequestID, &entry.IP, &entry.UserAgent, &entry.Metadata)
	return entry, err
}

// beginPlatform opens a transaction pinned to the actor with no tenant, then
// proves the super-user role inside it. Clearing app.tenant_id keeps a platform
// read from silently inheriting one tenant's context.
func (r *AuditRepository) beginPlatform(ctx context.Context, actor uuid.UUID) (*sql.Tx, error) {
	if r == nil || r.db == nil || actor == uuid.Nil {
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
	_, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true), set_config('app.user_id',$1,true)`, actor.String())
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err = platformAdmin(ctx, tx, actor); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
