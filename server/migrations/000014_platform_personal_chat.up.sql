ALTER TABLE ai_chat_sessions ALTER COLUMN tenant_id DROP NOT NULL;
DROP POLICY session_select ON ai_chat_sessions;
DROP POLICY session_insert ON ai_chat_sessions;
DROP POLICY session_update ON ai_chat_sessions;
DROP POLICY session_delete ON ai_chat_sessions;
CREATE POLICY session_personal ON ai_chat_sessions FOR ALL
USING (
    user_id = nullif(current_setting('app.user_id', true), '')::uuid
    AND (
        (tenant_id IS NULL
         AND nullif(current_setting('app.tenant_id', true), '') IS NULL
         AND EXISTS (SELECT 1 FROM users u WHERE u.id = ai_chat_sessions.user_id AND u.role = 'super_user'))
        OR (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
            AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = ai_chat_sessions.user_id AND u.role = 'super_user')
            AND EXISTS (SELECT 1 FROM tenant_memberships m
                        WHERE m.user_id = ai_chat_sessions.user_id AND m.tenant_id = ai_chat_sessions.tenant_id))
    )
);
DO $$
DECLARE item record;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('ai_chat_messages','message_select','SELECT'),
        ('ai_chat_messages','message_insert','INSERT'),
        ('ai_chat_messages','message_delete','DELETE'),
        ('ai_chat_token_usage','token_select','SELECT'),
        ('ai_chat_token_usage','token_insert','INSERT'),
        ('ai_chat_tool_logs','tool_select','SELECT'),
        ('ai_chat_tool_logs','tool_insert','INSERT')
    ) AS v(tbl,pol,cmd) LOOP
        EXECUTE format('DROP POLICY %I ON %I', item.pol, item.tbl);
        EXECUTE format('CREATE POLICY %I ON %I FOR %s %s (session_id IN (SELECT id FROM ai_chat_sessions))',
            item.pol, item.tbl, item.cmd, CASE WHEN item.cmd = 'INSERT' THEN 'WITH CHECK' ELSE 'USING' END);
    END LOOP;
END $$;
ALTER TABLE ai_chat_messages ADD CONSTRAINT ai_chat_messages_id_session_key UNIQUE (id, session_id);
ALTER TABLE ai_chat_token_usage ADD CONSTRAINT ai_chat_usage_message_session_fkey
    FOREIGN KEY (message_id, session_id) REFERENCES ai_chat_messages(id, session_id);

-- Tanpa FK agar penghapusan akun atau sesi tidak menghapus metering.
CREATE TABLE platform_chat_token_usages (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id uuid NOT NULL,
    session_id uuid NOT NULL,
    prompt_tokens int NOT NULL CHECK (prompt_tokens >= 0),
    completion_tokens int NOT NULL CHECK (completion_tokens >= 0),
    total_tokens int NOT NULL CHECK (total_tokens = prompt_tokens + completion_tokens AND total_tokens > 0),
    failed boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX platform_chat_token_usages_user_idx ON platform_chat_token_usages(user_id, created_at DESC);
ALTER TABLE platform_chat_token_usages ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_chat_token_usages FORCE ROW LEVEL SECURITY;
CREATE POLICY platform_usage_select ON platform_chat_token_usages FOR SELECT USING (
    user_id = nullif(current_setting('app.user_id', true), '')::uuid
    AND nullif(current_setting('app.tenant_id', true), '') IS NULL
    AND EXISTS (SELECT 1 FROM users u WHERE u.id = platform_chat_token_usages.user_id AND u.role = 'super_user')
);
CREATE POLICY platform_usage_insert ON platform_chat_token_usages FOR INSERT WITH CHECK (
    user_id = nullif(current_setting('app.user_id', true), '')::uuid
    AND nullif(current_setting('app.tenant_id', true), '') IS NULL
    AND EXISTS (SELECT 1 FROM users u WHERE u.id = platform_chat_token_usages.user_id AND u.role = 'super_user')
);
CREATE FUNCTION reject_platform_usage_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'platform usage is append-only';
END $$;
CREATE TRIGGER platform_usage_immutable BEFORE UPDATE OR DELETE OR TRUNCATE
    ON platform_chat_token_usages FOR EACH STATEMENT EXECUTE FUNCTION reject_platform_usage_mutation();
