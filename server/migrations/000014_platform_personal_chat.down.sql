-- Rollback tidak boleh menghapus percakapan atau bukti pemakaian platform.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM ai_chat_sessions WHERE tenant_id IS NULL)
       OR EXISTS (SELECT 1 FROM platform_chat_token_usages) THEN
        RAISE EXCEPTION 'platform chat data must be archived before rollback';
    END IF;
END $$;
DROP TABLE platform_chat_token_usages;
DROP FUNCTION reject_platform_usage_mutation();
ALTER TABLE ai_chat_token_usage DROP CONSTRAINT ai_chat_usage_message_session_fkey;
ALTER TABLE ai_chat_messages DROP CONSTRAINT ai_chat_messages_id_session_key;
DROP POLICY session_personal ON ai_chat_sessions;
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
        EXECUTE format('CREATE POLICY %I ON %I FOR %s %s (session_id IN (SELECT id FROM ai_chat_sessions WHERE tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid AND user_id = nullif(current_setting(''app.user_id'', true), '''')::uuid))',
            item.pol, item.tbl, item.cmd, CASE WHEN item.cmd = 'INSERT' THEN 'WITH CHECK' ELSE 'USING' END);
    END LOOP;
END $$;
ALTER TABLE ai_chat_sessions ALTER COLUMN tenant_id SET NOT NULL;
CREATE POLICY session_select ON ai_chat_sessions FOR SELECT USING (
    tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY session_insert ON ai_chat_sessions FOR INSERT WITH CHECK (
    tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY session_update ON ai_chat_sessions FOR UPDATE USING (
    tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    AND user_id = nullif(current_setting('app.user_id', true), '')::uuid)
WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY session_delete ON ai_chat_sessions FOR DELETE USING (
    tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
    AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
