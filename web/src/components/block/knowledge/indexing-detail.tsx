import { useMutation, useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { Button } from '@/components/ui/button'
import type { KnowledgeScope } from '@/lib/api/models/knowledge'
import { queries } from '@/lib/api/queries'
import { formatDateTime } from '@/lib/date'

const labels = {
  not_configured: 'Belum dikonfigurasi',
  pending: 'Menunggu indeks',
  indexing: 'Mengindeks',
  ready: 'Indeks siap',
  partial: 'Indeks sebagian',
  failed: 'Indeks gagal',
}

export default function IndexingDetail({
  scope,
  id,
  canManage,
}: {
  scope: KnowledgeScope
  id: string
  canManage: boolean
}) {
  const options = useMemo(() => queries.knowledge.indexing(scope, id), [scope, id])
  const indexing = useQuery(options)
  const reindex = useMutation(queries.knowledge.reindex())
  const data = indexing.data
  const busy = data?.status === 'pending' || data?.status === 'indexing'
  const valid =
    data &&
    Number.isSafeInteger(data.indexed_chunks) &&
    Number.isSafeInteger(data.eligible_chunks) &&
    data.indexed_chunks >= 0 &&
    data.eligible_chunks > 0 &&
    data.indexed_chunks <= data.eligible_chunks
  return (
    <section aria-label="Indeks semantik" className="space-y-3 border-t pt-4">
      <h3 className="text-ink font-medium">Indeks semantik</h3>
      <p className="text-ink-2">
        Terpisah dari hasil ekstraksi. Dokumen tetap dapat dicari secara leksikal saat indeks belum
        siap.
      </p>
      {indexing.isPending ? (
        <p role="status">Memuat status indeks…</p>
      ) : indexing.isError ? (
        <div role="alert" className="space-y-2">
          <p>Status indeks gagal dimuat.</p>
          <Button
            variant="outline"
            disabled={indexing.isFetching}
            onClick={() => void indexing.refetch()}
          >
            Muat ulang status
          </Button>
        </div>
      ) : data ? (
        <div className="space-y-2" aria-live="polite">
          <p>{labels[data.status] ?? 'Status indeks tidak dikenal'}</p>
          <p>
            {valid
              ? `${data.indexed_chunks} dari ${data.eligible_chunks} potongan layak terindeks (${Math.floor((data.indexed_chunks / data.eligible_chunks) * 100)}%)`
              : 'Persentase indeks tidak tersedia; penghitung potongan belum valid.'}
          </p>
          <p className="text-ink-2">
            Persentase potongan terindeks, bukan persentase informasi dokumen.
          </p>
          <p className="break-all">Revisi profil: {data.profile_revision || 'Belum tersedia'}</p>
          <p>Diperbarui: {data.updated_at ? formatDateTime(data.updated_at) : 'Belum tersedia'}</p>
          {data.error_code && <p className="break-all">Kode galat: {data.error_code}</p>}
        </div>
      ) : null}
      {canManage && (
        <Button
          variant="outline"
          disabled={
            !data ||
            indexing.isError ||
            indexing.isFetching ||
            busy ||
            data.status === 'not_configured' ||
            reindex.isPending
          }
          onClick={() => reindex.mutate({ scope, id })}
        >
          {reindex.isPending
            ? 'Mengajukan indeks…'
            : data?.status === 'failed' || data?.status === 'partial'
              ? 'Coba indeks ulang'
              : 'Indeks ulang'}
        </Button>
      )}
      {reindex.isError && (
        <p role="alert" className="text-destructive">
          Gagal mengajukan indeks ulang. Periksa akses dan coba lagi.
        </p>
      )}
      {busy && (
        <p className="text-ink-2">
          Status diperiksa otomatis selama panel terbuka, maksimal 30 kali. Tutup panel untuk
          menghentikan pemeriksaan.
        </p>
      )}
    </section>
  )
}
