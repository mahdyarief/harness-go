import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { BlurFade } from '@/components/ui/blur-fade'
import { ProgressiveBlur } from '@/components/ui/progressive-blur'
import { Skeleton } from '@/components/ui/skeleton'
import type { ChatMessage, MessageReference } from '@/lib/api/models/chat'
import { cn } from '@/lib/utils'

import LoadingState from './loading-state'
import Markdown from './markdown'
import ToolChips from './tool-chips'
import ReferenceChips from './reference-chips'

export interface ToolActivity {
  callId: string
  name: string
  state: 'running' | 'done' | 'error'
}

interface MessageListProps {
  messages: ChatMessage[]
  streamingText: string
  tools: ToolActivity[]
  /** User's message sent optimistically before the server acknowledges it. */
  pendingUserMessage?: { id: string; content: string }
  /** Set when the harness dropped older history to fit the context window. */
  compacted?: string
  /** Advisory from the harness, e.g. it nudged a stalled model to continue. */
  notice?: string
  streaming: boolean
  /** The first page is still loading. */
  loading: boolean
  /** Older pages exist above the loaded window. */
  hasOlder: boolean
  /** An older page is currently in flight. */
  loadingOlder: boolean
  onLoadOlder: () => void
  /** Open the per-document preview for a cited passage. */
  onReferenceClick?: (reference: MessageReference) => void
}

const EASE = 'cubic-bezier(0.23, 1, 0.32, 1)'
/** How close to the bottom still counts as "following" the conversation. */
const STICK_THRESHOLD = 80
/** Messages past this index load together instead of rippling in one by one. */
const STAGGER_LIMIT = 6

/** Blur ramp for the fade over the top edge; the mask math assumes 8 levels. */
const TOP_BLUR_LEVELS = [0.25, 0.5, 1, 2, 3, 4, 6, 8]

/** Placeholder transcript shown while the first page of a session loads. */
function TranscriptSkeleton() {
  return (
    <div role="status" aria-label="Memuat percakapan" className="flex flex-col gap-5 py-2">
      <div className="flex flex-col items-end gap-1">
        <Skeleton className="h-3 w-8" />
        <Skeleton className="rounded-card h-10 w-2/5" />
      </div>
      <div className="flex flex-col items-start gap-2">
        <Skeleton className="h-3 w-12" />
        <Skeleton className="h-3.5 w-11/12" />
        <Skeleton className="h-3.5 w-4/5" />
        <Skeleton className="h-3.5 w-3/5" />
      </div>
      <div className="flex flex-col items-end gap-1">
        <Skeleton className="h-3 w-8" />
        <Skeleton className="rounded-card h-10 w-1/2" />
      </div>
      <div className="flex flex-col items-start gap-2">
        <Skeleton className="h-3 w-12" />
        <Skeleton className="h-3.5 w-3/4" />
        <Skeleton className="h-3.5 w-2/3" />
      </div>
    </div>
  )
}

