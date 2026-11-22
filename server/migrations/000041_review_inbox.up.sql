CREATE TABLE review_items (
    id text PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    run_id text NOT NULL,
    subject text NOT NULL,
    result jsonb NOT NULL,
    schema jsonb,
    evidence jsonb NOT NULL DEFAULT '[]'::jsonb,
    reason text NOT NULL DEFAULT '',
    required_role text NOT NULL CHECK (required_role IN ('reviewer', 'tenant_admin')),
    status text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'DECIDED', 'EXPIRED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, run_id, subject)
);

CREATE TABLE review_decisions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    item_id text NOT NULL REFERENCES review_items(id),
    reviewer text NOT NULL,
    role text NOT NULL CHECK (role IN ('reviewer', 'tenant_admin')),
    result jsonb NOT NULL,
    overrides jsonb NOT NULL DEFAULT '[]'::jsonb,
    reason text NOT NULL DEFAULT '',
    at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX review_items_status_idx ON review_items (tenant_id, status, created_at);
CREATE INDEX review_decisions_item_idx ON review_decisions (item_id, at);

ALTER TABLE review_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_items FORCE ROW LEVEL SECURITY;
ALTER TABLE review_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_decisions FORCE ROW LEVEL SECURITY;

CREATE POLICY review_scope ON review_items USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY review_decision_scope ON review_decisions USING (item_id IN (SELECT id FROM review_items WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)) WITH CHECK (item_id IN (SELECT id FROM review_items WHERE tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid));
