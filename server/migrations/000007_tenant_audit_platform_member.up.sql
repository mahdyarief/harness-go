-- The platform admin can add a user to a tenant from the user drawer. That
-- write is not the same act as a tenant admin assigning a member: the actor
-- belongs to no tenant, so it gets its own action and keeps the super user in
-- actor_id. This only widens the allowed action set; existing rows are kept.
ALTER TABLE tenant_audit_events
    DROP CONSTRAINT IF EXISTS tenant_audit_events_action_check;
ALTER TABLE tenant_audit_events
    ADD CONSTRAINT tenant_audit_events_action_check
    CHECK (action IN ('tenant_created', 'member_assigned', 'member_assigned_by_platform'));
