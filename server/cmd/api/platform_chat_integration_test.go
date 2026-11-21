package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

type platformChatFixture struct {
	owner, pool                                        *sql.DB
	repo                                               *repositories.ChatRepository
	admin, other, user, peer, outsider, tenant, second uuid.UUID
	scope                                              models.ChatScope
	session                                            models.ChatSession
	message                                            models.ChatMessage
}

func chatRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func chatStatus(t *testing.T, err error, want int) {
	t.Helper()
	got, _ := apperr.From(err)
	if err == nil || got != want {
		t.Fatalf("error = %v (status %d), want %d", err, got, want)
	}
}

func chatSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code {
		t.Fatalf("SQL error = %v, want SQLSTATE %s", err, code)
	}
}

func chatExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	chatRequire(t, err)
}

// These DSNs must name newly created disposable databases, never staging.
func chatTestDB(t *testing.T, env string) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " not set (new disposable PostgreSQL 18 database)")
	}
	u, err := url.Parse(dsn)
	chatRequire(t, err)
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "hsg_chat_regression_") {
		t.Fatal("refusing database without hsg_chat_regression_ disposable prefix")
	}
	db, err := lib.OpenDB(dsn)
	chatRequire(t, err)
	t.Cleanup(func() { db.Close() })
	m, err := lib.NewMigrator(db)
	chatRequire(t, err)
	if err = m.Up(); err != nil && err.Error() != "no change" {
		t.Fatal(err)
	}
	return db, dsn
}

func newPlatformChatFixture(t *testing.T) *platformChatFixture {
	t.Helper()
	owner, dsn := chatTestDB(t, "PLATFORM_CHAT_TEST_DB_DSN")
	f := &platformChatFixture{owner: owner, admin: uuid.New(), other: uuid.New(), user: uuid.New(), peer: uuid.New(), outsider: uuid.New(), tenant: uuid.New(), second: uuid.New(), scope: models.ChatScope{Kind: models.ChatPlatformPersonal}}
	for _, id := range []uuid.UUID{f.admin, f.other, f.user, f.peer, f.outsider} {
		role := "user"
		if id == f.admin || id == f.other {
			role = "super_user"
		}
		chatExec(t, owner, `INSERT INTO users(id,name,email,role) VALUES($1,'Regression',$2,$3)`, id, id.String()+"@test.invalid", role)
	}
	for _, id := range []uuid.UUID{f.tenant, f.second} {
		chatExec(t, owner, `INSERT INTO tenants(id,name) VALUES($1,'Regression')`, id)
		chatExec(t, owner, `INSERT INTO tenant_token_balances(tenant_id,balance) VALUES($1,1000)`, id)
	}
	for _, pair := range [][2]uuid.UUID{{f.tenant, f.user}, {f.tenant, f.peer}, {f.second, f.user}} {
		chatExec(t, owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, pair[0], pair[1])
	}
	f.pool = tenantTestPool(t, owner, dsn)
	var role string
	chatRequire(t, f.pool.QueryRow(`SELECT current_user`).Scan(&role))
	chatExec(t, owner, `GRANT SELECT,INSERT ON platform_chat_token_usages TO `+role)
	f.pool.SetMaxOpenConns(1)
	f.pool.SetMaxIdleConns(1)
	f.repo = repositories.NewChatRepository(f.pool)
	return f
}

func (f *platformChatFixture) seedSession(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var err error
	f.session, err = f.repo.CreateSession(ctx, f.admin, f.scope, "Platform regression")
	chatRequire(t, err)
	f.message, err = f.repo.AppendMessage(ctx, f.admin, f.scope, models.ChatMessage{SessionID: f.session.ID, Role: "assistant", Content: "synthetic"})
	chatRequire(t, err)
	chatRequire(t, f.repo.RecordTokenUsage(ctx, f.admin, f.scope, models.ChatTokenUsage{SessionID: f.session.ID, MessageID: f.message.ID, PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}))
	chatRequire(t, f.repo.RecordToolLog(ctx, f.admin, f.scope, models.ChatToolLog{SessionID: f.session.ID, Turn: 1, ToolName: "synthetic", Arguments: "{}", Result: "{}"}))
	// Seed independently so a metering API regression cannot hide RLS coverage.
	chatExec(t, f.owner, `INSERT INTO platform_chat_token_usages(user_id,session_id,prompt_tokens,completion_tokens,total_tokens,failed) VALUES($1,$2,7,3,10,false)`, f.admin, f.session.ID)
}

