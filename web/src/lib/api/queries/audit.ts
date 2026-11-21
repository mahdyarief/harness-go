import { queryOptions } from '@tanstack/react-query'

import type { AuditFilterDto } from '../dtos/platform/audit/schema'
import { auditServices } from '../services/audit'

export const AUDIT_QUERY_KEY = 'audit-logs'

export const auditKeys = {
  list: (params: AuditFilterDto) => [AUDIT_QUERY_KEY, 'list', params] as const,
  detail: (id: string) => [AUDIT_QUERY_KEY, 'detail', id] as const,
}

export const auditQueries = {
  list: (params: AuditFilterDto) =>
    queryOptions({
      queryKey: auditKeys.list(params),
      queryFn: () => auditServices.list(params),
      // The trail is append-only: a row never changes, so a short stale window
      // is enough and avoids refetching on every focus.
      staleTime: 30_000,
    }),

  detail: (id: string) =>
    queryOptions({
      queryKey: auditKeys.detail(id),
      queryFn: () => auditServices.get(id),
      enabled: Boolean(id),
      staleTime: 60_000,
    }),
}
