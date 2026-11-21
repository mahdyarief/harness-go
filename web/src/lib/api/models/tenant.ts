import type { TenantRole } from '../dtos/tenant/schema'

export interface TenantMembership {
  id: string
  name: string
  role: TenantRole
}
/** GET /api/v1/tenants/:id/members: a membership with the user's identity. */
export interface TenantMember {
  user_id: string
  name: string
  email: string
  role: TenantRole
  created_at: string
}
export interface CreatedTenant {
  id: string
  name: string
}

/** GET /api/v1/platform/tenants: every tenant, for the platform admin's pickers. */
export interface PlatformTenant {
  id: string
  name: string
}

export interface PlatformTenantMember {
  user_id: string
  name: string
  email: string
  role: TenantRole
  created_at: string
}

/** GET /api/v1/platform/tenants/directory: every tenant grouped with its members. */
export interface PlatformTenantGroup {
  id: string
  name: string
  created_at: string
  members: PlatformTenantMember[]
}