func TestPlatformChatIntegration(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *platformChatFixture)
	}{
		{"superuser_without_membership", testPlatformChatCRUD},
		{"other_admin_isolation", testPlatformChatOtherAdmin},
		{"admin_membership_preserves_platform_scope", testPlatformChatMembership},
		{"nonmember_forbidden", testPlatformChatNonmember},
		{"tenant_scope_and_ownership", testPlatformChatTenant},
		{"raw_hsg_tenant_rls", testPlatformChatRawRLS},
		{"role_downgrade_revokes_access", testPlatformChatDowngrade},
		{"pooled_context_does_not_leak", testPlatformChatPool},
		{"platform_usage_api", testPlatformChatUsageAPI},
		{"append_only_usage_survives_delete_no_tenant_debit", testPlatformChatLedger},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, newPlatformChatFixture(t)) })
	}
}

func testPlatformChatCRUD(t *testing.T, f *platformChatFixture) {
	ctx := context.Background()
	scope, err := f.repo.ResolveScope(ctx, f.admin, uuid.Nil)
	chatRequire(t, err)
	if scope.Kind != models.ChatPlatformPersonal || scope.TenantID != uuid.Nil {
		t.Fatalf("scope = %+v", scope)
	}
	rows, err := f.repo.ListSessions(ctx, f.admin, scope, repositories.ListParams{Limit: 20})
	chatRequire(t, err)
	total, err := f.repo.CountSessions(ctx, f.admin, scope)
	chatRequire(t, err)
	if len(rows) != 0 || total != 0 {
		t.Fatalf("initial list = %v, %d", rows, total)
	}
	f.seedSession(t)
	if f.session.TenantID != nil || f.session.Scope != models.ChatPlatformPersonal {
		t.Fatalf("session = %+v", f.session)
	}
	chatRequire(t, f.repo.UpdateSessionTitle(ctx, f.admin, scope, f.session.ID, "Renamed"))
	renamed, err := f.repo.GetSession(ctx, f.admin, scope, f.session.ID)
	chatRequire(t, err)
	if renamed.Title != "Renamed" {
		t.Fatalf("title = %q", renamed.Title)
	}
	rows, err = f.repo.ListSessions(ctx, f.admin, scope, repositories.ListParams{Limit: 20})
	chatRequire(t, err)
	total, err = f.repo.CountSessions(ctx, f.admin, scope)
	chatRequire(t, err)
	if len(rows) != 1 || total != 1 || rows[0].ID != f.session.ID || rows[0].TenantID != nil {
		t.Fatalf("list = %+v, %d", rows, total)
	}
	messages, err := f.repo.ListMessages(ctx, f.admin, scope, f.session.ID)
	chatRequire(t, err)
	if len(messages) != 1 || messages[0].Content != "synthetic" {
		t.Fatalf("messages = %+v", messages)
	}
	logs, _, err := f.repo.ListToolLogs(ctx, f.admin, scope, f.session.ID, 100, 0)
	chatRequire(t, err)
	if len(logs) != 1 || logs[0].ToolName != "synthetic" {
		t.Fatalf("logs = %+v", logs)
	}
	chatRequire(t, f.repo.DeleteSession(ctx, f.admin, scope, f.session.ID))
	exists, err := f.repo.ExistsSession(ctx, f.admin, scope, f.session.ID)
	chatRequire(t, err)
	if exists {
		t.Fatal("deleted session remains visible")
	}
}

func testPlatformChatOtherAdmin(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	assertChatHidden(t, f, f.other, f.scope, f.session.ID, f.message.ID)
	own, err := f.repo.CreateSession(context.Background(), f.other, f.scope, "Other admin")
	chatRequire(t, err)
	assertChatHidden(t, f, f.admin, f.scope, own.ID, uuid.New())
}

