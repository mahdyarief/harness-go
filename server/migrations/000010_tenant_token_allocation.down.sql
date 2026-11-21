DROP POLICY IF EXISTS token_usage_tenant_insert ON tenant_token_usages;
DROP POLICY IF EXISTS token_usage_tenant_select ON tenant_token_usages;
DROP POLICY IF EXISTS token_usage_platform_admin ON tenant_token_usages;
DROP POLICY IF EXISTS token_topup_platform_admin_insert ON tenant_token_topups;
DROP POLICY IF EXISTS token_topup_tenant ON tenant_token_topups;
DROP POLICY IF EXISTS token_topup_platform_admin ON tenant_token_topups;
DROP POLICY IF EXISTS token_balance_tenant_update ON tenant_token_balances;
DROP POLICY IF EXISTS token_balance_tenant_select ON tenant_token_balances;
DROP POLICY IF EXISTS token_balance_platform_admin_update ON tenant_token_balances;
DROP POLICY IF EXISTS token_balance_platform_admin_insert ON tenant_token_balances;
DROP POLICY IF EXISTS token_balance_platform_admin ON tenant_token_balances;

ALTER TABLE tenant_token_usages DISABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_topups DISABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_balances DISABLE ROW LEVEL SECURITY;

DROP TABLE IF EXISTS tenant_token_usages; -- includes billed_tokens column
DROP TABLE IF EXISTS tenant_token_topups;
DROP TABLE IF EXISTS tenant_token_balances;
