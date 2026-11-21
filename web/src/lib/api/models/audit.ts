/** One row of the append-only audit trail. */
export interface AuditLog {
  id: string
  occurred_at: string
  actor_id: string
  /** Empty when the actor's mirror row no longer exists. */
  actor_name?: string
  actor_role: string
  action: string
  entity_type: string
  entity_id: string | null
  tenant_id: string | null
  request_id: string
  ip: string
  user_agent: string
  metadata: Record<string, unknown>
}