// A hidden session must be unreadable and immutable, including child writes.
func assertChatHidden(t *testing.T, f *platformChatFixture, user uuid.UUID, scope models.ChatScope, session, message uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	exists, err := f.repo.ExistsSession(ctx, user, scope, session)
	chatRequire(t, err)
	if exists {
		t.Fatal("foreign session exists")
	}
	_, err = f.repo.GetSession(ctx, user, scope, session)
	chatStatus(t, err, 404)
	err = f.repo.UpdateSessionTitle(ctx, user, scope, session, "stolen")
	chatStatus(t, err, 404)
	chatStatus(t, f.repo.DeleteSession(ctx, user, scope, session), 404)
	messages, err := f.repo.ListMessages(ctx, user, scope, session)
	chatRequire(t, err)
	if len(messages) != 0 {
		t.Fatal("foreign transcript leaked")
	}
	_, _, err = f.repo.ListToolLogs(ctx, user, scope, session, 100, 0)
	chatStatus(t, err, 404)
	_, err = f.repo.AppendMessage(ctx, user, scope, models.ChatMessage{SessionID: session, Role: "user", Content: "forbidden"})
	chatSQLState(t, err, "42501")
	err = f.repo.RecordToolLog(ctx, user, scope, models.ChatToolLog{SessionID: session, Turn: 1, ToolName: "forbidden", Arguments: "{}", Result: "{}"})
	chatSQLState(t, err, "42501")
	err = f.repo.RecordTokenUsage(ctx, user, scope, models.ChatTokenUsage{SessionID: session, MessageID: message, PromptTokens: 1, TotalTokens: 1})
	chatSQLState(t, err, "42501")
	rows, err := f.repo.ListSessions(ctx, user, scope, repositories.ListParams{Limit: 100})
	chatRequire(t, err)
	for _, row := range rows {
		if row.ID == session {
			t.Fatal("foreign session listed")
		}
	}
}

func testPlatformChatMembership(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	chatExec(t, f.owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, f.tenant, f.admin)
	scope, err := f.repo.ResolveScope(context.Background(), f.admin, uuid.Nil)
	chatRequire(t, err)
	if scope.Kind != models.ChatPlatformPersonal || scope.TenantID != uuid.Nil {
		t.Fatalf("membership changed scope: %+v", scope)
	}
	_, err = f.repo.GetSession(context.Background(), f.admin, scope, f.session.ID)
	chatRequire(t, err)
	tenantScope := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: f.tenant}
	_, err = f.repo.CreateSession(context.Background(), f.admin, tenantScope, "tenant injection")
	chatStatus(t, err, 403)
	injected := models.ChatScope{Kind: models.ChatPlatformPersonal, TenantID: f.tenant}
	session, err := f.repo.CreateSession(context.Background(), f.admin, injected, "ignored tenant")
	chatRequire(t, err)
	if session.TenantID != nil {
		t.Fatal("platform session acquired a tenant")
	}
}

func testPlatformChatNonmember(t *testing.T, f *platformChatFixture) {
	ctx := context.Background()
	_, err := f.repo.ResolveScope(ctx, f.outsider, uuid.Nil)
	chatStatus(t, err, 403)
	for _, scope := range []models.ChatScope{f.scope, {Kind: models.ChatTenantPersonal, TenantID: f.tenant}} {
		_, err = f.repo.ListSessions(ctx, f.outsider, scope, repositories.ListParams{Limit: 20})
		chatStatus(t, err, 403)
		_, err = f.repo.CreateSession(ctx, f.outsider, scope, "forbidden")
		chatStatus(t, err, 403)
		chatStatus(t, f.repo.RecordPlatformUsage(ctx, f.outsider, scope, uuid.New(), 1, 0, false), 403)
	}
}

func testPlatformChatTenant(t *testing.T, f *platformChatFixture) {
	ctx := context.Background()
	one := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: f.tenant}
	two := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: f.second}
	session, err := f.repo.CreateSession(ctx, f.user, one, "Tenant one")
	chatRequire(t, err)
	if session.TenantID == nil || *session.TenantID != f.tenant || session.Scope != models.ChatTenantPersonal {
		t.Fatalf("tenant session = %+v", session)
	}
	message, err := f.repo.AppendMessage(ctx, f.user, one, models.ChatMessage{SessionID: session.ID, Role: "assistant", Content: "tenant synthetic"})
	chatRequire(t, err)
	chatRequire(t, f.repo.RecordTokenUsage(ctx, f.user, one, models.ChatTokenUsage{SessionID: session.ID, MessageID: message.ID, PromptTokens: 1, TotalTokens: 1}))
	chatRequire(t, f.repo.RecordToolLog(ctx, f.user, one, models.ChatToolLog{SessionID: session.ID, Turn: 1, ToolName: "synthetic", Arguments: "{}", Result: "{}"}))
	assertChatHidden(t, f, f.peer, one, session.ID, message.ID)
	assertChatHidden(t, f, f.user, two, session.ID, message.ID)
	// Raw RLS cannot rely on the repository's tenant WHERE clause.
	chatRaw(t, f, f.user, f.second, func(tx *sql.Tx) {
		var count int
		chatRequire(t, tx.QueryRow(`SELECT count(*) FROM ai_chat_sessions WHERE id=$1`, session.ID).Scan(&count))
		if count != 0 {
			t.Fatal("cross-tenant session visible through raw RLS")
		}
	})
	_, err = f.repo.CreateSession(ctx, f.peer, two, "no membership")
	chatStatus(t, err, 403)
	_, err = f.repo.CreateSession(ctx, f.user, f.scope, "platform injection")
	chatStatus(t, err, 403)
	chatStatus(t, f.repo.RecordPlatformUsage(ctx, f.user, one, session.ID, 1, 0, false), 403)
	chatRequire(t, f.repo.DeleteSession(ctx, f.user, one, session.ID))
}

