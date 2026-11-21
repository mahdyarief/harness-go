-- Token allocation: the platform admin grants each tenant a pool of LLM tokens
-- that is spent down as the tenant's chats consume them. There is no automatic
-- reset; the admin tops up manually, and every addition and every deduction is
-- append-only so usage is fully transparent and audit-ready.
--
-- These tables are tenant-scoped by RLS, not user-scoped: a super user must see
-- every tenant's balance and ledger, while a tenant member sees only their own
-- tenant. The policies below combine the platform-admin permissive SELECT (mirrors
-- 000008) with a tenant_scope policy keyed on app.tenant_id. The balance is the
-- only mutable row; the ledgers are append-only by design (no UPDATE/DELETE).

CREATE TABLE tenant_token_balances (
    tenant_id   uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    balance     bigint NOT NULL DEFAULT 0 CHECK (balance >= 0),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tenant_token_topups (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    amount      bigint NOT NULL CHECK (amount > 0),
    note        text NOT NULL DEFAULT '' CHECK (length(trim(note)) <= 500),
    actor_id    uuid NOT NULL REFERENCES users(id),
    reason      text NOT NULL DEFAULT 'manual' CHECK (reason IN ('manual', 'initial')),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tenant_token_usages (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id),
    session_id      uuid REFERENCES ai_chat_sessions(id) ON DELETE SET NULL,
    message_id      uuid REFERENCES ai_chat_messages(id) ON DELETE SET NULL,
    prompt_tokens   int NOT NULL CHECK (prompt_tokens >= 0),
    completion_tokens int NOT NULL CHECK (completion_tokens >= 0),
    total_tokens    int NOT NULL CHECK (total_tokens >= 0),
    -- billed_tokens is what was actually deducted. It equals total_tokens
    -- except on the last turn before exhaustion, where it is clamped to the
    -- remaining balance so the balance can never go negative. Keeping both
    -- makes the ledger exact (sum(billed) = granted - balance) while the
    -- consumption history stays transparent.
    billed_tokens   int NOT NULL CHECK (billed_tokens >= 0),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tenant_token_topups_tenant_idx ON tenant_token_topups(tenant_id, created_at DESC);
CREATE INDEX tenant_token_usages_tenant_idx ON tenant_token_usages(tenant_id, created_at DESC);
CREATE INDEX tenant_token_usages_user_idx ON tenant_token_usages(user_id, created_at DESC);
CREATE INDEX tenant_token_usages_session_idx ON tenant_token_usages(session_id);

ALTER TABLE tenant_token_balances ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_balances FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_topups ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_topups FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_usages ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_token_usages FORCE ROW LEVEL SECURITY;

-- The platform admin reads every tenant's allocation and ledger, and is the
-- only role allowed to add tokens (top-ups and the initial grant).
CREATE POLICY token_balance_platform_admin ON tenant_token_balances FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));
CREATE POLICY token_balance_platform_admin_insert ON tenant_token_balances FOR INSERT
    WITH CHECK (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));
CREATE POLICY token_balance_platform_admin_update ON tenant_token_balances FOR UPDATE
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));
CREATE POLICY token_topup_platform_admin ON tenant_token_topups FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));
CREATE POLICY token_topup_platform_admin_insert ON tenant_token_topups FOR INSERT
    WITH CHECK (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));
CREATE POLICY token_usage_platform_admin ON tenant_token_usages FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));

-- A tenant member reads only their own tenant's allocation and ledger, and the
-- chat runtime debits the balance and appends usage inside that same scope.
CREATE POLICY token_balance_tenant_select ON tenant_token_balances FOR SELECT
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY token_balance_tenant_update ON tenant_token_balances FOR UPDATE
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY token_topup_tenant ON tenant_token_topups FOR SELECT
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY token_usage_tenant_select ON tenant_token_usages FOR SELECT
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY token_usage_tenant_insert ON tenant_token_usages FOR INSERT
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
