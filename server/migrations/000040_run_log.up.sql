-- Run event log: one row per harness run plus an append-only event stream.
-- A run names the tenant that owns it and, for member runs, the parent run.
-- Profile/model/config hashes make the exact configuration reproducible from
-- the log; usage and cost are terminal aggregates updated by FinishRun.
CREATE TABLE runs (
    id                 uuid        NOT NULL DEFAULT uuidv7() PRIMARY KEY,
    tenant_id          uuid        NOT NULL REFERENCES tenants(id),
    parent_run_id      uuid        REFERENCES runs(id),
    profile_name       text        NOT NULL DEFAULT '' CHECK (length(profile_name) <= 200),
    profile_hash       text        NOT NULL DEFAULT '' CHECK (length(profile_hash) <= 128),
    model              text        NOT NULL DEFAULT '' CHECK (length(model) <= 200),
    config_hash        text        NOT NULL DEFAULT '' CHECK (length(config_hash) <= 128),
    status             text        NOT NULL DEFAULT 'running' CHECK (length(status) <= 40),
    prompt_tokens      integer     NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens  integer     NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    cost_usd           numeric(18, 6) NOT NULL DEFAULT 0,
    event_seq          bigint      NOT NULL DEFAULT 0,
    started_at         timestamptz NOT NULL DEFAULT now(),
    ended_at           timestamptz
);

CREATE INDEX runs_tenant_started_idx ON runs(tenant_id, started_at DESC, id DESC);
CREATE INDEX runs_parent_idx ON runs(parent_run_id) WHERE parent_run_id IS NOT NULL;

-- Events are immutable: no UPDATE, no DELETE, no TRUNCATE. Seq is contiguous
-- per run and assigned under the run row lock, so concurrent appends cannot
-- skip a number. The payload holds only typed structured data, never raw
-- prompts unless the caller explicitly enabled prompt logging.
CREATE TABLE run_events (
    run_id    uuid        NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    seq       bigint      NOT NULL CHECK (seq > 0),
    member    text        NOT NULL DEFAULT '' CHECK (length(member) <= 200),
    type      text        NOT NULL CHECK (length(btrim(type)) BETWEEN 1 AND 100),
    payload   jsonb       NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(payload) = 'object' AND pg_column_size(payload) <= 65536),
    at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);

CREATE INDEX run_events_type_idx ON run_events(run_id, type, seq);

ALTER TABLE runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE runs FORCE ROW LEVEL SECURITY;
ALTER TABLE run_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE run_events FORCE ROW LEVEL SECURITY;

-- Tenant isolation: reads and writes require the transaction's tenant GUC to
-- match the run's tenant. A member event must belong to a run in that tenant.
CREATE POLICY run_select ON runs FOR SELECT
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY run_insert ON runs FOR INSERT
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY run_update ON runs FOR UPDATE
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY run_event_select ON run_events FOR SELECT
    USING (run_id IN (SELECT id FROM runs
                      WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid));
CREATE POLICY run_event_insert ON run_events FOR INSERT
    WITH CHECK (run_id IN (SELECT id FROM runs
                           WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid));

-- Belt and braces: even the owner cannot edit or remove history.
CREATE FUNCTION reject_run_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'run_events is append-only';
END;
$$;

CREATE TRIGGER run_events_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON run_events
    FOR EACH STATEMENT EXECUTE FUNCTION reject_run_event_mutation();
