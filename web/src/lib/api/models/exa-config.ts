/** Global Exa web-access configuration. API keys are write-only. */
export interface ExaConfig {
  enabled: boolean
  key_count: number
  has_keys: boolean
  updated_at: string
}

export type ExaTestResult = {
  status:
    | 'completed'
    | 'disabled'
    | 'failed'
    | 'timeout'
    | 'provider_auth_failed'
    | 'rate_limited'
    | 'invalid_response'
}
