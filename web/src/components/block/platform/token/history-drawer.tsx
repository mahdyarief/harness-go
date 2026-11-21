import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { tokenQueries } from '@/lib/api/queries/token'
import { formatDateTime } from '@/lib/date'

import type { Models } from '@/lib/api/models'

type HistoryDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  tenantId: string
  tenantName: string
}

const PAGE_SIZE = 20

export function HistoryDrawer({ open, onOpenChange, tenantId, tenantName }: HistoryDrawerProps) {
  if (!open) return null

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" size="xl" className="p-0">
        <div className="flex h-full flex-col">
          <SheetHeader>
            <SheetTitle>Riwayat token</SheetTitle>
            <SheetDescription>
              Penambahan dan pemakaian token untuk tenant <strong>{tenantName}</strong>. Setiap
              baris pemakaian merujuk ke sesi dan pesan asal, sehingga bisa dipakai sebagai log
              debug.
            </SheetDescription>
          </SheetHeader>

          <Tabs defaultValue="usage" className="flex min-h-0 flex-1 flex-col p-4">
            <TabsList>
              <TabsTrigger value="usage">Pemakaian</TabsTrigger>
              <TabsTrigger value="topup">Penambahan</TabsTrigger>
            </TabsList>
            <TabsContent value="usage" className="min-h-0 flex-1 overflow-y-auto">
              <UsageHistory tenantId={tenantId} />
            </TabsContent>
            <TabsContent value="topup" className="min-h-0 flex-1 overflow-y-auto">
              <TopUpHistory tenantId={tenantId} />
            </TabsContent>
          </Tabs>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function UsageHistory({ tenantId }: { tenantId: string }) {
  const [offset, setOffset] = useState(0)
  const { data, isPending, isError } = useQuery(
    tokenQueries.usages(tenantId, { offset, limit: PAGE_SIZE }),
  )

  if (isPending) return <HistorySkeleton />
  if (isError) return <p className="text-red p-2 text-sm">Gagal memuat riwayat.</p>

  const rows = data?.data ?? []
  const total = data?.metadata?.total ?? 0
  if (rows.length === 0) {
    return <p className="text-ink-2 p-2 text-sm">Belum ada pemakaian token.</p>
  }

  return (
    <div className="divide-line divide-y">
      {rows.map((row) => (
        <UsageRow key={row.id} row={row} />
      ))}
      <HistoryPager offset={offset} pageSize={PAGE_SIZE} total={total} onChange={setOffset} />
    </div>
  )
}

function UsageRow({ row }: { row: Models.TokenUsage }) {
  const clamped = row.billed_tokens !== row.total_tokens
  return (
    <div className="flex items-start justify-between gap-4 py-3">
      <div className="min-w-0 space-y-0.5">
        <p className="text-ink text-sm tabular-nums">{formatDateTime(row.created_at)}</p>
        <p className="truncate text-xs text-ink-2">
          {row.user_name} · {row.user_email} · sesi {row.session_id?.slice(0, 8) ?? '-'}
        </p>
      </div>
      <div className="shrink-0 text-right">
        <p className="text-ink text-sm font-medium tabular-nums">
          −{row.billed_tokens.toLocaleString('id-ID')}
        </p>
        <p className="text-xs text-ink-2 tabular-nums">
          {row.prompt_tokens.toLocaleString('id-ID')} prompt +{' '}
          {row.completion_tokens.toLocaleString('id-ID')} completion
        </p>
        {clamped && (
          <p className="text-xs text-ink-2">
            ditagih sebagian dari {row.total_tokens.toLocaleString('id-ID')}
          </p>
        )}
      </div>
    </div>
  )
}

function TopUpHistory({ tenantId }: { tenantId: string }) {
  const [offset, setOffset] = useState(0)
  const { data, isPending, isError } = useQuery(
    tokenQueries.topups(tenantId, { offset, limit: PAGE_SIZE }),
  )

  if (isPending) return <HistorySkeleton />
  if (isError) return <p className="text-red p-2 text-sm">Gagal memuat riwayat.</p>

  const rows = data?.data ?? []
  const total = data?.metadata?.total ?? 0
  if (rows.length === 0) {
    return <p className="text-ink-2 p-2 text-sm">Belum ada penambahan token.</p>
  }

  return (
    <div className="divide-line divide-y">
      {rows.map((row) => (
        <TopUpRow key={row.id} row={row} />
      ))}
      <HistoryPager offset={offset} pageSize={PAGE_SIZE} total={total} onChange={setOffset} />
    </div>
  )
}

function TopUpRow({ row }: { row: Models.TokenTopup }) {
  return (
    <div className="flex items-start justify-between gap-4 py-3">
      <div className="min-w-0 space-y-0.5">
        <p className="text-ink text-sm tabular-nums">{formatDateTime(row.created_at)}</p>
        <p className="truncate text-xs text-ink-2">
          {row.note || 'Tanpa catatan'} · oleh {row.actor_email}
        </p>
      </div>
      <p className="shrink-0 text-sm font-medium text-green tabular-nums">
        +{row.amount.toLocaleString('id-ID')}
      </p>
    </div>
  )
}

function HistoryPager({
  offset,
  pageSize,
  total,
  onChange,
}: {
  offset: number
  pageSize: number
  total: number
  onChange: (offset: number) => void
}) {
  if (total <= pageSize) return null
  const page = Math.floor(offset / pageSize) + 1
  const pages = Math.ceil(total / pageSize)
  return (
    <div className="flex items-center justify-between py-3">
      <p className="text-xs text-ink-2">
        Halaman {page} dari {pages}
      </p>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={page <= 1}
          onClick={() => onChange(Math.max(0, offset - pageSize))}
        >
          Sebelumnya
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={page >= pages}
          onClick={() => onChange(offset + pageSize)}
        >
          Berikutnya
        </Button>
      </div>
    </div>
  )
}

function HistorySkeleton() {
  return (
    <div className="space-y-3 py-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex justify-between gap-4">
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-16" />
        </div>
      ))}
    </div>
  )
}
