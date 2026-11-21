-- Knowledge usage log: append-only events that let us measure how much the
-- semantic (embedding) index is actually contributing versus plain lexical
-- search, and which documents earn their place in the knowledge base.
--
-- One row per (document, chunk) offered to the model as a search result
-- ('retrieved'), and one row per document the final answer actually cited
-- ('cited'). The source column records whether the chunk came from the lexical
-- branch, the semantic branch, or both, so embedding contribution is directly
-- answerable from the data.
--
-- tenant_id is the chat scope that produced the event, not the document owner:
-- a global document retrieved inside a tenant chat is logged against that
-- tenant, so each tenant's dashboard describes its own usage.
CREATE TABLE knowledge_document_usage_log (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id uuid REFERENCES tenants(id) ON DELETE CASCADE,
    document_id uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
    chunk_id uuid REFERENCES knowledge_chunks(id) ON DELETE SET NULL,
    event text NOT NULL CHECK (event IN ('retrieved', 'cited')),
    source text NOT NULL CHECK (source IN ('lexical', 'semantic', 'both')),
    session_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX knowledge_document_usage_log_document_idx ON knowledge_document_usage_log(document_id, created_at DESC);
CREATE INDEX knowledge_document_usage_log_tenant_idx ON knowledge_document_usage_log(tenant_id, created_at DESC);
CREATE INDEX knowledge_document_usage_log_session_idx ON knowledge_document_usage_log(session_id) WHERE session_id IS NOT NULL;

ALTER TABLE knowledge_document_usage_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE knowledge_document_usage_log FORCE ROW LEVEL SECURITY;

-- Reads follow the knowledge base rules: a member sees global and own-tenant
-- events, a super_user sees global events.
CREATE POLICY knowledge_document_usage_log_select ON knowledge_document_usage_log FOR SELECT
    USING (knowledge_can_read(tenant_id));

-- Appends are open to any member of the pinned tenant (not just admins): a
-- regular member's chat is exactly the traffic the metrics must capture.
-- Global events stay restricted to super_user.
CREATE FUNCTION knowledge_can_log_usage(row_tenant uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT CASE
        WHEN row_tenant IS NULL THEN EXISTS (
            SELECT 1 FROM users u
            WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
              AND u.role = 'super_user'
        )
        ELSE row_tenant = nullif(current_setting('app.tenant_id', true), '')::uuid
            AND EXISTS (
                SELECT 1 FROM tenant_memberships m
                WHERE m.user_id = nullif(current_setting('app.user_id', true), '')::uuid
                  AND m.tenant_id = row_tenant
            )
    END
$$;

CREATE POLICY knowledge_document_usage_log_insert ON knowledge_document_usage_log FOR INSERT
    WITH CHECK (knowledge_can_log_usage(tenant_id));

GRANT EXECUTE ON FUNCTION knowledge_can_log_usage(uuid) TO hsg_tenant;
