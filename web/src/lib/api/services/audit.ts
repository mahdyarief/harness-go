import { api } from '../client-fetch'
import type { AuditFilterDto } from '../dtos/platform/audit/schema'
import type { AuditLog } from '../models/audit'

type Envelope<T> = { data: T; metadata?: { total: number; offset: number; limit: number } }

/** Drop empty strings so the API never receives `?action=`. */
function clean(params: AuditFilterDto) {
  return Object.fromEntries(
    Object.entries(params).filter(([, value]) => value !== undefined && value !== ''),
  )
}

export const auditServices = {
  /** GET /api/v1/platform/audit-logs — super-user only, newest first. */
  async list(params: AuditFilterDto) {
    return (await api.get<Envelope<AuditLog[]>>('/api/v1/platform/audit-logs', {
      params: clean(params),
    })).data
  },

  /** GET /api/v1/platform/audit-logs/:id */
  async get(id: string) {
    return (
      await api.get<Envelope<AuditLog>>(
        `/api/v1/platform/audit-logs/${encodeURIComponent(id)}`,
      )
    ).data.data
  },
}
