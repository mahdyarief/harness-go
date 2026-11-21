-- The platform admin assigns tenant roles from the user drawer, so a super
-- user must be able to read every tenant and membership. The 000006 policies
-- only expose rows reachable through app.tenant_id / app.user_id; these
-- permissive SELECT policies add the platform-admin case and OR with them.
-- The check reads users.role, which the tenant role may SELECT but not modify.
CREATE POLICY tenant_platform_admin ON tenants FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));

CREATE POLICY membership_platform_admin ON tenant_memberships FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = nullif(current_setting('app.user_id', true), '')::uuid
          AND u.role = 'super_user'
    ));
