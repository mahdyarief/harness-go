export interface TokenBalance {
  tenant_id: string
  /** Present only on the platform list, which joins tenants. */
  tenant_name?: string
  balance: number
  updated_at: string
}

export interface TokenTopup {
  id: string
  tenant_id: string
  amount: number
  note: string
  actor_id: string
  actor_name: string
  actor_email: string
  reason: string
  created_at: string
}

export interface TokenUsage {
  id: string
  tenant_id: string
  user_id: string
  user_name: string
  user_email: string
  session_id?: string
  message_id?: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  billed_tokens: number
  created_at: string
}
