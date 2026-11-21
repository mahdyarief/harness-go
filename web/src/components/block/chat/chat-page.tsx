import { useInfiniteQuery, useMutation, useQuery } from '@tanstack/react-query'
import { IconLayoutSidebarLeftExpand, IconListSearch } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useQueryState } from 'nuqs'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { axiosErrorMessage, toastAxiosError } from '@/lib/api/axios-error'
import { parseDto } from '@/lib/api/dtos/parse'
import { ChatTurnSchema } from '@/lib/api/dtos/chat/schema'
import type { ChatStreamEvent, MessageReference } from '@/lib/api/models/chat'
import { chatQueries } from '@/lib/api/queries/chat'
import { ReferencePreview } from './reference-preview'
import { authQueries } from '@/lib/api/queries/auth'
import { cn } from '@/lib/utils'
import ChatUnavailable from './chat-unavailable'
import LoadingState from './loading-state'
import Composer from './composer'
import MessageList, { type ToolActivity } from './message-list'
import SessionList from './session-list'
import { ToolLogDrawer } from './tool-log-drawer'

const LIST_PARAMS = { offset: 0, limit: 50 }

const CHAT_PANEL_KEY = 'hsgChatPanelCollapsed'

function readPanelCollapsed(): boolean {
  try {
    return window.localStorage.getItem(CHAT_PANEL_KEY) === 'true'
  } catch {
    return false
  }
}

function writePanelCollapsed(collapsed: boolean) {
  try {
    if (collapsed) window.localStorage.setItem(CHAT_PANEL_KEY, 'true')
    else window.localStorage.removeItem(CHAT_PANEL_KEY)
  } catch {
    // Persistence is optional; the panel state still works in memory.
  }
}

