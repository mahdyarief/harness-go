package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

// TokenRepository owns the tenant token pool: the balance row, the append-only
// top-up ledger, and the append-only usage ledger.
//
// Every method runs inside one transaction that proves the actor first:
//   - a platform admin acts with app.user_id set and no tenant context, so the
//     platform-admin policies let it read every tenant and write top-ups;
//   - a tenant member acts with both app.user_id and app.tenant_id pinned to a
//     tenant it demonstrably belongs to, so the tenant policies scope it.
//
// Nothing here trusts a tenant_id from the client: the tenant is always derived
// from membership (tenant scope) or validated as an existing tenant (platform).
type TokenRepository struct{ db *sql.DB }

func NewTokenRepository(db *sql.DB) *TokenRepository { return &TokenRepository{db: db} }

const tokenBalanceColumns = `tenant_id,balance,updated_at`

// platformBegin opens a platform-admin transaction. It sets only app.user_id;
// app.tenant_id stays empty so tenant policies cannot match, which keeps the
// platform flows from silently reading one tenant as if it were their own.
func (r *TokenRepository) platformBegin(ctx context.Context, actor uuid.UUID) (*sql.Tx, error) {
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

// tenantBegin opens a tenant-scoped transaction for a member of tenant.
func (r *TokenRepository) tenantBegin(ctx context.Context, actor, tenant uuid.UUID) (*sql.Tx, error) {
	return r.tenantBeginRole(ctx, actor, tenant, false)
}

// tenantBeginRole is tenantBegin with an optional tenant_admin role check.
func (r *TokenRepository) tenantBeginRole(ctx context.Context, actor, tenant uuid.UUID, admin bool) (*sql.Tx, error) {
	if r == nil || r.db == nil || actor == uuid.Nil || tenant == uuid.Nil {
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
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true)`, actor.String()); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err = verified(ctx, tx, actor, tenant, admin); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// readBegin opens a read transaction for actor on tenant, choosing the access
// path by role: a platform admin reads any tenant through the platform policy,
// while a tenant member must prove membership (and, for tenant_admin-only
// views, the tenant_admin role) and then reads under the tenant policy.
func (r *TokenRepository) readBegin(ctx context.Context, actor, tenant uuid.UUID, tenantAdmin bool) (*sql.Tx, error) {
	admin, err := r.isPlatformAdmin(ctx, actor)
	if err != nil {
		return nil, err
	}
	if admin {
		return r.platformBegin(ctx, actor)
	}
	return r.tenantBeginRole(ctx, actor, tenant, tenantAdmin)
}

// isPlatformAdmin reads the actor's application role. It runs before any
// tenant context is set so it does not depend on either policy.
func (r *TokenRepository) isPlatformAdmin(ctx context.Context, actor uuid.UUID) (bool, error) {
	if actor == uuid.Nil {
		return false, apperr.ErrForbidden
	}
	var role string
	err := r.db.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1`, actor).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, apperr.ErrForbidden
	}
	if err != nil {
		return false, err
	}
	return role == "super_user", nil
}

// Balance reads one tenant's balance. actor may be a platform admin (any
// tenant) or a member of that tenant.
func (r *TokenRepository) Balance(ctx context.Context, actor, tenant uuid.UUID) (models.TokenBalance, error) {
	var b models.TokenBalance
	tx, err := r.readBegin(ctx, actor, tenant, false)
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	b, err = r.readBalance(ctx, tx, tenant)
	if err != nil {
		return b, err
	}
	return b, tx.Commit()
}

// readBalance is shared by both access paths; the caller owns the transaction.
func (r *TokenRepository) readBalance(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) (models.TokenBalance, error) {
	var b models.TokenBalance
	err := tx.QueryRowContext(ctx,
		`SELECT `+tokenBalanceColumns+` FROM tenant_token_balances WHERE tenant_id=$1`, tenant).
		Scan(&b.TenantID, &b.Balance, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// A tenant that has never been granted tokens has a zero balance, which
		// is exactly what the hard-block check needs to see.
		b.TenantID = tenant
		return b, nil
	}
	return b, err
}