// SET LOCAL ROLE exercises the real runtime role without exposing its password.
func chatRaw(t *testing.T, f *platformChatFixture, user, tenant uuid.UUID, run func(*sql.Tx)) {
	t.Helper()
	chatExec(t, f.owner, `GRANT USAGE ON SCHEMA public TO hsg_tenant`)
	chatExec(t, f.owner, `GRANT SELECT ON users,tenant_memberships TO hsg_tenant`)
	chatExec(t, f.owner, `GRANT SELECT,INSERT,UPDATE,DELETE ON ai_chat_sessions,ai_chat_messages,ai_chat_token_usage,ai_chat_tool_logs,platform_chat_token_usages TO hsg_tenant`)
	tx, err := f.owner.BeginTx(context.Background(), nil)
	chatRequire(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`SET LOCAL ROLE hsg_tenant`)
	chatRequire(t, err)
	tenantID := ""
	if tenant != uuid.Nil {
		tenantID = tenant.String()
	}
	_, err = tx.Exec(`SELECT set_config('app.user_id',$1,true),set_config('app.tenant_id',$2,true)`, user.String(), tenantID)
	chatRequire(t, err)
	var role string
	chatRequire(t, tx.QueryRow(`SELECT current_user`).Scan(&role))
	if role != "hsg_tenant" {
		t.Fatalf("raw role = %s", role)
	}
	run(tx)
}

func testPlatformChatRawRLS(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	chatExec(t, f.owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, f.tenant, f.admin)
	chatRaw(t, f, f.admin, uuid.Nil, func(tx *sql.Tx) {
		var count int
		chatRequire(t, tx.QueryRow(`SELECT count(*) FROM ai_chat_sessions WHERE id=$1`, f.session.ID).Scan(&count))
		if count != 1 {
			t.Fatal("positive RLS control did not see platform session")
		}
	})
	queries := []struct {
		name, query string
		args        []any
	}{
		{"session", `INSERT INTO ai_chat_sessions(id,tenant_id,user_id,title) VALUES($1,NULL,$2,'denied')`, []any{uuid.New(), f.admin}},
		{"tenant_session_for_superuser", `INSERT INTO ai_chat_sessions(id,tenant_id,user_id,title) VALUES($1,$2,$3,'denied')`, []any{uuid.New(), f.tenant, f.admin}},
		{"message", `INSERT INTO ai_chat_messages(id,session_id,role,content) VALUES($1,$2,'user','denied')`, []any{uuid.New(), f.session.ID}},
		{"tool", `INSERT INTO ai_chat_tool_logs(id,session_id,turn,tool_name,arguments,result,duration_ms) VALUES($1,$2,1,'denied','{}','{}',0)`, []any{uuid.New(), f.session.ID}},
		{"usage", `INSERT INTO ai_chat_token_usage(id,session_id,message_id,prompt_tokens,completion_tokens,total_tokens) VALUES($1,$2,$3,1,0,1)`, []any{uuid.New(), f.session.ID, f.message.ID}},
		{"platform_ledger", `INSERT INTO platform_chat_token_usages(user_id,session_id,prompt_tokens,completion_tokens,total_tokens,failed) VALUES($1,$2,1,0,1,false)`, []any{f.admin, f.session.ID}},
	}
	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			chatRaw(t, f, f.admin, f.tenant, func(tx *sql.Tx) {
				for _, table := range []string{"ai_chat_sessions", "ai_chat_messages", "ai_chat_token_usage", "ai_chat_tool_logs", "platform_chat_token_usages"} {
					var count int
					chatRequire(t, tx.QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
					if count != 0 {
						t.Fatalf("nonempty tenant context exposed %s", table)
					}
				}
				_, err := tx.Exec(q.query, q.args...)
				chatSQLState(t, err, "42501")
			})
		})
	}
	chatRaw(t, f, f.admin, f.tenant, func(tx *sql.Tx) {
		for _, query := range []string{`UPDATE ai_chat_sessions SET title='denied' WHERE id=$1`, `DELETE FROM ai_chat_sessions WHERE id=$1`} {
			result, err := tx.Exec(query, f.session.ID)
			chatRequire(t, err)
			n, err := result.RowsAffected()
			chatRequire(t, err)
			if n != 0 {
				t.Fatal("nonempty tenant context mutated platform session")
			}
		}
	})
}

