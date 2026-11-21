package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

// The DSN must point at a disposable PostgreSQL 18 database with role creation rights.
func tenantTestPool(t *testing.T, owner *sql.DB, dsn string) *sql.DB {
	t.Helper()
	role := "tenant_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	password := uuid.NewString()
	var database string
	if err := owner.QueryRow(`SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`, role, password),
		fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO %s`, pgx.Identifier{database}.Sanitize(), role),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, role),
		fmt.Sprintf(`GRANT SELECT ON users TO %s`, role),
		fmt.Sprintf(`GRANT SELECT,INSERT,UPDATE ON tenants,tenant_memberships,tenant_audit_events TO %s`, role),
		fmt.Sprintf(`GRANT SELECT,INSERT,UPDATE,DELETE ON ai_chat_sessions,ai_chat_messages,ai_chat_token_usage,ai_chat_tool_logs TO %s`, role),
		fmt.Sprintf(`GRANT SELECT,INSERT,UPDATE ON tenant_token_balances TO %s`, role),
		fmt.Sprintf(`GRANT SELECT,INSERT ON tenant_token_topups,tenant_token_usages TO %s`, role),
		fmt.Sprintf(`GRANT SELECT,INSERT ON audit_logs TO %s`, role),
	} {
		if _, err := owner.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// Register after the owner's Close cleanup, but before pool creation can fail.
	t.Cleanup(func() {
		if _, err := owner.Exec("DROP OWNED BY " + role); err != nil {
			t.Errorf("cleanup role grants: %v", err)
		}
		if _, err := owner.Exec("DROP ROLE " + role); err != nil {
			t.Errorf("cleanup role: %v", err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, password)
	pool, err := connectTenantDB(u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func tenantConflict(err error) bool {
	status, _ := apperr.From(err)
	return status == int(apperr.KindConflict)
}

func TestTenantIntegration(t *testing.T) {
	dsn := os.Getenv("TENANT_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TENANT_TEST_DB_DSN not set (disposable PostgreSQL 18 database)")
	}
	ctx := context.Background()
	owner, err := lib.OpenDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	migrator, err := lib.NewMigrator(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrator.Up(); err != nil && err.Error() != "no change" {
		t.Fatal(err)
	}
	pool := tenantTestPool(t, owner, dsn)
	repo := repositories.NewTenantRepository(pool)
	platform, a, b, reviewer, outsider := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, id := range []uuid.UUID{platform, a, b, reviewer, outsider} {
		role := "user"
		if id == platform {
			role = "super_user"
		}
		if _, err = owner.Exec(`INSERT INTO users(id,name,email,role) VALUES($1,'Test',$2,$3)`, id, id.String()+"@test.invalid", role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = repo.Create(ctx, a, a, "forbidden"); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("non-platform create: %v", err)
	}
	one, err := repo.Create(ctx, platform, a, "One")
	if err != nil {
		t.Fatal(err)
	}
	two, err := repo.Create(ctx, platform, b, "Two")
	if err != nil {
		t.Fatal(err)
	}
	if one.ID.Version() != 7 || two.ID.Version() != 7 {
		t.Fatal("not UUIDv7")
	}
	if got, err := repo.List(ctx, a); err != nil || len(got) != 1 || got[0].Role != "tenant_admin" || got[0].ID != one.ID {
		t.Fatalf("list role/isolation: %v %v", got, err)
	}
	if got, err := repo.List(ctx, platform); err != nil || len(got) != 0 {
		t.Fatalf("platform sees tenant: %v %v", got, err)
	}
	for _, id := range []uuid.UUID{platform, b, outsider} {
		if _, err = repo.Members(ctx, id, one.ID); !errors.Is(err, apperr.ErrForbidden) {
			t.Fatalf("cross tenant read: %v", err)
		}
	}
	if err = repo.AssignMember(ctx, a, one.ID, reviewer, "reviewer"); err != nil {
		t.Fatal(err)
	}
	// The member list carries identity from users, not just the UUID.
	members, err := repo.Members(ctx, a, one.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("member count: %d", len(members))
	}
	for _, member := range members {
		if member.Name != "Test" || member.Email != member.UserID.String()+"@test.invalid" {
			t.Fatalf("member identity: %v", member)
		}
	}
	if err = repo.AssignMember(ctx, reviewer, one.ID, reviewer, "tenant_admin"); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("reviewer escalation: %v", err)
	}
	if err = repo.AssignMember(ctx, platform, one.ID, platform, "tenant_admin"); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("platform bypass: %v", err)
	}
	if err = repo.AssignMember(ctx, a, one.ID, reviewer, "super_user"); err == nil {
		t.Fatal("accepted global role")
	}
	if err = repo.AssignMember(ctx, a, one.ID, a, "reviewer"); !tenantConflict(err) {
		t.Fatalf("last admin demotion: %v", err)
	}
	if err = repo.AssignMember(ctx, a, one.ID, b, "tenant_admin"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []uuid.UUID{a, b} {
		wg.Add(1)
		go func(id uuid.UUID) { defer wg.Done(); results <- repo.AssignMember(ctx, id, one.ID, id, "reviewer") }(id)
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else if !tenantConflict(e) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent demotions succeeded=%d", successes)
	}
	var count int
	if err = owner.QueryRow(`SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND role='tenant_admin'`, one.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin invariant %d %v", count, err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	for range 5 {
		if _, err = repo.List(ctx, a); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Members(ctx, outsider, one.ID); !errors.Is(err, apperr.ErrForbidden) {
			t.Fatal(err)
		}
		if err = pool.QueryRow(`SELECT count(*) FROM tenants`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("pool tenant leak %d %v", count, err)
		}
		if err = pool.QueryRow(`SELECT count(*) FROM tenant_memberships`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("pool membership leak %d %v", count, err)
		}
	}
	// Even an explicitly scoped runtime transaction cannot read/write another tenant.
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('app.user_id',$1,true),set_config('app.tenant_id',$2,true)`, a.String(), one.ID.String()); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(`SELECT count(*) FROM tenants WHERE id=$1`, two.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-tenant RLS read: %d %v", count, err)
	}
	if _, err = tx.Exec(`INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, two.ID, outsider); err == nil {
		t.Fatal("cross-tenant RLS write allowed")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(`SELECT count(*) FROM tenants`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback context leaked: %d %v", count, err)
	}
	if _, err = pool.Exec(`UPDATE users SET role='super_user' WHERE id=$1`, reviewer); err == nil {
		t.Fatal("runtime can escalate global role")
	}
	if _, err = owner.Exec(`ALTER TABLE tenants NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	unsafe, checkErr := repositories.TenantRoleUnsafe(ctx, pool)
	_, restoreErr := owner.Exec(`ALTER TABLE tenants FORCE ROW LEVEL SECURITY`)
	if checkErr != nil || restoreErr != nil || !unsafe {
		t.Fatalf("disabled FORCE RLS accepted: %v %v %v", unsafe, checkErr, restoreErr)
	}
	if _, err = connectTenantDB(dsn); err == nil {
		t.Fatal("startup accepted owner pool")
	}
	if _, err = repo.List(ctx, uuid.Nil); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatal("anonymous repository access")
	}
	// Platform directory: a super user reads every tenant and membership; a
	// tenant session and an anonymous actor do not.
	all, err := repo.ListAll(ctx, platform)
	if err != nil {
		t.Fatalf("platform list all: %v", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, tenant := range all {
		seen[tenant.ID] = true
	}
	if !seen[one.ID] || !seen[two.ID] {
		t.Fatalf("platform list missing tenants: %v", all)
	}
	if _, err = repo.ListAll(ctx, a); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("tenant session lists directory: %v", err)
	}
	if _, err = repo.ListAll(ctx, uuid.Nil); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatal("anonymous platform list")
	}
	// The grouped directory carries each tenant's members with their identity,
	// and keeps a memberless tenant as an empty group.
	directory, err := repo.Directory(ctx, platform)
	if err != nil {
		t.Fatalf("platform directory: %v", err)
	}
	groups := map[uuid.UUID]models.PlatformTenantGroup{}
	for _, group := range directory {
		groups[group.ID] = group
	}
	if len(groups) != 2 {
		t.Fatalf("directory tenant count: %d", len(groups))
	}
	if group := groups[one.ID]; len(group.Members) != 3 {
		t.Fatalf("directory members of one: %v", group.Members)
	}
	for _, member := range groups[one.ID].Members {
		if member.Name != "Test" || member.Email != member.UserID.String()+"@test.invalid" {
			t.Fatalf("directory identity: %v", member)
		}
	}
	// Two was created with b as its only admin at this point.
	if group := groups[two.ID]; len(group.Members) != 1 {
		t.Fatalf("directory members of two: %v", group.Members)
	}
	if _, err = repo.Directory(ctx, a); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("tenant session reads directory: %v", err)
	}
	if _, err = repo.Directory(ctx, uuid.Nil); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatal("anonymous directory")
	}
	membership, err := repo.Membership(ctx, platform, reviewer)
	if err != nil || membership == nil || membership.TenantID != one.ID || membership.Role != "reviewer" {
		t.Fatalf("platform membership read: %v %v", membership, err)
	}
	if m, err := repo.Membership(ctx, platform, outsider); err != nil || m != nil {
		t.Fatalf("membership of memberless user: %v %v", m, err)
	}
	if _, err = repo.Membership(ctx, a, reviewer); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("tenant session reads membership: %v", err)
	}
	// Platform assignment upserts a membership and records the platform action.
	if err = repo.AssignPlatformMember(ctx, platform, two.ID, reviewer, "reviewer"); err != nil {
		t.Fatal(err)
	}
	if err = repo.AssignPlatformMember(ctx, a, two.ID, reviewer, "reviewer"); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("tenant session assigns platform member: %v", err)
	}
	if err = repo.AssignPlatformMember(ctx, platform, two.ID, reviewer, "super_user"); err == nil {
		t.Fatal("accepted global tenant role")
	}
	if err = repo.AssignPlatformMember(ctx, platform, uuid.Must(uuid.NewV7()), reviewer, "reviewer"); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("unknown tenant: %v", err)
	}
	var auditAction string
	if err = owner.QueryRow(`SELECT action FROM tenant_audit_events WHERE tenant_id=$1 AND subject_user_id=$2 ORDER BY created_at DESC LIMIT 1`, two.ID, reviewer).Scan(&auditAction); err != nil || auditAction != "member_assigned_by_platform" {
		t.Fatalf("platform audit action %q %v", auditAction, err)
	}
	if unsafe, err := repositories.TenantRoleUnsafe(ctx, owner); err != nil || !unsafe {
		t.Fatal("owner accepted")
	}
}
