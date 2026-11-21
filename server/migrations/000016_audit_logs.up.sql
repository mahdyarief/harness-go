-- Audit trail foundation: one append-only ledger for "who did what, when, to
-- which entity, from where". It is deliberately generic so new actions do not
-- need a schema change; action and entity_type are validated only for shape,
-- not against an enum.
--
-- The actor and tenant columns carry no foreign keys on purpose: an audit trail
-- must outlive the rows it describes, and a deleted user or tenant must not be
-- able to erase or block its own history.
CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_id uuid NOT NULL,
    actor_role text NOT NULL DEFAULT '' CHECK (length(actor_role) <= 40),
    action text NOT NULL CHECK (length(btrim(action)) BETWEEN 1 AND 100),
    entity_type text NOT NULL CHECK (length(btrim(entity_type)) BETWEEN 1 AND 100),
    entity_id uuid,
    tenant_id uuid,
    request_id text NOT NULL DEFAULT '' CHECK (length(request_id) <= 100),
    ip text NOT NULL DEFAULT '' CHECK (length(ip) <= 64),
    user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 512),
    -- Structured context only. Never prompts, credentials, tokens or raw
    -- request bodies; the size cap keeps a single row from becoming a payload
    -- store.
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(metadata) = 'object' AND pg_column_size(metadata) <= 8192)
);

CREATE INDEX audit_logs_tenant_time_idx ON audit_logs(tenant_id, occurred_at DESC, id DESC)
    WHERE tenant_id IS NOT NULL;
CREATE INDEX audit_logs_actor_time_idx ON audit_logs(actor_id, occurred_at DESC, id DESC);
CREATE INDEX audit_logs_entity_idx ON audit_logs(entity_type, entity_id);
CREATE INDEX audit_logs_time_idx ON audit_logs(occurred_at DESC, id DESC);

ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;

-- Reads: a platform admin sees everything; a tenant member sees only rows
-- scoped to the tenant pinned in the transaction. There is no UPDATE or DELETE
-- policy at all, so those commands are denied before the trigger below fires.
CREATE POLICY audit_log_select ON audit_logs FOR SELECT USING (
    EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    )
    OR (
        tenant_id IS NOT NULL
        AND tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
        AND EXISTS (
            SELECT 1 FROM tenant_memberships m
            WHERE m.user_id = nullif(current_setting('app.user_id', true), '')::uuid
              AND m.tenant_id = audit_logs.tenant_id
        )
    )
);

-- Writes: the actor must be the signed-in user, and a tenant-scoped row must
-- match the tenant pinned in the transaction. A platform admin may write a row
-- for any tenant (an administrative act on that tenant); a tenant member may
-- only write rows for their own tenant.
CREATE POLICY audit_log_insert ON audit_logs FOR INSERT WITH CHECK (
    actor_id = nullif(current_setting('app.user_id', true), '')::uuid
    AND (
        EXISTS (
            SELECT 1 FROM users u
            WHERE u.id = actor_id AND u.role = 'super_user'
        )
        OR (
            tenant_id IS NOT NULL
            AND tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
            AND EXISTS (
                SELECT 1 FROM tenant_memberships m
                WHERE m.user_id = actor_id AND m.tenant_id = audit_logs.tenant_id
            )
        )
    )
);

-- Belt and braces: even the owner and any future policy addition cannot edit
-- or remove history.
CREATE FUNCTION reject_audit_log_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only';
END;
$$;

CREATE TRIGGER audit_logs_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_log_mutation();
