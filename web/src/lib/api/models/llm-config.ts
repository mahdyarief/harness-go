/** Read model: credentials are never returned by the API. */
export interface LlmConfig {
  enabled: boolean
  endpoint: string
  model: string
  language: string
  timeout_seconds: number
  max_tokens: number
  has_credential: boolean
  updated_at: string
}

/** Only a non-sensitive summary may enter query state, never evidence or quotes. */
export interface LlmTestResult {
  status:
    | 'completed'
    | 'disabled'
    | 'failed'
    | 'timeout'
    | 'provider_auth_failed'
    | 'rate_limited'
    | 'invalid_response'
}
