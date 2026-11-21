package repositories

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

type TenantRepository struct{ db *sql.DB }

func NewTenantRepository(db *sql.DB) *TenantRepository { return &TenantRepository{db: db} }

// begin checks effective privileges on every checkout, including inherited roles.
func (r *TenantRepository) begin(ctx context.Context, user uuid.UUID) (*sql.Tx, error) {
	if r == nil || r.db == nil || user == uuid.Nil {
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
	_, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', '', true), set_config('app.user_id', $1, true)`, user.String())
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
func setTenant(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, id.String())
	return err
}
func verified(ctx context.Context, tx *sql.Tx, user, tenant uuid.UUID, admin bool) error {
	var role string
	err := tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE user_id=$1 AND tenant_id=$2`, user, tenant).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrForbidden
	}
	if err != nil {
		return err
	}
	if admin && role != "tenant_admin" {
		return apperr.ErrForbidden
	}
	return setTenant(ctx, tx, tenant)
}
func (r *TenantRepository) List(ctx context.Context, user uuid.UUID) ([]models.Tenant, error) {
	tx, err := r.begin(ctx, user)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id FROM tenant_memberships WHERE user_id=$1 ORDER BY tenant_id`, user)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]models.Tenant, 0, len(ids))
	for _, id := range ids {
		if err = verified(ctx, tx, user, id, false); err != nil {
			return nil, err
		}
		var t models.Tenant
		err = tx.QueryRowContext(ctx, `SELECT t.id,t.name,t.created_at,m.role FROM tenants t JOIN tenant_memberships m ON m.tenant_id=t.id AND m.user_id=$2 WHERE t.id=$1`, id, user).Scan(&t.ID, &t.Name, &t.CreatedAt, &t.Role)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (r *TenantRepository) Members(ctx context.Context, user, tenant uuid.UUID) ([]models.TenantMember, error) {
	tx, err := r.begin(ctx, user)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = verified(ctx, tx, user, tenant, true); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT m.user_id, u.name, u.email, m.role, m.created_at
		FROM tenant_memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.tenant_id=$1
		ORDER BY u.email`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]models.TenantMember, 0)
	for rows.Next() {
		var m models.TenantMember
		if err = rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (r *TenantRepository) VerifyMembership(ctx context.Context, user, tenant uuid.UUID) error {
	tx, err := r.begin(ctx, user)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = verified(ctx, tx, user, tenant, false); err != nil {
		return err
	}
	return tx.Commit()
}

// AssignMember upserts exactly one existing user; it never changes users.role.
func (r *TenantRepository) AssignMember(ctx context.Context, actor, tenant, user uuid.UUID, role string) error {
	if user == uuid.Nil || (role != "tenant_admin" && role != "reviewer") {
		return apperr.New(apperr.KindBadRequest, "Pengguna atau peran tenant tidak valid")
	}
	tx, err := r.begin(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = verified(ctx, tx, actor, tenant, true); err != nil {
		return err
	}
	var locked uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrNotFound
	}
	if err != nil {
		return err
	}
	// Re-check after the lock: a concurrent admin may have demoted this actor.
	if err = verified(ctx, tx, actor, tenant, true); err != nil {
		return err
	}
	if err = existingUser(ctx, tx, user); err != nil {
		return err
	}
	var last bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2 AND role='tenant_admin') AND (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND role='tenant_admin')=1`, tenant, user).Scan(&last)
	if err != nil {
		return err
	}
	if last && role != "tenant_admin" {
		return apperr.New(apperr.KindConflict, "Tenant harus memiliki minimal satu tenant_admin")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_memberships (tenant_id,user_id,role) VALUES ($1,$2,$3) ON CONFLICT (tenant_id,user_id) DO UPDATE SET role=EXCLUDED.role`, tenant, user, role)
	if err != nil {
		return err
	}
	if err = auditTenant(ctx, tx, tenant, actor, user, "member_assigned", role); err != nil {
		return err
	}
	if err = auditAppend(ctx, tx, actor, "tenant_admin", ActionTenantMemberAdded, "tenant_membership",
		&user, &tenant, map[string]any{"role": role, "source": "tenant"}); err != nil {
		return err
	}
	return tx.Commit()
}

// platformAdmin proves the actor is a super user inside the current
// transaction. The tenant role may read the users mirror but not write it, so
// a tenant session cannot forge this.
func platformAdmin(ctx context.Context, tx *sql.Tx, actor uuid.UUID) error {
	var role string
	err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1`, actor).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrForbidden
	}
	if err != nil {
		return err
	}
	if role != "super_user" {
		return apperr.ErrForbidden
	}
	return nil
}

