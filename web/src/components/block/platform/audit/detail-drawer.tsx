import { useQuery } from '@tanstack/react-query'

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { auditQueries } from '@/lib/api/queries/audit'
import { formatDateTime } from '@/lib/date'
import type { Models } from '@/lib/api/models'

type AuditDetailDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The row from the table; refetched by id so the drawer stays truthful. */
  record: Models.AuditLog
}

/** Read-only detail view. The trail is append-only, so there is nothing to edit. */
export function AuditDetailDrawer({ open, onOpenChange, record }: AuditDetailDrawerProps) {
  // Mounted only while open; `enabled` keeps a closed drawer from fetching.
  const { data, isPending } = useQuery({
    ...auditQueries.detail(record.id),
    enabled: open,
  })
  const entry = data ?? record

  const metadata =
    entry.metadata && Object.keys(entry.metadata).length > 0
      ? JSON.stringify(entry.metadata, null, 2)
      : null

  const rows: Array<[string, string]> = [
    ['Waktu', formatDateTime(entry.occurred_at)],
    ['Aktor', entry.actor_name || entry.actor_id],
    ['Peran aktor', entry.actor_role || '-'],
    ['Aksi', entry.action],
    ['Tipe entitas', entry.entity_type],
    ['ID entitas', entry.entity_id ?? '-'],
    ['ID tenant', entry.tenant_id ?? '-'],
    ['Request ID', entry.request_id || '-'],
    ['IP', entry.ip || '-'],
    ['User agent', entry.user_agent || '-'],
  ]

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" size="md" className="p-0">
        <div className="flex h-full min-h-0 flex-col">
          <SheetHeader>
            <SheetTitle>Detail jejak audit</SheetTitle>
            <SheetDescription>
              Baris jejak audit bersifat permanen (append-only) dan tidak dapat diubah.
            </SheetDescription>
          </SheetHeader>

          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            {isPending ? (
              <p role="status" className="text-ink-2 text-sm">
                Memuat detail…
              </p>
            ) : (
              <dl className="flex flex-col gap-3 text-sm">
                {rows.map(([label, value]) => (
                  <div key={label} className="grid grid-cols-[8rem_1fr] gap-2">
                    <dt className="text-ink-2">{label}</dt>
                    <dd className="text-ink break-words">{value}</dd>
                  </div>
                ))}
              </dl>
            )}

            {metadata && (
              <div className="mt-6 flex flex-col gap-2">
                <p className="text-ink text-sm font-medium">Metadata</p>
                <pre className="bg-field border-line overflow-x-auto rounded-card border p-3 font-mono text-xs">
                  {metadata}
                </pre>
              </div>
            )}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