func testPlatformChatDowngrade(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	ctx := context.Background()
	chatExec(t, f.owner, `UPDATE users SET role='user' WHERE id=$1`, f.admin)
	_, err := f.repo.ResolveScope(ctx, f.admin, uuid.Nil)
	chatStatus(t, err, 403)
	checks := []func() error{
		func() error {
			_, e := f.repo.ListSessions(ctx, f.admin, f.scope, repositories.ListParams{Limit: 20})
			return e
		},
		func() error { _, e := f.repo.CreateSession(ctx, f.admin, f.scope, "denied"); return e },
		func() error { _, e := f.repo.GetSession(ctx, f.admin, f.scope, f.session.ID); return e },
		func() error { _, e := f.repo.ExistsSession(ctx, f.admin, f.scope, f.session.ID); return e },
		func() error {
			e := f.repo.UpdateSessionTitle(ctx, f.admin, f.scope, f.session.ID, "denied")
			return e
		},
		func() error { return f.repo.DeleteSession(ctx, f.admin, f.scope, f.session.ID) },
		func() error { _, e := f.repo.ListMessages(ctx, f.admin, f.scope, f.session.ID); return e },
		func() error {
			_, e := f.repo.AppendMessage(ctx, f.admin, f.scope, models.ChatMessage{SessionID: f.session.ID, Role: "user", Content: "denied"})
			return e
		},
		func() error { _, _, e := f.repo.ListToolLogs(ctx, f.admin, f.scope, f.session.ID, 100, 0); return e },
		func() error {
			return f.repo.RecordToolLog(ctx, f.admin, f.scope, models.ChatToolLog{SessionID: f.session.ID})
		},
		func() error {
			return f.repo.RecordTokenUsage(ctx, f.admin, f.scope, models.ChatTokenUsage{SessionID: f.session.ID, MessageID: f.message.ID})
		},
		func() error { return f.repo.RecordPlatformUsage(ctx, f.admin, f.scope, f.session.ID, 1, 0, true) },
	}
	for i, check := range checks {
		t.Run(fmt.Sprintf("operation_%02d", i), func(t *testing.T) { chatStatus(t, check(), 403) })
	}
	chatRaw(t, f, f.admin, uuid.Nil, func(tx *sql.Tx) {
		for _, table := range []string{"ai_chat_sessions", "ai_chat_messages", "ai_chat_token_usage", "ai_chat_tool_logs", "platform_chat_token_usages"} {
			var count int
			chatRequire(t, tx.QueryRow(`SELECT count(*) FROM `+table+` WHERE `+map[string]string{"ai_chat_sessions": "id", "ai_chat_messages": "session_id", "ai_chat_token_usage": "session_id", "ai_chat_tool_logs": "session_id", "platform_chat_token_usages": "session_id"}[table]+`=$1`, f.session.ID).Scan(&count))
			if count != 0 {
				t.Fatalf("downgraded user can read %s", table)
			}
		}
	})
	// A membership after downgrade may open tenant chat, never the old platform session.
	chatExec(t, f.owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, f.tenant, f.admin)
	scope, err := f.repo.ResolveScope(ctx, f.admin, uuid.Nil)
	chatRequire(t, err)
	if scope.Kind != models.ChatTenantPersonal {
		t.Fatalf("downgraded scope = %+v", scope)
	}
	assertChatHidden(t, f, f.admin, scope, f.session.ID, f.message.ID)
}