// lockTenant serializes membership changes for one tenant. An advisory lock
// stands in for SELECT ... FOR UPDATE because the platform admin carries no
// tenant context: RLS would either hide the row or demand UPDATE rights on
// tenants, which the platform flows must not need.
func lockTenant(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenant.String())
	return err
}

// requireTenant answers 404 when the tenant does not exist or is hidden from
// the current session.
func requireTenant(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id=$1)`, tenant).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return apperr.ErrNotFound
	}
	return nil
}
func existingUser(ctx context.Context, tx *sql.Tx, user uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1`, user).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrNotFound
	}
	return err
}
func auditTenant(ctx context.Context, tx *sql.Tx, tenant, actor, subject uuid.UUID, action, role string) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_audit_events (id,tenant_id,actor_id,subject_user_id,action,member_role) VALUES ($1,$2,$3,$4,$5,$6)`, id, tenant, actor, subject, action, role)
	return err
}

// Create checks the platform role in the same transaction as provisioning.
func (r *TenantRepository) Create(ctx context.Context, actor, admin uuid.UUID, name string) (models.Tenant, error) {
	var t models.Tenant
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 200 || admin == uuid.Nil {
		return t, apperr.New(apperr.KindBadRequest, "Nama tenant atau pengguna tidak valid")
	}
	tx, err := r.begin(ctx, actor)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	if err = platformAdmin(ctx, tx, actor); err != nil {
		return t, err
	}
	if err = existingUser(ctx, tx, admin); err != nil {
		return t, err
	}
	t.ID, err = uuid.NewV7()
	if err != nil {
		return t, err
	}
	t.Name = name
	if err = setTenant(ctx, tx, t.ID); err != nil {
		return t, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO tenants (id,name) VALUES ($1,$2) RETURNING created_at`, t.ID, t.Name).Scan(&t.CreatedAt)
	if err != nil {
		return t, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_memberships (tenant_id,user_id,role) VALUES ($1,$2,'tenant_admin')`, t.ID, admin)
	if err != nil {
		return t, err
	}
	if err = auditTenant(ctx, tx, t.ID, actor, admin, "tenant_created", "tenant_admin"); err != nil {
		return t, err
	}
	if err = auditAppend(ctx, tx, actor, "super_user", ActionTenantCreated, "tenant",
		&t.ID, &t.ID, map[string]any{"name": t.Name, "admin_id": admin.String()}); err != nil {
		return t, err
	}
	if err = tx.Commit(); err != nil {
		return t, err
	}
	return t, nil
}

// ListAll returns every tenant for the platform admin's pickers. Migration
// 000008 exposes all rows to a super user; the check below proves the role in
// the same transaction so a tenant session cannot read the directory.
func (r *TenantRepository) ListAll(ctx context.Context, actor uuid.UUID) ([]models.Tenant, error) {
	tx, err := r.begin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = platformAdmin(ctx, tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,created_at FROM tenants ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]models.Tenant, 0)
	for rows.Next() {
		var t models.Tenant
		if err = rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
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

// Directory returns every tenant with its members for the platform admin.
// It reads the whole directory in one pass so the page does not fan out one
// request per tenant. Migration 000008 lets a super user SELECT every tenant
// and membership; the role is re-proved here inside the transaction.
func (r *TenantRepository) Directory(ctx context.Context, actor uuid.UUID) ([]models.PlatformTenantGroup, error) {
	tx, err := r.begin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = platformAdmin(ctx, tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.name, t.created_at, m.user_id, u.name, u.email, m.role, m.created_at
		FROM tenants t
		LEFT JOIN tenant_memberships m ON m.tenant_id = t.id
		LEFT JOIN users u ON u.id = m.user_id
		ORDER BY t.name, u.name, u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]models.PlatformTenantGroup, 0)
	byID := make(map[uuid.UUID]int)
	for rows.Next() {
		var (
			tenantID    uuid.UUID
			tenantName  string
			tenantSince time.Time
			memberID    uuid.NullUUID
			memberName  sql.NullString
			memberEmail sql.NullString
			memberRole  sql.NullString
			memberSince sql.NullTime
		)
		if err = rows.Scan(&tenantID, &tenantName, &tenantSince, &memberID, &memberName, &memberEmail, &memberRole, &memberSince); err != nil {
			return nil, err
		}
		index, ok := byID[tenantID]
		if !ok {
			index = len(result)
			byID[tenantID] = index
			result = append(result, models.PlatformTenantGroup{
				ID:        tenantID,
				Name:      tenantName,
				CreatedAt: tenantSince,
				Members:   make([]models.PlatformTenantMember, 0),
			})
		}
		// A tenant with no memberships yields one row of NULLs from the join.
		if !memberID.Valid {
			continue
		}
		result[index].Members = append(result[index].Members, models.PlatformTenantMember{
			UserID:    memberID.UUID,
			Name:      memberName.String,
			Email:     memberEmail.String,
			Role:      memberRole.String,
			CreatedAt: memberSince.Time,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Membership reports the tenant a user belongs to, for the edit form. The
// table allows several memberships per user; the drawer edits one, so the
// oldest is returned and any others are left untouched.
func (r *TenantRepository) Membership(ctx context.Context, actor, user uuid.UUID) (*models.TenantMembership, error) {
	tx, err := r.begin(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = platformAdmin(ctx, tx, actor); err != nil {
		return nil, err
	}
	var m models.TenantMembership
	err = tx.QueryRowContext(ctx, `SELECT tenant_id,user_id,role,created_at FROM tenant_memberships WHERE user_id=$1 ORDER BY created_at LIMIT 1`, user).Scan(&m.TenantID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &m, nil
}

// AssignPlatformMember upserts one membership on behalf of a platform admin.
// Unlike AssignMember it does not require the actor to belong to the tenant:
// it proves the platform role in the same transaction and records the platform
// action, so the audit trail shows where the assignment came from.
func (r *TenantRepository) AssignPlatformMember(ctx context.Context, actor, tenant, user uuid.UUID, role string) error {
	if user == uuid.Nil || tenant == uuid.Nil {
		return apperr.New(apperr.KindBadRequest, "Pengguna atau tenant tidak valid")
	}
	if role != "tenant_admin" && role != "reviewer" {
		return apperr.New(apperr.KindBadRequest, "Peran tenant tidak valid")
	}
	tx, err := r.begin(ctx, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = platformAdmin(ctx, tx, actor); err != nil {
		return err
	}
	if err = lockTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err = requireTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err = existingUser(ctx, tx, user); err != nil {
		return err
	}
	var last bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2 AND role='tenant_admin') AND (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND role='tenant_admin')=1`, tenant, user).Scan(&last)
	if err != nil {
		return err
	}
	if last && role != "tenant_admin" {
		return apperr.New(apperr.KindConflict, "Tenant harus memiliki minimal satu tenant_admin")
	}
	// The insert and audit policies key on app.tenant_id, not on membership.
	if err = setTenant(ctx, tx, tenant); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_memberships (tenant_id,user_id,role) VALUES ($1,$2,$3) ON CONFLICT (tenant_id,user_id) DO UPDATE SET role=EXCLUDED.role`, tenant, user, role)
	if err != nil {
		return err
	}
	if err = auditTenant(ctx, tx, tenant, actor, user, "member_assigned_by_platform", role); err != nil {
		return err
	}
	if err = auditAppend(ctx, tx, actor, "super_user", ActionTenantMemberAdded, "tenant_membership",
		&user, &tenant, map[string]any{"role": role, "source": "platform"}); err != nil {
		return err
	}
	return tx.Commit()
}
