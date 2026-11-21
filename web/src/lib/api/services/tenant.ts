import { api } from '../client-fetch'
import type { AssignMemberDto, CreateTenantDto } from '../dtos/tenant/schema'
import type {
  CreatedTenant,
  PlatformTenant,
  PlatformTenantGroup,
  TenantMember,
  TenantMembership,
} from '../models/tenant'

type Envelope<T> = { data: T }

export const tenantServices = {
  async list() {
    return (await api.get<Envelope<TenantMembership[]>>('/api/v1/tenants')).data.data
  },
  // Every tenant, not just the caller's memberships: the platform user drawer
  // offers this as the picker while assigning a tenant role.
  async platformList() {
    return (await api.get<Envelope<PlatformTenant[]>>('/api/v1/platform/tenants')).data.data
  },
  // Every tenant grouped with its members: the platform tenant page.
  async platformDirectory() {
    return (await api.get<Envelope<PlatformTenantGroup[]>>('/api/v1/platform/tenants/directory'))
      .data.data
  },
  async create(input: CreateTenantDto) {
    return (await api.post<Envelope<CreatedTenant>>('/api/v1/platform/tenants', input)).data.data
  },
  async members(tenantId: string) {
    return (
      await api.get<Envelope<TenantMember[]>>(
        `/api/v1/tenants/${encodeURIComponent(tenantId)}/members`,
      )
    ).data.data
  },
  async assignMember(tenantId: string, input: AssignMemberDto) {
    await api.put(`/api/v1/tenants/${encodeURIComponent(tenantId)}/members`, input)
  },
}
