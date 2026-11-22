package repositories

import (
	"context"
	"database/sql"
	"strconv"
)

type roleQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// tenantTables must all exist, have RLS enabled and forced. Counting them makes
// the guard fail closed if a migration is missing, and forces this list to be
// updated whenever a new tenant-scoped table is added.
const tenantTables = `('tenants','tenant_memberships','tenant_audit_events','ai_chat_sessions','ai_chat_messages','ai_chat_token_usage','ai_chat_tool_logs','tenant_token_balances','tenant_token_topups','tenant_token_usages','platform_chat_token_usages','audit_logs','knowledge_documents','knowledge_chunks','runs','run_events')`
const tenantTableCount = 16

func TenantRoleUnsafe(ctx context.Context, db roleQuerier) (bool, error) {
	var unsafe bool
	err := db.QueryRowContext(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication
		OR has_any_column_privilege(current_user, 'public.users', 'UPDATE')
		OR (SELECT count(*) FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relname IN `+tenantTables+` AND relkind IN ('r','p')) <> `+strconv.Itoa(tenantTableCount)+`
		OR EXISTS (SELECT 1 FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relname IN `+tenantTables+` AND (NOT relrowsecurity OR NOT relforcerowsecurity))
		OR EXISTS (SELECT 1 FROM pg_class WHERE relnamespace = 'public'::regnamespace
		AND relkind IN ('r','p') AND pg_has_role(current_user, relowner, 'MEMBER'))
		OR EXISTS (SELECT 1 FROM pg_roles r WHERE r.oid <> (SELECT oid FROM pg_roles WHERE rolname=current_user)
		AND pg_has_role(current_user, r.oid, 'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))
		FROM pg_roles WHERE rolname = current_user`).Scan(&unsafe)
	return unsafe, err
}
