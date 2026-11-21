import type { ApiItemResponse, ApiListResponse } from '@/types/api'

import { env } from '@/config/env'
import { activeTenantHeaders } from '@/lib/auth/active-tenant'
import { getStoredAccessToken } from '@/lib/auth/token-storage'

import type {
  ChatAccess,
  ChatMessage,
  ChatSession,
  ChatSessionDetail,
  ChatStreamEvent,
  ChatToolLog,
} from '../models/chat'
import type { ChatSessionTitleDto, ChatTurnDto, CreateChatSessionDto } from '../dtos/chat/schema'

import { api, refreshAccessToken, SIGN_IN_PATH } from '../client-fetch'
import { axiosErrorMessage } from '../axios-error'

const path = '/api/v1/chat/sessions'

/** Signals a failed stream or a non-streaming error payload. */
export class ChatStreamError extends Error {}

function readEnvelopeMessage(body: unknown): string {
  if (typeof body === 'object' && body !== null && 'message' in body) {
    const message = (body as { message?: unknown }).message
    if (typeof message === 'string' && message) return message
  }
  return 'Permintaan gagal diproses'
}

/**
 * Send one turn and consume the SSE response.
 *
 * This bypasses axios because axios cannot stream a response body in the
 * browser. The access token is read from the same cookie store the axios
 * interceptor uses, and a 401 triggers the same single-flight refresh before
 * retrying once. Pass an AbortSignal to stop the stream when the user leaves.
 */
async function streamTurn(
  sessionId: string,
  body: ChatTurnDto,
  onEvent: (event: ChatStreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const url = `${env.VITE_API_URL}${path}/${encodeURIComponent(sessionId)}/turn`

  const send = () => {
    const token = getStoredAccessToken()
    return fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...activeTenantHeaders(),
      },
      body: JSON.stringify(body),
      signal,
    })
  }

  let response = await send()
  if (response.status === 401) {
    try {
      await refreshAccessToken()
    } catch {
      window.location.assign(SIGN_IN_PATH)
      throw new ChatStreamError('Sesi berakhir; silakan masuk kembali.')
    }
    response = await send()
  }

  if (!response.ok) {
    let payload: unknown = null
    try {
      payload = await response.json()
    } catch {
      // A proxy error page is not JSON; the status line is all we know.
    }
    throw new ChatStreamError(readEnvelopeMessage(payload))
  }
  if (!response.body) {
    throw new ChatStreamError('Server tidak mengirimkan stream.')
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })

      // SSE frames are separated by a blank line and may be split across
      // network chunks, so only complete frames are parsed.
      let boundary = buffer.indexOf('\n\n')
      while (boundary !== -1) {
        const frame = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        const payload = frame
          .split('\n')
          .filter((line) => line.startsWith('data:'))
          .map((line) => line.slice(5).trimStart())
          .join('\n')
        if (payload) {
          let event: ChatStreamEvent
          try {
            event = JSON.parse(payload) as ChatStreamEvent
          } catch {
            throw new ChatStreamError('Format stream tidak dikenali.')
          }
          onEvent(event)
        }
        boundary = buffer.indexOf('\n\n')
      }
    }
  } finally {
    reader.releaseLock()
  }
}

export const chatServices = {
  access: () => api.get<ApiItemResponse<ChatAccess>>('/api/v1/chat/access'),
  list: (params?: Record<string, unknown>) =>
    api.get<ApiListResponse<ChatSession>>(path, { params }),
  get: (id: string) =>
    api.get<ApiItemResponse<ChatSessionDetail>>(`${path}/${encodeURIComponent(id)}`),
  messages: (id: string, params?: Record<string, unknown>) =>
    api.get<ApiListResponse<ChatMessage>>(`${path}/${encodeURIComponent(id)}/messages`, { params }),
  create: async (body: CreateChatSessionDto) => {
    const res = await api.post<ApiItemResponse<ChatSession>>(path, body)
    return res.data.data
  },
  rename: async (id: string, body: ChatSessionTitleDto) => {
    await api.put(`${path}/${encodeURIComponent(id)}`, body)
  },
  remove: async (id: string) => {
    await api.delete(`${path}/${encodeURIComponent(id)}`)
  },
  /** Harness debug log: one page of a session's tool calls, oldest first. */
  toolLogs: (id: string, params?: Record<string, unknown>) =>
    api.get<ApiListResponse<ChatToolLog>>(`${path}/${encodeURIComponent(id)}/tool-logs`, {
      params,
    }),
  /** Blocking turn; used when streaming is unavailable. */
  send: async (id: string, body: ChatTurnDto) => {
    try {
      const res = await api.post<ApiItemResponse<{ content: string; turns: number }>>(
        `${path}/${encodeURIComponent(id)}/turn`,
        { ...body, stream: false },
      )
      return res.data.data
    } catch (error) {
      throw new ChatStreamError(axiosErrorMessage(error))
    }
  },
  stream: streamTurn,
} as const
