import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { IconChevronDown, IconChevronLeft, IconChevronRight, IconTool } from '@tabler/icons-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import type { ChatToolLog } from '@/lib/api/models/chat'
import { chatQueries } from '@/lib/api/queries/chat'
import { formatDateTime } from '@/lib/date'
import { cn } from '@/lib/utils'

/** Rows per page; ten keeps the drawer scannable without hiding the pager. */
const TOOL_LOG_PAGE_SIZE = 10

type ToolLogDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  sessionId: string
}

/**
 * Harness debug log for one session. Rows stay compact — tool, outcome, turn,
 * duration — and the raw arguments/result expand on demand, so a long history
 * is paged and only the detail someone opens is ever rendered in full.
 */
export function ToolLogDrawer({ open, onOpenChange, sessionId }: ToolLogDrawerProps) {
  if (!open) return null

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" size="xl" className="flex h-full flex-col gap-0 p-0">
        <SheetHeader className="border-line border-b">
          <SheetTitle>Log tool</SheetTitle>
          <SheetDescription>
            Setiap pemanggilan tool oleh agent pada percakapan ini. Buka satu baris untuk melihat
            argumen, hasil, durasi, dan galat secara lengkap.
          </SheetDescription>
        </SheetHeader>
        <ToolLogPanel key={sessionId} sessionId={sessionId} />
      </SheetContent>
    </Sheet>
  )
}

function ToolLogPanel({ sessionId }: { sessionId: string }) {
  const [offset, setOffset] = useState(0)

  const { data, isPending, isError, isFetching, isPlaceholderData, refetch } = useQuery(
    chatQueries.toolLogs(sessionId, { offset, limit: TOOL_LOG_PAGE_SIZE }),
  )

  const logs = data?.data ?? []
  const total = data?.metadata?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / TOOL_LOG_PAGE_SIZE))
  const currentPage = Math.floor(offset / TOOL_LOG_PAGE_SIZE) + 1
  const from = logs.length === 0 ? 0 : offset + 1
  const to = Math.min(offset + logs.length, total)

  return (
    <>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {isPending ? (
          <div className="space-y-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-control" />
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <p className="text-[13px] text-ink-2">Log tool gagal dimuat.</p>
            <Button type="button" variant="outline" size="sm" onClick={() => refetch()}>
              Coba lagi
            </Button>
          </div>
        ) : logs.length === 0 ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <span className="bg-field flex size-10 items-center justify-center rounded-control text-ink-2">
              <IconTool className="size-5" aria-hidden />
            </span>
            <p className="text-[13px] text-ink-2">
              Belum ada tool yang dipanggil pada percakapan ini.
            </p>
          </div>
        ) : (
          <ul
            className={cn(
              'divide-line bg-surface overflow-hidden rounded-card divide-y border border-line transition-opacity',
              isPlaceholderData && 'opacity-60',
            )}
          >
            {logs.map((log) => (
              <ToolLogRow key={log.id} log={log} />
            ))}
          </ul>
        )}
      </div>

      {total > 0 && (
        <div className="border-line flex items-center justify-between gap-3 border-t px-4 py-3">
          <p className="text-xs text-ink-2 tabular-nums">
            {from}–{to} dari {total}
          </p>
          <div className="flex items-center gap-1.5">
            <Button
              type="button"
              variant="outline"
              size="xs"
              mode="icon"
              aria-label="Halaman sebelumnya"
              disabled={currentPage <= 1 || isFetching}
              onClick={() => setOffset((prev) => Math.max(0, prev - TOOL_LOG_PAGE_SIZE))}
            >
              <IconChevronLeft aria-hidden />
            </Button>
            <span className="min-w-12 text-center text-xs text-ink-2 tabular-nums">
              {currentPage}/{pageCount}
            </span>
            <Button
              type="button"
              variant="outline"
              size="xs"
              mode="icon"
              aria-label="Halaman berikutnya"
              disabled={currentPage >= pageCount || isFetching}
              onClick={() => setOffset((prev) => prev + TOOL_LOG_PAGE_SIZE)}
            >
              <IconChevronRight aria-hidden />
            </Button>
          </div>
        </div>
      )}
    </>
  )
}

function ToolLogRow({ log }: { log: ChatToolLog }) {
  const [open, setOpen] = useState(false)
  const failed = Boolean(log.error)

  return (
    <li>
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            autoHeight
            className="hover:bg-hover-2 flex w-full items-center gap-3 rounded-none px-3 py-2.5 text-left"
          >
            <span
              className={cn(
                'flex size-7 shrink-0 items-center justify-center rounded-control',
                failed ? 'bg-red-tint text-red' : 'bg-green-tint text-green',
              )}
              aria-hidden
            >
              <IconTool className="size-4" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <span className="truncate font-mono text-[13px] font-medium text-ink">
                  {log.tool_name}
                </span>
                <Badge variant={failed ? 'destructive' : 'success'} appearance="light" size="sm">
                  {failed ? 'Gagal' : 'Berhasil'}
                </Badge>
              </span>
              <span className="block text-xs text-ink-2 tabular-nums">
                Giliran {log.turn} · {log.duration_ms} ms
              </span>
            </span>
            <IconChevronDown
              className={cn(
                'size-4 shrink-0 text-ink-3 transition-transform duration-200',
                open && 'rotate-180',
              )}
              aria-hidden
            />
          </Button>
        </CollapsibleTrigger>

        <CollapsibleContent>
          <dl className="divide-line border-line divide-y border-t">
            <LogBlock label="Argumen" value={log.arguments} />
            <LogBlock label="Hasil" value={log.result} />
            {log.error && <LogBlock label="Galat" value={log.error} error />}
            <div className="flex items-center justify-between px-3 py-2 text-xs text-ink-2">
              <span>Waktu</span>
              <span className="tabular-nums">{formatDateTime(log.created_at)}</span>
            </div>
          </dl>
        </CollapsibleContent>
      </Collapsible>
    </li>
  )
}

function LogBlock({ label, value, error }: { label: string; value: string; error?: boolean }) {
  const empty = value.trim() === ''

  return (
    <div className="px-3 py-2">
      <dt className="mb-1 text-xs text-ink-2">{label}</dt>
      <dd>
        {empty ? (
          <span className="text-xs text-ink-3">—</span>
        ) : (
          <pre
            className={cn(
              'bg-field max-h-56 overflow-auto rounded-control p-2 font-mono text-xs whitespace-pre-wrap break-words',
              error ? 'text-red' : 'text-ink',
            )}
          >
            {value}
          </pre>
        )}
      </dd>
    </div>
  )
}
