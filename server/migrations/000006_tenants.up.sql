CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 200),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE tenant_memberships (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    user_id uuid NOT NULL REFERENCES users(id),
    role text NOT NULL CHECK (role IN ('tenant_admin', 'reviewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);
CREATE INDEX tenant_memberships_user_idx ON tenant_memberships(user_id);
CREATE TABLE tenant_audit_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    subject_user_id uuid REFERENCES users(id),
    action text NOT NULL CHECK (action IN ('tenant_created', 'member_assigned')),
    member_role text CHECK (member_role IN ('tenant_admin', 'reviewer')),
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON tenants USING (id = nullif(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY membership_select ON tenant_memberships FOR SELECT USING (user_id = nullif(current_setting('app.user_id', true), '')::uuid OR tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY membership_insert ON tenant_memberships FOR INSERT WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY membership_update ON tenant_memberships FOR UPDATE USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY audit_scope ON tenant_audit_events USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
