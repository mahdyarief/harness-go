import { api } from '../client-fetch'
import type { TokenBalance, TokenTopup, TokenUsage } from '../models/token'
import type { PaginateDto } from '../dtos/paginate'
import type { TopUpDto } from '../dtos/token/schema'

type Envelope<T> = { data: T; metadata?: { total: number; offset: number; limit: number } }

export const tokenServices = {
  /** GET /api/v1/platform/tokens — all tenants' balances (platform admin only). */
  async listBalances() {
    return (await api.get<Envelope<TokenBalance[]>>('/api/v1/platform/tokens')).data.data
  },

  /** POST /api/v1/platform/tokens/:tenant_id/topups — grant tokens (platform admin only). */
  async topUp(tenantId: string, input: TopUpDto) {
    return (
      await api.post<Envelope<TokenBalance>>(
        `/api/v1/platform/tokens/${encodeURIComponent(tenantId)}/topups`,
        input,
      )
    ).data.data
  },

  /** GET /api/v1/tenants/:tenant_id/tokens — one tenant's balance. */
  async balance(tenantId: string) {
    return (
      await api.get<Envelope<TokenBalance>>(
        `/api/v1/tenants/${encodeURIComponent(tenantId)}/tokens`,
      )
    ).data.data
  },

  /** GET /api/v1/tenants/:tenant_id/tokens/topups — top-up history. */
  async topups(tenantId: string, params?: PaginateDto) {
    const url = `/api/v1/tenants/${encodeURIComponent(tenantId)}/tokens/topups`
    const query = params ? `?offset=${params.offset}&limit=${params.limit}` : ''
    return (await api.get<Envelope<TokenTopup[]>>(url + query)).data
  },

  /** GET /api/v1/tenants/:tenant_id/tokens/usages — usage history. */
  async usages(tenantId: string, params?: PaginateDto) {
    const url = `/api/v1/tenants/${encodeURIComponent(tenantId)}/tokens/usages`
    const query = params ? `?offset=${params.offset}&limit=${params.limit}` : ''
    return (await api.get<Envelope<TokenUsage[]>>(url + query)).data
  },
}
