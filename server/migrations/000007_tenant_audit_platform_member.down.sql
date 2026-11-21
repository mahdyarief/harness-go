-- Restore the narrower action set; rows written with the platform action
-- cannot survive it, so they are removed first.
DELETE FROM tenant_audit_events WHERE action = 'member_assigned_by_platform';
ALTER TABLE tenant_audit_events
    DROP CONSTRAINT IF EXISTS tenant_audit_events_action_check;
ALTER TABLE tenant_audit_events
    ADD CONSTRAINT tenant_audit_events_action_check
    CHECK (action IN ('tenant_created', 'member_assigned'));