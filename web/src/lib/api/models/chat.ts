import type { ISO8601DateString } from '@/types/time'

/** One conversation thread, mirroring server/internal/models/chat.go. */
export interface ChatSession {
  id: string
  tenant_id: string | null
  user_id: string
  title: string
  created_at: ISO8601DateString
  updated_at: ISO8601DateString
}

export type ChatRole = 'system' | 'user' | 'assistant' | 'tool'

/**
 * One knowledge passage the assistant used, recorded by the search tool itself
 * rather than parsed from the reply, so a citation always points at a real
 * chunk. `position` is the zero-based chunk order inside the document and is
 * what the preview uses to highlight the cited part.
 */
export interface MessageReference {
  document_id: string
  chunk_id: string
  title: string
  filename: string
  format: string
  locator: string
  heading_path: string
  position: number
  excerpt: string
}

/** One persisted turn in a session. */
export interface ChatMessage {
  id: string
  session_id: string
  role: ChatRole
  content: string
  tool_call_id?: string | null
  tool_name?: string | null
  finish_reason?: string | null
  references?: MessageReference[] | null
  created_at: ISO8601DateString
}

/**
 * A session plus only its newest page of messages, as returned by
 * GET /chat/sessions/:id. The rest of the transcript is paged backwards from
 * GET /chat/sessions/:id/messages.
 */
export interface ChatSessionDetail {
  session: ChatSession
  messages: ChatMessage[]
}

/**
 * One event from the server's SSE stream. `delta` carries incremental text,
 * `tool_start`/`tool_end` bracket a tool call, `compaction` reports that older
 * history was reclaimed to stay inside the context budget, `notice` carries a
 * harness advisory (for example, the model stalled and was nudged), `ping`
 * is a keep-alive the server sends while the model is thinking or running a
 * tool (it carries no payload), and `done` terminates the turn with its usage
 * metrics.
 */
export type ChatStreamEvent =
  | { type: 'delta'; text: string }
  | { type: 'tool_start'; tool_name: string; tool_call: string }
  | { type: 'tool_end'; tool_name: string; tool_call: string; error?: string; duration_ms?: number }
  | { type: 'compaction'; text: string }
  | { type: 'notice'; text: string }
  | { type: 'budget'; error?: string }
  | { type: 'ping' }
  | {
      type: 'done'
      turns?: number
      tokens?: number
      tool_calls?: number
      duration_ms?: number
      error?: string
      /**
       * The turn ended after the provider's stream was cut (no [DONE]), but
       * text had already been delivered to the user. The persisted answer is
       * partial, so the UI keeps it with a "terputus" note instead of treating
       * the turn as a hard failure.
       */
      partial?: boolean
    }

/** One tool invocation recorded during a turn, used as the harness debug log. */
export interface ChatToolLog {
  id: string
  session_id: string
  turn: number
  tool_name: string
  arguments: string
  result: string
  duration_ms: number
  error?: string | null
  created_at: ISO8601DateString
}

/**
 * Whether the caller can use chat right now. `no_tenant` means the account is
 * not assigned to a tenant; `no_allocation` means the tenant holds no tokens.
 */
export type ChatAccessStatus = 'ready' | 'no_tenant' | 'no_allocation'

export interface ChatAccess {
  status: ChatAccessStatus
  scope?: string
  /** Whether the server's web-search integration is enabled. */
  web_search: boolean
  /** Whether the knowledge-base tool can run for this chat scope. */
  knowledge: boolean
}