func testPlatformChatPool(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	ctx := context.Background()
	scope := models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: f.tenant}
	for i := 0; i < 8; i++ {
		_, err := f.repo.GetSession(ctx, f.admin, f.scope, f.session.ID)
		chatRequire(t, err)
		_, err = f.repo.CreateSession(ctx, f.user, scope, "tenant pooled")
		chatRequire(t, err)
		_, err = f.repo.CreateSession(ctx, f.outsider, f.scope, "rollback")
		chatStatus(t, err, 403)
		_, err = f.repo.AppendMessage(ctx, f.other, f.scope, models.ChatMessage{SessionID: f.session.ID, Role: "user", Content: "rollback"})
		chatSQLState(t, err, "42501")
		var user, tenant string
		var count int
		chatRequire(t, f.pool.QueryRow(`SELECT coalesce(current_setting('app.user_id',true),''),coalesce(current_setting('app.tenant_id',true),'')`).Scan(&user, &tenant))
		if user != "" || tenant != "" {
			t.Fatalf("pooled context leaked: user=%q tenant=%q", user, tenant)
		}
		chatRequire(t, f.pool.QueryRow(`SELECT count(*) FROM ai_chat_sessions`).Scan(&count))
		if count != 0 {
			t.Fatal("context-free pooled query exposed sessions")
		}
	}
	if f.pool.Stats().MaxOpenConnections != 1 {
		t.Fatal("pool test must reuse a single connection")
	}
}

func testPlatformChatUsageAPI(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	for _, failed := range []bool{false, true} {
		if err := f.repo.RecordPlatformUsage(context.Background(), f.admin, f.scope, f.session.ID, 2, 1, failed); err != nil {
			t.Errorf("RecordPlatformUsage(failed=%v): %v", failed, err)
		}
	}
	var count, total int
	chatRequire(t, f.owner.QueryRow(`SELECT count(*),sum(total_tokens) FROM platform_chat_token_usages WHERE session_id=$1`, f.session.ID).Scan(&count, &total))
	if count != 3 || total != 16 {
		t.Errorf("usage API persisted %d rows/%d tokens, want 3/16", count, total)
	}
}

func testPlatformChatLedger(t *testing.T, f *platformChatFixture) {
	f.seedSession(t)
	ctx := context.Background()
	chatExec(t, f.owner, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'tenant_admin')`, f.tenant, f.admin)
	chatExec(t, f.owner, `INSERT INTO platform_chat_token_usages(user_id,session_id,prompt_tokens,completion_tokens,total_tokens,failed) VALUES($1,$2,2,1,3,true)`, f.admin, f.session.ID)
	for _, query := range []string{`UPDATE platform_chat_token_usages SET failed=true WHERE session_id=$1`, `DELETE FROM platform_chat_token_usages WHERE session_id=$1`} {
		_, err := f.owner.Exec(query, f.session.ID)
		chatSQLState(t, err, "P0001")
	}
	chatRequire(t, f.repo.DeleteSession(ctx, f.admin, f.scope, f.session.ID))
	var count, total, failed int
	chatRequire(t, f.owner.QueryRow(`SELECT count(*),sum(total_tokens),count(*) FILTER(WHERE failed) FROM platform_chat_token_usages WHERE session_id=$1`, f.session.ID).Scan(&count, &total, &failed))
	if count != 2 || total != 13 || failed != 1 {
		t.Fatalf("retained metering = %d rows, %d tokens, %d failed", count, total, failed)
	}
	for _, table := range []string{"ai_chat_messages", "ai_chat_tool_logs", "ai_chat_token_usage"} {
		chatRequire(t, f.owner.QueryRow(`SELECT count(*) FROM `+table+` WHERE session_id=$1`, f.session.ID).Scan(&count))
		if count != 0 {
			t.Fatalf("session deletion did not cascade %s", table)
		}
	}
	for _, tenant := range []uuid.UUID{f.tenant, f.second} {
		var balance int
		chatRequire(t, f.owner.QueryRow(`SELECT balance FROM tenant_token_balances WHERE tenant_id=$1`, tenant).Scan(&balance))
		if balance != 1000 {
			t.Fatalf("platform usage debited tenant: %d", balance)
		}
		chatRequire(t, f.owner.QueryRow(`SELECT count(*) FROM tenant_token_usages WHERE tenant_id=$1`, tenant).Scan(&count))
		if count != 0 {
			t.Fatal("platform usage added tenant ledger entry")
		}
	}
}