// Balances lists every tenant with its current balance, for the platform
// allocation table. Tenants without a balance row appear with zero.
func (r *TokenRepository) Balances(ctx context.Context, actor uuid.UUID) ([]models.TokenBalance, error) {
	tx, err := r.platformBegin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(b.balance, 0), COALESCE(b.updated_at, t.created_at)
		FROM tenants t
		LEFT JOIN tenant_token_balances b ON b.tenant_id = t.id
		ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]models.TokenBalance, 0)
	for rows.Next() {
		var b models.TokenBalance
		if err = rows.Scan(&b.TenantID, &b.TenantName, &b.Balance, &b.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Topups lists the additions for one tenant, newest first. Only a platform
// admin or that tenant's tenant_admin may read the top-up history.
func (r *TokenRepository) Topups(ctx context.Context, actor, tenant uuid.UUID, limit, offset int) ([]models.TokenTopup, error) {
	tx, err := r.readBegin(ctx, actor, tenant, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT t.id,t.tenant_id,t.amount,t.note,t.actor_id,u.name,u.email,t.reason,t.created_at
		FROM tenant_token_topups t
		JOIN users u ON u.id = t.actor_id
		WHERE t.tenant_id=$1
		ORDER BY t.created_at DESC, t.id DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]models.TokenTopup, 0)
	for rows.Next() {
		var t models.TokenTopup
		if err = rows.Scan(&t.ID, &t.TenantID, &t.Amount, &t.Note, &t.ActorID, &t.ActorName, &t.ActorEmail, &t.Reason, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// CountTopups is the total for the top-up history's pagination metadata.
func (r *TokenRepository) CountTopups(ctx context.Context, actor, tenant uuid.UUID) (int, error) {
	tx, err := r.readBegin(ctx, actor, tenant, true)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tenant_token_topups WHERE tenant_id=$1`, tenant).Scan(&total); err != nil {
		return 0, err
	}
	return total, tx.Commit()
}

// Usages lists consumption records for one tenant, newest first, with the
// visibility tier applied here rather than in the handler:
//   - a platform admin sees the whole tenant;
//   - a tenant_admin sees the whole tenant;
//   - a regular member sees only their own rows.
//
// The tier is decided from the database role/membership, not from anything the
// client sends, so widening the view is not a matter of request parameters.
func (r *TokenRepository) Usages(ctx context.Context, actor, tenant uuid.UUID, limit, offset int) ([]models.TokenUsage, error) {
	onlyUser, tx, err := r.usageScope(ctx, actor, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := `
		SELECT t.id,t.tenant_id,t.user_id,u.name,u.email,t.session_id,t.message_id,t.prompt_tokens,t.completion_tokens,t.total_tokens,t.billed_tokens,t.created_at
		FROM tenant_token_usages t
		JOIN users u ON u.id = t.user_id
		WHERE t.tenant_id=$1`
	args := []any{tenant}
	if onlyUser != nil {
		args = append(args, *onlyUser)
		query += fmt.Sprintf(` AND t.user_id=$%d`, len(args))
	}
	args = append(args, limit, offset)
	query += fmt.Sprintf(` ORDER BY t.created_at DESC, t.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]models.TokenUsage, 0)
	for rows.Next() {
		var u models.TokenUsage
		if err = rows.Scan(&u.ID, &u.TenantID, &u.UserID, &u.UserName, &u.UserEmail, &u.SessionID, &u.MessageID,
			&u.PromptTokens, &u.CompletionTokens, &u.TotalTokens, &u.BilledTokens, &u.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// CountUsages is the total for the usage history's pagination metadata. It
// applies the same visibility tier as Usages, so a regular member's count
// matches the rows they can actually see.
func (r *TokenRepository) CountUsages(ctx context.Context, actor, tenant uuid.UUID) (int, error) {
	onlyUser, tx, err := r.usageScope(ctx, actor, tenant)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	query := `SELECT COUNT(*) FROM tenant_token_usages WHERE tenant_id=$1`
	args := []any{tenant}
	if onlyUser != nil {
		args = append(args, *onlyUser)
		query += ` AND user_id=$2`
	}
	var total int
	if err = tx.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, tx.Commit()
}

// usageScope opens the read transaction and returns the user filter, if any.
func (r *TokenRepository) usageScope(ctx context.Context, actor, tenant uuid.UUID) (*uuid.UUID, *sql.Tx, error) {
	admin, err := r.isPlatformAdmin(ctx, actor)
	if err != nil {
		return nil, nil, err
	}
	if admin {
		tx, err := r.platformBegin(ctx, actor)
		return nil, tx, err
	}
	tx, err := r.tenantBegin(ctx, actor, tenant)
	if err != nil {
		return nil, nil, err
	}
	var role string
	if err = tx.QueryRowContext(ctx,
		`SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2`, tenant, actor).Scan(&role); err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	if role == "tenant_admin" {
		return nil, tx, nil
	}
	return &actor, tx, nil
}

// TopUp grants tokens to a tenant. The caller must be a platform admin; the
// tenant must exist, and the amount must be positive. The balance is upserted
// and the addition is recorded in the append-only ledger. The new balance is
// re-read so the response carries the authoritative server timestamp.
func (r *TokenRepository) TopUp(ctx context.Context, actor, tenant uuid.UUID, amount int64, note string) (models.TokenBalance, error) {
	var b models.TokenBalance
	if tenant == uuid.Nil || amount <= 0 {
		return b, apperr.New(apperr.KindBadRequest, "Penambahan token tidak valid")
	}
	tx, err := r.platformBegin(ctx, actor)
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	if err = requireTenant(ctx, tx, tenant); err != nil {
		return b, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return b, err
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO tenant_token_topups (id,tenant_id,amount,note,actor_id,reason)
		 VALUES ($1,$2,$3,$4,$5,'manual')`, id, tenant, amount, note, actor); err != nil {
		return b, err
	}
	// Upsert keeps a single row per tenant; the CHECK on balance rejects a
	// negative result, so this can never drive the balance below zero.
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO tenant_token_balances (tenant_id,balance) VALUES ($1,$2)
		 ON CONFLICT (tenant_id) DO UPDATE SET balance = tenant_token_balances.balance + EXCLUDED.balance, updated_at = now()`,
		tenant, amount); err != nil {
		return b, err
	}
	b, err = r.readBalance(ctx, tx, tenant)
	if err != nil {
		return b, err
	}
	if err = auditAppend(ctx, tx, actor, "super_user", ActionTokenTopUp, "tenant",
		&tenant, &tenant, map[string]any{"amount": amount, "balance_after": b.Balance, "note": note}); err != nil {
		return b, err
	}
	if err = tx.Commit(); err != nil {
		return b, err
	}
	return b, nil
}

// Spend deducts tokens for a completed chat turn and records the usage. The
// decrement runs under an advisory lock so two concurrent turns cannot both
// pass a pre-flight check and then overspend.
//
// The charge is clamped to the remaining balance: a turn that costs more than
// what is left drains the pool to exactly zero rather than going negative. The
// usage row keeps both the actual total and the billed amount, so the ledger
// stays exact while the request history stays transparent. A tenant with no
// balance row or a zero balance is not charged at all, and its rows still
// record the real total with billed_tokens = 0.
func (r *TokenRepository) Spend(ctx context.Context, actor, tenant uuid.UUID, u models.TokenUsage) error {
	if tenant == uuid.Nil || u.TotalTokens <= 0 {
		return nil
	}
	tx, err := r.tenantBegin(ctx, actor, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenant.String()); err != nil {
		return err
	}

	var remaining int64
	err = tx.QueryRowContext(ctx,
		`SELECT balance FROM tenant_token_balances WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&remaining)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	billed := int64(u.TotalTokens)
	if billed > remaining {
		billed = remaining
	}
	if remaining > 0 {
		if _, err = tx.ExecContext(ctx,
			`UPDATE tenant_token_balances SET balance = balance - $1, updated_at = now() WHERE tenant_id=$2`,
			billed, tenant); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO tenant_token_usages
		   (id,tenant_id,user_id,session_id,message_id,prompt_tokens,completion_tokens,total_tokens,billed_tokens)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, tenant, u.UserID, u.SessionID, u.MessageID, u.PromptTokens, u.CompletionTokens, u.TotalTokens, billed)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// CheckBalance is the pre-flight gate before any LLM call. It returns the
// available balance for a tenant the actor may see. The caller compares the
// estimated spend against it; a request with no headroom is rejected before any
// provider cost is incurred.
func (r *TokenRepository) CheckBalance(ctx context.Context, actor, tenant uuid.UUID) (int64, error) {
	b, err := r.Balance(ctx, actor, tenant)
	if err != nil {
		return 0, err
	}
	return b.Balance, nil
}

// UsageSince counts tokens spent since a time, for the summary tiles without
// loading the whole ledger. onlyUser narrows it to one member's own usage.
func (r *TokenRepository) UsageSince(ctx context.Context, actor, tenant uuid.UUID, onlyUser *uuid.UUID, since time.Time) (int64, int64, error) {
	tx, err := r.readBegin(ctx, actor, tenant, false)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	query := `SELECT COALESCE(SUM(total_tokens),0), COUNT(*) FROM tenant_token_usages
		WHERE tenant_id=$1 AND created_at >= $2`
	args := []any{tenant, since}
	if onlyUser != nil {
		args = append(args, *onlyUser)
		query += fmt.Sprintf(` AND user_id=$%d`, len(args))
	}
	var total, count int64
	if err = tx.QueryRowContext(ctx, query, args...).Scan(&total, &count); err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return total, count, nil
}
