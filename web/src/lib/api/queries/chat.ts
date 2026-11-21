import {
  infiniteQueryOptions,
  keepPreviousData,
  mutationOptions,
  queryOptions,
} from '@tanstack/react-query'

import { DEFAULT_PAGINATE } from '@/lib/constants/paginate'
import { getQueryClient } from '@/lib/providers/react-query'

import type { ChatStreamEvent } from '../models/chat'
import type { ChatSessionTitleDto, ChatTurnDto, CreateChatSessionDto } from '../dtos/chat/schema'

import { services } from '../services'

export const CHAT_QUERY_KEY = 'chat'

export const LIST_SESSION_QUERY_KEY = (params?: Record<string, unknown>) => [
  CHAT_QUERY_KEY,
  'sessions',
  'list',
  params,
]

export const GET_SESSION_QUERY_KEY = (id: string) => [CHAT_QUERY_KEY, 'sessions', 'detail', id]

export const ACCESS_QUERY_KEY = [CHAT_QUERY_KEY, 'access']

export const TOOL_LOGS_QUERY_KEY = (id: string, params?: Record<string, unknown>) => [
  CHAT_QUERY_KEY,
  'sessions',
  'tool-logs',
  id,
  params,
]

export const MESSAGES_QUERY_KEY = (id: string) => [CHAT_QUERY_KEY, 'sessions', 'messages', id]

/** Messages per page; the newest page loads first, older pages on scroll-up. */
export const MESSAGES_PAGE_SIZE = 30

const access = () =>
  queryOptions({
    queryKey: ACCESS_QUERY_KEY,
    queryFn: async () => {
      const res = await services.chat.access()
      return res.data.data
    },
  })

const list = (params?: Record<string, unknown>) =>
  queryOptions({
    queryKey: LIST_SESSION_QUERY_KEY(params),
    queryFn: async () => {
      const res = await services.chat.list(params ?? DEFAULT_PAGINATE)
      return res.data
    },
  })

const get = (id: string) =>
  queryOptions({
    queryKey: GET_SESSION_QUERY_KEY(id),
    queryFn: async () => {
      const res = await services.chat.get(id)
      return res.data.data
    },
  })

/**
 * One session's transcript, paged backwards from the newest message. Each page
 * arrives oldest-first and is prepended by the caller, so the array index grows
 * towards the start of the conversation as older pages are fetched.
 */
const messages = (id: string) =>
  infiniteQueryOptions({
    queryKey: MESSAGES_QUERY_KEY(id),
    enabled: Boolean(id),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const res = await services.chat.messages(id, {
        offset: pageParam,
        limit: MESSAGES_PAGE_SIZE,
      })
      return res.data
    },
    getNextPageParam: (lastPage) => {
      const loaded = lastPage.data.length
      const offset = lastPage.metadata?.offset ?? 0
      const total = lastPage.metadata?.total ?? 0
      const next = offset + loaded
      return loaded > 0 && next < total ? next : undefined
    },
  })

const toolLogs = (id: string, params?: Record<string, unknown>) =>
  queryOptions({
    queryKey: TOOL_LOGS_QUERY_KEY(id, params),
    queryFn: async () => {
      const res = await services.chat.toolLogs(id, params)
      return res.data
    },
    enabled: Boolean(id),
    placeholderData: keepPreviousData,
  })

const create = () => {
  const qc = getQueryClient()
  return mutationOptions({
    mutationFn: (body: CreateChatSessionDto) => services.chat.create(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [CHAT_QUERY_KEY] })
    },
  })
}

const rename = () => {
  const qc = getQueryClient()
  return mutationOptions({
    mutationFn: ({ id, body }: { id: string; body: ChatSessionTitleDto }) =>
      services.chat.rename(id, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [CHAT_QUERY_KEY] })
    },
  })
}

const remove = () => {
  const qc = getQueryClient()
  return mutationOptions({
    mutationFn: (id: string) => services.chat.remove(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [CHAT_QUERY_KEY] })
    },
  })
}

/**
 * Stream one turn. `onEvent` travels in the mutation variables rather than
 * being captured at option-factory time, so the callback never goes stale
 * between renders while a long stream is running.
 */
const sendTurn = () => {
  const qc = getQueryClient()
  return mutationOptions({
    mutationFn: async (vars: {
      sessionId: string
      body: ChatTurnDto
      onEvent: (event: ChatStreamEvent) => void
      signal?: AbortSignal
    }) => {
      await services.chat.stream(vars.sessionId, vars.body, vars.onEvent, vars.signal)
    },
    onSettled: (_data, _error, vars) => {
      // The live text was already rendered from the stream; refetch so the
      // persisted transcript (including tool rows) replaces it. Returning the
      // promises keeps the mutation pending until the transcript is back, so
      // the UI never flashes an empty thread between the two states.
      // The transcript is offset-paged from the newest message, so appending a
      // turn shifts every older page. Reset to the first page instead of
      // refetching stale offsets, which would duplicate or skip messages.
      return Promise.all([
        qc.invalidateQueries({ queryKey: GET_SESSION_QUERY_KEY(vars.sessionId) }),
        qc.resetQueries({ queryKey: MESSAGES_QUERY_KEY(vars.sessionId) }),
        qc.invalidateQueries({
          queryKey: [CHAT_QUERY_KEY, 'sessions', 'tool-logs', vars.sessionId],
        }),
        qc.invalidateQueries({ queryKey: [CHAT_QUERY_KEY, 'sessions', 'list'] }),
      ])
    },
  })
}

export const chatQueries = {
  access,
  list,
  get,
  messages,
  toolLogs,
  create,
  rename,
  remove,
  sendTurn,
} as const