export default function ChatPage() {
  const profile = useQuery(authQueries.profile())
  const isSuperUser = profile.data?.data.role === 'super_user'
  const [sessionId, setSessionId] = useQueryState('session')
  const [streamingText, setStreamingText] = useState('')
  const [pendingUser, setPendingUser] = useState<{ id: string; content: string } | undefined>(
    undefined,
  )
  const [tools, setTools] = useState<ToolActivity[]>([])
  const [compacted, setCompacted] = useState('')
  const [notice, setNotice] = useState('')
  const [logOpen, setLogOpen] = useState(false)
  const [reference, setReference] = useState<MessageReference | null>(null)
  const [sessionsOpen, setSessionsOpen] = useState(false)
  const [sessionsCollapsed, setSessionsCollapsed] = useState(readPanelCollapsed)
  useEffect(() => {
    writePanelCollapsed(sessionsCollapsed)
  }, [sessionsCollapsed])
  const [useWebSearch, setUseWebSearch] = useState(false)
  const [useKnowledgeSearch, setUseKnowledgeSearch] = useState(true)
  const abortRef = useRef<AbortController | null>(null)

  const access = useQuery(chatQueries.access())
  const blockedStatus = access.data && access.data.status !== 'ready' ? access.data.status : null
  // Hold the session queries until access is known, so a blocked account never
  // triggers a request that the server is going to refuse. A failed probe falls
  // through to the normal error handling below.
  const accessSettled = access.data?.status === 'ready' || access.isError

  const sessions = useQuery({ ...chatQueries.list(LIST_PARAMS), enabled: accessSettled })
  const activeSession = useQuery({
    ...chatQueries.get(sessionId ?? ''),
    enabled: accessSettled && Boolean(sessionId),
  })
  const history = useInfiniteQuery({
    ...chatQueries.messages(sessionId ?? ''),
    enabled: accessSettled && Boolean(sessionId),
  })
  const createSession = useMutation(chatQueries.create())
  const removeSession = useMutation(chatQueries.remove())
  const renameSession = useMutation(chatQueries.rename())
  const sendTurn = useMutation(chatQueries.sendTurn())

  const sessionList = sessions.data?.data ?? []
  // Pages arrive newest-first, each page oldest-first, so reversing the page
  // order and flattening yields one chronological transcript.
  const messages = (history.data?.pages ?? [])
    .slice()
    .reverse()
    .flatMap((page) => page.data)
  const streaming = sendTurn.isPending
  const activeId = sessionId ?? null

  const handleCreate = async () => {
    try {
      const session = await createSession.mutateAsync({})
      void setSessionId(session.id)
      setSessionsOpen(false)
    } catch (error) {
      toastAxiosError(error)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      await removeSession.mutateAsync(id)
      if (sessionId === id) void setSessionId(null)
      return true
    } catch (error) {
      toastAxiosError(error)
      return false
    }
  }

  const handleRename = async (id: string, title: string) => {
    try {
      await renameSession.mutateAsync({ id, body: { title } })
      return true
    } catch (error) {
      toastAxiosError(error)
      return false
    }
  }

  const handleSelect = (id: string) => {
    abortRef.current?.abort()
    setStreamingText('')
    setTools([])
    void setSessionId(id)
    setSessionsOpen(false)
  }

  const handleSend = async (content: string) => {
    let targetId = sessionId

    // First turn in a fresh page: create the session on demand so the user
    // can start typing without a separate "new conversation" step.
    if (!targetId) {
      try {
        const session = await createSession.mutateAsync({})
        targetId = session.id
        void setSessionId(session.id)
      } catch (error) {
        toastAxiosError(error)
        return
      }
    }

    const parsed = parseDto(ChatTurnSchema, {
      content,
      // The toggle only exists when the server advertises the integration, so
      // a stale flag can never ask for a tool the server cannot offer.
      use_web_search: useWebSearch && (access.data?.web_search ?? false),
      use_knowledge_search: useKnowledgeSearch && (access.data?.knowledge ?? true),
    })
    const controller = new AbortController()
    abortRef.current = controller
    setStreamingText('')
    // Show the user's turn immediately, before the network round-trips, so the
    // prompt never waits on the stream to appear.
    setPendingUser({ id: `pending-${Date.now()}`, content })
    setTools([])
    setCompacted('')
    setNotice('')

    // Any text the user has already seen is persisted by the server, so a
    // later transport failure must not erase it from the screen.
    let hasText = false

    const onEvent = (event: ChatStreamEvent) => {
      if (event.type === 'delta') {
        hasText = true
        setStreamingText((prev) => prev + event.text)
        return
      }
      if (event.type === 'compaction') {
        setCompacted(event.text)
        return
      }
      if (event.type === 'notice') {
        setNotice(event.text)
        return
      }
      if (event.type === 'tool_start') {
        setTools((prev) => [
          ...prev,
          { callId: event.tool_call, name: event.tool_name, state: 'running' },
        ])
        return
      }
      if (event.type === 'tool_end') {
        setTools((prev) =>
          prev.map((tool) =>
            tool.callId === event.tool_call
              ? { ...tool, state: event.error ? 'error' : 'done' }
              : tool,
          ),
        )
        return
      }
      if (event.type === 'ping') {
        // Keep-alive while the model thinks or runs a tool; nothing to render.
        return
      }
      if (event.type === 'budget' && event.error) {
        toastAxiosError(new Error(event.error))
        return
      }
      if (event.type === 'done' && event.partial) {
        // The stream was cut mid-answer but text was already shown and saved.
        // Keep it on screen with a note rather than dropping it as a failure.
        setNotice('Jawaban terputus sebelum selesai. Sebagian jawaban tetap disimpan.')
        return
      }
      if (event.type === 'done' && event.error) {
        toastAxiosError(new Error(event.error))
      }
    }

    try {
      await sendTurn.mutateAsync({
        sessionId: targetId,
        body: parsed,
        onEvent,
        signal: controller.signal,
      })
    } catch (error) {
      // An abort is a user action, not a failure worth reporting.
      if ((error as Error)?.name !== 'AbortError') {
        toastAxiosError(error)
      }
      // On a transport failure after text has been delivered, the server has
      // already persisted the reply, so the text on screen must stay. Only
      // clear it when nothing was shown and the transcript has no reply.
      if (!hasText) {
        setStreamingText('')
      }
    } finally {
      abortRef.current = null
      setTools([])
      setPendingUser(undefined)
    }
  }

  const handleStop = () => abortRef.current?.abort()

  if (blockedStatus) return <ChatUnavailable status={blockedStatus} />
  if (access.isPending) {
    return (
      <div
        role="status"
        aria-label="Memeriksa akses chat"
        className="bg-surface shadow-card flex min-h-[20rem] flex-1 items-center justify-center rounded-card"
      >
        <LoadingState label="Memeriksa akses" />
      </div>
    )
  }

  return (
    <div className="bg-surface shadow-card flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-card md:flex-row">
      <SessionList
        sessions={sessionList}
        activeId={activeId}
        loading={sessions.isLoading}
        error={sessions.isError ? axiosErrorMessage(sessions.error) : undefined}
        creating={createSession.isPending}
        renaming={renameSession.isPending}
        onSelect={handleSelect}
        onCreate={handleCreate}
        onDelete={handleDelete}
        onRename={handleRename}
        onToggleCollapse={() => setSessionsCollapsed(true)}
        className={cn(
          'hidden border-r border-line md:w-60 lg:w-72',
          !sessionsCollapsed && 'md:flex',
        )}
      />

      <Sheet open={sessionsOpen} onOpenChange={setSessionsOpen}>
        <SheetContent
          side="left"
          showCloseButton={false}
          className="bg-inset w-[85vw] max-w-sm p-0"
        >
          <SheetTitle className="sr-only">Percakapan</SheetTitle>
          <SessionList
            sessions={sessionList}
            activeId={activeId}
            loading={sessions.isLoading}
            error={sessions.isError ? axiosErrorMessage(sessions.error) : undefined}
            creating={createSession.isPending}
            renaming={renameSession.isPending}
            onSelect={handleSelect}
            onCreate={handleCreate}
            onDelete={handleDelete}
            onRename={handleRename}
            onClose={() => setSessionsOpen(false)}
            className="h-full"
          />
        </SheetContent>
      </Sheet>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <header className="border-line flex items-start justify-between gap-3 border-b px-4 py-3">
          <div className="flex min-w-0 items-start gap-2">
            <Button
              type="button"
              variant="ghost"
              mode="icon"
              onClick={() => setSessionsOpen(true)}
              aria-label="Buka daftar percakapan"
              className="-ml-1.5 md:hidden"
            >
              <IconLayoutSidebarLeftExpand className="size-4" />
            </Button>
            {sessionsCollapsed && (
              <Button
                type="button"
                variant="ghost"
                mode="icon"
                onClick={() => setSessionsCollapsed(false)}
                aria-label="Tampilkan daftar percakapan"
                className="-ml-1.5 hidden md:inline-flex"
              >
                <IconLayoutSidebarLeftExpand className="size-4" />
              </Button>
            )}
            <div className="min-w-0">
              <h1 className="text-ink truncate text-[13px] font-medium">
                {activeSession.data?.session.title ?? 'Chat AI'}
              </h1>
              <p className="text-ink-2 hidden text-[12px] md:block">
                {isSuperUser
                  ? 'Akun platform · Akses data tetap mengikuti izin dan cakupan yang diberikan server.'
                  : 'Ruang kerja tenant · Asisten hanya mengakses data yang diizinkan untuk Anda.'}
              </p>
            </div>
          </div>
          {activeId && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setLogOpen(true)}
              className="rounded-chip shrink-0 md:h-7 md:px-2"
            >
              <IconListSearch className="size-3.5" />
              Log tool
            </Button>
          )}
        </header>

        {sessions.isError ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6">
            <p role="alert" className="text-sm text-red">
              {axiosErrorMessage(sessions.error)}
            </p>
            <Button
              variant="outline"
              disabled={sessions.isFetching}
              onClick={() => void sessions.refetch()}
            >
              Coba lagi
            </Button>
          </div>
        ) : history.isError ? (
          <div className="flex flex-1 items-center justify-center p-6">
            <p role="alert" className="text-sm text-red">
              Percakapan gagal dimuat.
            </p>
          </div>
        ) : (
          <MessageList
            key={activeId ?? 'new'}
            messages={messages}
            streamingText={streamingText}
            pendingUserMessage={pendingUser}
            tools={tools}
            compacted={compacted}
            notice={notice}
            streaming={streaming}
            loading={Boolean(sessionId) && history.isPending}
            hasOlder={Boolean(history.hasNextPage)}
            loadingOlder={history.isFetchingNextPage}
            onLoadOlder={() => void history.fetchNextPage()}
            onReferenceClick={setReference}
          />
        )}

        <Composer
          disabled={sessions.isLoading || sessions.isError || history.isLoading || history.isError}
          streaming={streaming}
          webSearch={useWebSearch}
          webSearchAvailable={access.data?.web_search ?? false}
          onWebSearchChange={setUseWebSearch}
          knowledgeSearch={useKnowledgeSearch}
          onKnowledgeSearchChange={setUseKnowledgeSearch}
          onSend={handleSend}
          onStop={handleStop}
        />
      </div>

      <ToolLogDrawer open={logOpen} onOpenChange={setLogOpen} sessionId={activeId ?? ''} />

      <ReferencePreview
        open={reference !== null}
        onOpenChange={(open) => {
          if (!open) setReference(null)
        }}
        reference={reference}
      />
    </div>
  )
}
