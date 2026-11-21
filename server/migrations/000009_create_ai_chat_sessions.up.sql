-- Chat sessions are tenant-scoped and user-scoped. A session belongs to exactly
-- one tenant and one user, and is never shared. RLS enforces the tenant scope
-- per-request via current_setting('app.tenant_id'). The user scope is enforced
-- by the user_id column and the session_select policy.

CREATE TABLE ai_chat_sessions (
    id          uuid        NOT NULL DEFAULT uuidv7() PRIMARY KEY,
    tenant_id   uuid        NOT NULL REFERENCES tenants(id),
    user_id     uuid        NOT NULL REFERENCES users(id),
    title       text        NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX ai_chat_sessions_tenant_idx ON ai_chat_sessions(tenant_id);
CREATE INDEX ai_chat_sessions_user_idx  ON ai_chat_sessions(user_id);

-- Messages belong to a session and are ordered by id. The role column carries
-- system/user/assistant/tool; tool_call_id links a tool-result message to the
-- assistant turn that requested it. Content may be empty for tool-result
-- messages or for assistant turns that only carry tool_calls.

CREATE TABLE ai_chat_messages (
    id              uuid        NOT NULL DEFAULT uuidv7() PRIMARY KEY,
    session_id      uuid        NOT NULL REFERENCES ai_chat_sessions(id) ON DELETE CASCADE,
    role            text        NOT NULL CHECK (role IN ('system','user','assistant','tool')),
    content         text        NOT NULL DEFAULT '',
    tool_call_id    text,
    tool_name       text,
    finish_reason   text,
    created_at      timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX ai_chat_messages_session_idx ON ai_chat_messages(session_id);

-- Token usage is informational only. The model may report different counts
-- than our own estimate; we record what the provider returned.

CREATE TABLE ai_chat_token_usage (
    id                  uuid        NOT NULL DEFAULT uuidv7() PRIMARY KEY,
    session_id          uuid        NOT NULL REFERENCES ai_chat_sessions(id) ON DELETE CASCADE,
    message_id         uuid        REFERENCES ai_chat_messages(id),
    prompt_tokens      integer     NOT NULL CHECK (prompt_tokens >= 0),
    completion_tokens  integer     NOT NULL CHECK (completion_tokens >= 0),
    total_tokens       integer     NOT NULL CHECK (total_tokens >= 0),
    created_at          timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX ai_chat_token_usage_session_idx ON ai_chat_token_usage(session_id);

-- Tool invocations are logged for audit. The arguments column is the raw JSON
-- the model sent; the result column is what the tool returned. Neither may be
-- NULL but both may be empty strings for tools that take or return nothing.

CREATE TABLE ai_chat_tool_logs (
    id          uuid        NOT NULL DEFAULT uuidv7() PRIMARY KEY,
    session_id  uuid        NOT NULL REFERENCES ai_chat_sessions(id) ON DELETE CASCADE,
    turn        integer     NOT NULL CHECK (turn >= 0),
    tool_name   text        NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 64),
    arguments   text        NOT NULL DEFAULT '',
    result      text        NOT NULL DEFAULT '',
    duration_ms integer     NOT NULL CHECK (duration_ms >= 0),
    error       text,
    created_at  timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX ai_chat_tool_logs_session_idx ON ai_chat_tool_logs(session_id);

-- RLS: every table is tenant-scoped. The session_select policy additionally
-- restricts reads to the owning user so a tenant_admin cannot read another
-- user's conversation. Tool logs and token usage inherit session isolation.

ALTER TABLE ai_chat_sessions     ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_sessions     FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_messages     ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_messages     FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_token_usage  ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_token_usage   FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_tool_logs     ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_chat_tool_logs     FORCE ROW LEVEL SECURITY;

-- Every command requires both the tenant and the owning user. Postgres ORs
-- permissive policies, so a single tenant-only policy would widen every other
-- one; keeping one policy per command avoids that.
CREATE POLICY session_select ON ai_chat_sessions FOR SELECT
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
           AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY session_insert ON ai_chat_sessions FOR INSERT
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY session_update ON ai_chat_sessions FOR UPDATE
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
           AND user_id = nullif(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY session_delete ON ai_chat_sessions FOR DELETE
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
           AND user_id = nullif(current_setting('app.user_id', true), '')::uuid);

CREATE POLICY message_select ON ai_chat_messages FOR SELECT
    USING (session_id IN (SELECT id FROM ai_chat_sessions
                          WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                            AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));
CREATE POLICY message_insert ON ai_chat_messages FOR INSERT
    WITH CHECK (session_id IN (SELECT id FROM ai_chat_sessions
                               WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                                 AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));
CREATE POLICY message_delete ON ai_chat_messages FOR DELETE
    USING (session_id IN (SELECT id FROM ai_chat_sessions
                          WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                            AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));

CREATE POLICY token_select ON ai_chat_token_usage FOR SELECT
    USING (session_id IN (SELECT id FROM ai_chat_sessions
                          WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                            AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));
CREATE POLICY token_insert ON ai_chat_token_usage FOR INSERT
    WITH CHECK (session_id IN (SELECT id FROM ai_chat_sessions
                               WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                                 AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));

CREATE POLICY tool_select ON ai_chat_tool_logs FOR SELECT
    USING (session_id IN (SELECT id FROM ai_chat_sessions
                          WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                            AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));
CREATE POLICY tool_insert ON ai_chat_tool_logs FOR INSERT
    WITH CHECK (session_id IN (SELECT id FROM ai_chat_sessions
                               WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
                                 AND user_id = nullif(current_setting('app.user_id', true), '')::uuid));