export default function MessageList({
  messages,
  streamingText,
  tools,
  pendingUserMessage,
  compacted,
  notice,
  streaming,
  loading,
  hasOlder,
  loadingOlder,
  onLoadOlder,
  onReferenceClick,
}: MessageListProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const endRef = useRef<HTMLDivElement>(null)
  const topRef = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)
  const firstId = useRef<string | null>(null)
  const lastId = useRef<string | null>(null)
  const lastHeight = useRef(0)
  const loadOlderRef = useRef(onLoadOlder)
  // Once this conversation has streamed a reply, later messages are not
  // revealed: the saved reply replaces the streamed text in place, and fading
  // it in would flash the text that was already on screen.
  const [live, setLive] = useState(false)
  if (streaming && !live) setLive(true)

  useEffect(() => {
    loadOlderRef.current = onLoadOlder
  }, [onLoadOlder])

  const visible = messages.filter((m) => m.role === 'user' || m.role === 'assistant')
  // Append the in-flight user message optimistically so it shows up instantly
  // while the server streams the reply.
  const withPending = useMemo(() => {
    if (!pendingUserMessage) return visible
    // The optimistic turn is always the newest one, so it is only already
    // persisted when the transcript's tail is this exact user message (followed
    // at most by its reply). Matching anywhere in history would hide a prompt
    // the user repeated verbatim in an earlier turn.
    let lastUser = -1
    for (let i = visible.length - 1; i >= 0; i -= 1) {
      if (visible[i].role === 'user') {
        lastUser = i
        break
      }
    }
    const persisted =
      lastUser !== -1 &&
      visible[lastUser].content === pendingUserMessage.content &&
      visible.slice(lastUser + 1).every((m) => m.role === 'assistant')
    if (persisted) return visible
    return [
      ...visible,
      {
        id: pendingUserMessage.id,
        role: 'user' as const,
        content: pendingUserMessage.content,
        created_at: new Date().toISOString(),
      } as ChatMessage,
    ]
  }, [visible, pendingUserMessage])

  // Keep the viewport anchored when older pages are prepended, and follow the
  // bottom when the user is already there. `lastHeight` is captured after the
  // previous paint, so the delta is exactly the height that was inserted above.
  useLayoutEffect(() => {
    const el = containerRef.current
    if (!el) return
    const nextFirstId = withPending[0]?.id ?? null
    const nextLast = withPending[withPending.length - 1]
    const nextLastId = nextLast?.id ?? null
    const prepended = firstId.current !== null && nextFirstId !== firstId.current
    // Sending a message should always bring the conversation back into view,
    // even if the user had scrolled up to read history.
    const sentByUser =
      nextLastId !== lastId.current && lastId.current !== null && nextLast?.role === 'user'

    if (sentByUser) {
      el.scrollTop = el.scrollHeight
      stickToBottom.current = true
    } else if (prepended) {
      el.scrollTop += el.scrollHeight - lastHeight.current
    } else if (firstId.current === null || stickToBottom.current) {
      el.scrollTop = el.scrollHeight
      stickToBottom.current = true
    }

    firstId.current = nextFirstId
    lastId.current = nextLastId
    lastHeight.current = el.scrollHeight
  }, [withPending])

  // Keep the newest text in view during streaming without yanking the user
  // back down if they scrolled up to read history. Pinning in a layout effect
  // (before paint) avoids the post-paint jump that scrollIntoView produces
  // while react-markdown re-lays-out each delta, which read as a shake.
  useLayoutEffect(() => {
    if (!streaming || !stickToBottom.current) return
    const el = containerRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [streamingText, tools.length, streaming])

  // Fetch the next older page when the top sentinel approaches the viewport.
  useEffect(() => {
    const sentinel = topRef.current
    const root = containerRef.current
    if (!sentinel || !root || !hasOlder) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) loadOlderRef.current()
      },
      { root, rootMargin: '240px 0px 0px 0px' },
    )
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [hasOlder, loadingOlder])

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={containerRef}
        onScroll={() => {
          const el = containerRef.current
          if (!el) return
          stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_THRESHOLD
        }}
        role="log"
        aria-label="Transkrip percakapan"
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 py-5 sm:px-4 sm:py-6"
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-4 sm:gap-5 lg:gap-6">
          <div ref={topRef} aria-hidden className="h-px w-full" />
          {loadingOlder && (
            <div role="status" className="flex items-center justify-center gap-2">
              <span
                aria-hidden
                className="bg-ink-3 size-1.5 rounded-full"
                style={{ animation: 'pixel-on 650ms ease-in-out infinite' }}
              />
              <span
                className="bg-clip-text text-[11px] font-medium text-transparent"
                style={{
                  backgroundImage:
                    'linear-gradient(90deg, var(--ink-3) 35%, var(--ink) 50%, var(--ink-3) 65%)',
                  backgroundSize: '200% 100%',
                  animation: 'shimmer-text 1.4s linear infinite',
                }}
              >
                Memuat pesan lama
              </span>
            </div>
          )}
          {!loading && !hasOlder && visible.length > 0 && (
            <p className="text-ink-3 text-center text-[11px]">Awal percakapan</p>
          )}

          {loading && withPending.length === 0 && !streaming && <TranscriptSkeleton />}

          {!loading && withPending.length === 0 && !streaming && (
            <div
              className="mx-auto max-w-sm py-16 text-center"
              style={{ animation: `fade-up 450ms ${EASE} both` }}
            >
              <h2 className="text-ink mb-2 text-lg font-semibold tracking-tight">
                Apa yang ingin Anda telusuri?
              </h2>
              <p className="text-ink-2 text-[13px] leading-6">
                Mulai percakapan. Asisten dapat membaca data aplikasi yang diizinkan.
              </p>
            </div>
          )}

          {withPending.map((message, index) => {
            const article = (
              <article
                className={cn(
                  'flex flex-col gap-1',
                  message.role === 'user' ? 'items-end' : 'items-start',
                )}
              >
                <span className="text-ink-3 px-1 text-[11px] font-medium">
                  {message.role === 'user' ? 'Anda' : 'Asisten'}
                </span>
                <div
                  className={cn(
                    'max-w-[90%] break-words text-[13px] leading-relaxed',
                    message.role === 'user'
                      ? 'bg-field text-ink whitespace-pre-wrap rounded-card px-3.5 py-2.5 shadow-hairline'
                      : 'text-ink px-1',
                  )}
                >
                  {message.role === 'user' ? (
                    message.content
                  ) : (
                    <Markdown>{message.content}</Markdown>
                  )}
                </div>
                {message.role === 'assistant' && message.finish_reason === 'error' && (
                  <p className="text-ink-3 px-1 text-[11px]">
                    Jawaban terputus sebelum selesai; hanya sebagian jawaban tersimpan.
                  </p>
                )}
                {message.role === 'assistant' &&
                  message.references &&
                  message.references.length > 0 && (
                    <ReferenceChips references={message.references} onSelect={onReferenceClick} />
                  )}
              </article>
            )
            return live ? (
              <div key={message.id}>{article}</div>
            ) : (
              <BlurFade key={message.id} delay={Math.min(index, STAGGER_LIMIT) * 0.04}>
                {article}
              </BlurFade>
            )
          })}

          {tools.length > 0 && <ToolChips tools={tools} />}

          {compacted ? (
            <p className="text-ink-3 px-1 text-[11px]">
              Konteks lama diringkas agar percakapan tetap berjalan — {compacted}.
            </p>
          ) : null}

          {notice ? <p className="text-ink-3 px-1 text-[11px]">{notice}</p> : null}

          {streaming && (
            <article className="flex flex-col items-start gap-1">
              <span className="text-ink-3 px-1 text-[11px] font-medium">Asisten</span>
              {streamingText ? (
                <div
                  className="text-ink max-w-[90%] break-words px-1 text-[13px] leading-relaxed"
                  aria-live="polite"
                  aria-atomic="false"
                >
                  <Markdown>{streamingText}</Markdown>
                  <span
                    aria-hidden
                    className="bg-ink ml-0.5 inline-block h-3 w-0.5 translate-y-0.5 rounded-full"
                    style={{ animation: 'fade-in 150ms ease-out both' }}
                  />
                </div>
              ) : (
                <div className="px-1 py-1">
                  <LoadingState
                    label={
                      tools.some((t) => t.state === 'running') ? 'Menelusuri data' : 'Berpikir'
                    }
                  />
                </div>
              )}
            </article>
          )}

          <div ref={endRef} />
        </div>
      </div>
      {(hasOlder || loadingOlder) && (
        <ProgressiveBlur position="top" height="2.5rem" blurLevels={TOP_BLUR_LEVELS} />
      )}
    </div>
  )
}
