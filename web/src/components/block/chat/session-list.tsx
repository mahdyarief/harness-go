import {
  IconCheck,
  IconLayoutSidebarLeftCollapse,
  IconMessage,
  IconPencil,
  IconPlus,
  IconTrash,
  IconX,
} from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'

import SimpleAlertDialog from '@/components/block/common/simple-alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import type { ChatSession } from '@/lib/api/models/chat'
import { cn } from '@/lib/utils'

interface SessionListProps {
  sessions: ChatSession[]
  activeId: string | null
  loading: boolean
  error?: string
  creating: boolean
  renaming: boolean
  onSelect: (id: string) => void
  onCreate: () => void
  onDelete: (id: string) => Promise<boolean>
  onRename: (id: string, title: string) => Promise<boolean>
  /** Renders an inline close control; used by the mobile drawer only. */
  onClose?: () => void
  /** Desktop/medium collapse control; omitted inside the mobile drawer. */
  onToggleCollapse?: () => void
  className?: string
}

export default function SessionList({
  sessions,
  activeId,
  loading,
  error,
  creating,
  renaming,
  onSelect,
  onCreate,
  onDelete,
  onRename,
  className,
  onClose,
  onToggleCollapse,
}: SessionListProps) {
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [pendingDelete, setPendingDelete] = useState<ChatSession | null>(null)
  const [deleting, setDeleting] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (editingId) inputRef.current?.select()
  }, [editingId])

  const startEditing = (session: ChatSession) => {
    setEditingId(session.id)
    setDraft(session.title)
  }

  const cancelEditing = () => {
    setEditingId(null)
    setDraft('')
  }

  const commitEditing = async (id: string) => {
    const title = draft.trim()
    if (!title) {
      cancelEditing()
      return
    }
    // Keep the field open when the save failed so the draft is not lost.
    if (await onRename(id, title)) cancelEditing()
  }

  return (
    <aside
      className={cn('bg-inset flex min-h-0 min-w-0 flex-col overflow-hidden', className)}
      aria-label="Daftar percakapan"
    >
      <div className="flex items-center justify-between gap-2 px-3 py-3">
        <div className="flex min-w-0 items-center gap-1">
          {onToggleCollapse ? (
            <Button
              type="button"
              variant="ghost"
              mode="icon"
              size="xs"
              onClick={onToggleCollapse}
              aria-label="Sembunyikan daftar percakapan"
              className="hidden shrink-0 md:inline-flex"
            >
              <IconLayoutSidebarLeftCollapse className="size-3.5" />
            </Button>
          ) : null}
          <h2 className="text-ink-2 truncate text-[12px] font-medium">Percakapan</h2>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onCreate}
            disabled={creating || loading || Boolean(error)}
            aria-label="Percakapan baru"
            className="rounded-chip h-9 md:h-7 md:px-2"
          >
            <IconPlus className="size-3.5" />
            Baru
          </Button>
          {onClose ? (
            <Button
              type="button"
              variant="ghost"
              mode="icon"
              size="icon"
              onClick={onClose}
              aria-label="Tutup daftar percakapan"
              className="md:hidden"
            >
              <IconX className="size-4" />
            </Button>
          ) : null}
        </div>
      </div>

      <ScrollArea className="min-w-0 flex-1" viewportClassName="[&>div]:w-full [&>div]:table-fixed">
        <div className="min-w-0 p-2">
          {loading && (
            <div className="space-y-2 p-1">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-9 w-full" />
              ))}
            </div>
          )}

          {!loading && error && (
            <p className="px-2 py-6 text-center text-sm text-red">
              Daftar percakapan gagal dimuat.
            </p>
          )}
          {!loading && !error && sessions.length === 0 && (
            <p className="px-2 py-6 text-center text-sm text-ink-2">Belum ada percakapan.</p>
          )}

          <ul className="min-w-0 space-y-1">
            {sessions.map((session) => {
              const active = session.id === activeId
              const editing = editingId === session.id
              return (
                <li key={session.id} className="group relative min-w-0">
                  {editing ? (
                    <form
                      className="flex min-w-0 items-center gap-1 px-1.5 py-1.5"
                      onSubmit={(event) => {
                        event.preventDefault()
                        void commitEditing(session.id)
                      }}
                    >
                      <Input
                        ref={inputRef}
                        value={draft}
                        onChange={(event) => setDraft(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === 'Escape') cancelEditing()
                        }}
                        maxLength={200}
                        aria-label={`Judul percakapan ${session.title}`}
                        className="h-8 min-w-0 flex-1 px-2 text-sm"
                      />
                      <Button
                        type="submit"
                        variant="ghost"
                        size="sm"
                        mode="icon"
                        disabled={renaming}
                        aria-label="Simpan judul"
                      >
                        <IconCheck />
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        mode="icon"
                        onClick={cancelEditing}
                        aria-label="Batal"
                      >
                        <IconX />
                      </Button>
                    </form>
                  ) : (
                    <>
                      <Button
                        type="button"
                        variant="ghost"
                        onClick={() => onSelect(session.id)}
                        aria-current={active ? 'true' : undefined}
                        className={cn(
                          'rounded-control relative flex h-11 w-full min-w-0 justify-start gap-2 px-2.5 pr-16 text-left text-[13px] transition-colors md:h-9 md:pr-2.5 md:group-hover:pr-16 md:group-focus-within:pr-16',
                          active
                            ? 'bg-surface text-ink shadow-btn hover:bg-surface font-medium'
                            : 'text-ink-2 hover:bg-hover-2 hover:text-ink',
                        )}
                      >
                        {active && (
                          <span
                            aria-hidden
                            className="bg-ink absolute top-1/2 left-0 h-4 w-0.5 -translate-y-1/2 rounded-full"
                          />
                        )}
                        <IconMessage
                          className={cn(
                            'size-4 shrink-0 transition-colors',
                            active ? 'text-ink-2' : 'text-ink-3 group-hover:text-ink-2',
                          )}
                        />
                        <span className="min-w-0 flex-1 truncate">{session.title}</span>
                      </Button>
                      <div className="absolute right-1.5 top-1/2 flex -translate-y-1/2 items-center gap-0.5 opacity-100 transition-opacity duration-150 focus-within:opacity-100 md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100 md:focus-within:opacity-100">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          mode="icon"
                          onClick={() => startEditing(session)}
                          aria-label={`Ubah judul ${session.title}`}
                        >
                          <IconPencil className="size-3.5" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          mode="icon"
                          onClick={() => setPendingDelete(session)}
                          aria-label={`Hapus ${session.title}`}
                          className="hover:bg-red-tint hover:text-red"
                        >
                          <IconTrash className="size-3.5" />
                        </Button>
                      </div>
                    </>
                  )}
                </li>
              )
            })}
          </ul>
        </div>
      </ScrollArea>

      <SimpleAlertDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title="Hapus percakapan?"
        description={
          pendingDelete
            ? `Percakapan "${pendingDelete.title}" beserta seluruh pesannya akan dihapus permanen. Tindakan ini tidak dapat dibatalkan.`
            : undefined
        }
        cancelText="Batal"
        confirmText="Hapus percakapan"
        loading={deleting}
        onCancel={() => setPendingDelete(null)}
        onConfirm={async () => {
          if (!pendingDelete || deleting) return
          setDeleting(true)
          // Keep the dialog open with a spinner until the server confirms, so a
          // slow or failed delete never looks like it succeeded.
          if (await onDelete(pendingDelete.id)) setPendingDelete(null)
          setDeleting(false)
        }}
      />
    </aside>
  )
}
