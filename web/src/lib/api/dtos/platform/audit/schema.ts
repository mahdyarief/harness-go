import z from 'zod'

import { optionalNumber } from '@/lib/validation'

/**
 * Filters for GET /api/v1/platform/audit-logs. Every field is optional; the
 * server validates UUIDs and timestamps again, this only keeps the URL clean.
 */
export const AuditFilterSchema = z.object({
  offset: optionalNumber('offset'),
  limit: optionalNumber('limit'),
  search: z.string().trim().max(100).optional(),
  action: z.string().trim().max(100).optional(),
  entity_type: z.string().trim().max(100).optional(),
  entity_id: z.string().uuid().optional().or(z.literal('')),
  actor_id: z.string().uuid().optional().or(z.literal('')),
  tenant_id: z.string().uuid().optional().or(z.literal('')),
  /** Plain date (YYYY-MM-DD) or RFC3339 timestamp. */
  from: z.string().trim().optional(),
  to: z.string().trim().optional(),
})

export type AuditFilterDto = z.infer<typeof AuditFilterSchema>
