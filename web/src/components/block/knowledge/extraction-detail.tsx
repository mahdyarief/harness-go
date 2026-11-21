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
import type {
  KnowledgeDocument,
  KnowledgeDocumentUsage,
  KnowledgeScope,
} from '@/lib/api/models/knowledge'
import { citationRate, semanticShare } from '@/lib/api/models/knowledge'
import { queries } from '@/lib/api/queries'
import IndexingDetail from './indexing-detail'
import { COVERAGE_LIMIT, coverageText, extractionStatus, extractionWarning } from './extraction'

export default function ExtractionDetail({
  scope,
  document,
  canManage,
  onClose,
}: {
  scope: KnowledgeScope
  document: KnowledgeDocument
  canManage: boolean
  onClose: () => void
}) {
  const [offset, setOffset] = useState(0)
  const preview = useQuery(queries.knowledge.preview(scope, document.id, offset))
  const current = preview.data?.data.document ?? document
  const metadata = current.extraction_metadata
  const pagination = preview.data?.metadata
  const usage = useQuery(queries.knowledge.usageDocument(scope, document.id))

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent side="right" size="xl">
        <SheetHeader>
          <SheetTitle>Detail ekstraksi</SheetTitle>
          <SheetDescription>
            {document.title} · {document.tenant_id === null ? 'Pustaka global' : 'Pustaka tenant'}
          </SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-6 pb-6 text-sm">
          <IndexingDetail scope={scope} id={document.id} canManage={canManage} />
          <UsageDetail usage={usage.data} isPending={usage.isPending} isError={usage.isError} />
          <section className="space-y-2" aria-label="Hasil ekstraksi">
            <h3 className="text-ink font-medium">{extractionStatus(metadata)}</h3>
            <p className="text-ink-2">
              Status ini hanya mencakup ekstraksi teks yang didukung, bukan seluruh informasi atau
              jaminan akurasi dokumen. Periksa sumber asli untuk konteks lengkap.
            </p>
            <p className="text-ink-2">
              {current.char_count.toLocaleString('id-ID')} karakter ·{' '}
              {current.chunk_count.toLocaleString('id-ID')} potongan
            </p>
            {metadata && (
              <p className="text-ink-2">Versi ekstraktor: {metadata.extractor_version}</p>
            )}
          </section>
          {metadata && (
            <>
              <section className="space-y-2" aria-label="Catatan ekstraksi">
                <h3 className="text-ink font-medium">Catatan ekstraksi</h3>
                {metadata.warnings.length ? (
                  <ul className="list-disc space-y-2 pl-5 break-words">
                    {metadata.warnings.map((warning, index) => (
                      <li key={`${index}-${warning}`}>{extractionWarning(warning)}</li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-ink-2">Tidak ada catatan ekstraksi yang dilaporkan.</p>
                )}
              </section>
              <section className="space-y-2" aria-label="Cakupan pemrosesan">
                <h3 className="text-ink font-medium">Cakupan pemrosesan</h3>
                <p className="text-ink-2">
                  Angka ini menunjukkan unit yang diproses, bukan persentase informasi atau akurasi.
                </p>
                {metadata.coverage.length ? (
                  <ul className="space-y-3">
                    {metadata.coverage.slice(0, COVERAGE_LIMIT).map((item, index) => (
                      <li key={index} className="break-words">
                        <p className="text-ink font-medium">
                          {item.scope} · {item.unit}
                        </p>
                        <p className="text-ink-2">{coverageText(item)}</p>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-ink-2">Rincian cakupan belum tersedia.</p>
                )}
                {metadata.coverage.length > COVERAGE_LIMIT && (
                  <p className="text-ink-2">
                    Menampilkan {COVERAGE_LIMIT} dari {metadata.coverage.length} rincian cakupan.
                  </p>
                )}
              </section>
            </>
          )}
          <section className="space-y-3" aria-label="Pratinjau teks">
            <h3 className="text-ink font-medium">Pratinjau teks</h3>
            <p className="text-ink-2">
              Teks hasil ekstraksi yang digunakan sebagai referensi percakapan.
            </p>
            {preview.isPending ? (
              <p role="status">Memuat pratinjau…</p>
            ) : preview.isError ? (
              <div role="alert" className="space-y-3">
                <p className="text-destructive">
                  Pratinjau gagal dimuat. Periksa akses Anda atau coba lagi.
                </p>
                <Button
                  variant="outline"
                  disabled={preview.isFetching}
                  onClick={() => void preview.refetch()}
                >
                  Coba lagi
                </Button>
              </div>
            ) : (
              <>
                {preview.data.data.chunks.length ? (
                  preview.data.data.chunks.map((chunk) => (
                    <article
                      key={chunk.position}
                      className="border-border space-y-2 rounded-lg border p-4"
                    >
                      <h4 className="text-ink font-medium break-words">
                        Potongan {chunk.position + 1}
                        {chunk.heading_path ? ` · ${chunk.heading_path}` : ''}
                      </h4>
                      {chunk.locator && (
                        <p className="text-ink-2 break-words">Lokasi: {chunk.locator}</p>
                      )}
                      <p className="text-ink whitespace-pre-wrap break-words">{chunk.content}</p>
                    </article>
                  ))
                ) : (
                  <p className="text-ink-2">Tidak ada potongan teks pada halaman ini.</p>
                )}
                {pagination && (
                  <nav
                    aria-label="Halaman pratinjau"
                    className="flex flex-wrap items-center justify-between gap-3"
                  >
                    <p className="text-ink-2" aria-live="polite">
                      {pagination.total === 0
                        ? '0 potongan'
                        : `${pagination.offset + 1}–${Math.min(pagination.offset + pagination.limit, pagination.total)} dari ${pagination.total} potongan`}
                    </p>
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        disabled={pagination.offset === 0 || preview.isFetching}
                        onClick={() => setOffset(Math.max(0, pagination.offset - pagination.limit))}
                      >
                        Sebelumnya
                      </Button>
                      <Button
                        variant="outline"
                        disabled={
                          pagination.offset + pagination.limit >= pagination.total ||
                          preview.isFetching
                        }
                        onClick={() => setOffset(pagination.offset + pagination.limit)}
                      >
                        Berikutnya
                      </Button>
                    </div>
                  </nav>
                )}
              </>
            )}
          </section>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function UsageDetail({
  usage,
  isPending,
  isError,
}: {
  usage: KnowledgeDocumentUsage | undefined
  isPending: boolean
  isError: boolean
}) {
  return (
    <section className="space-y-2" aria-label="Metrik penggunaan">
      <h3 className="text-ink font-medium">Metrik penggunaan</h3>
      <p className="text-ink-2">
        Dihitung dari 30 hari terakhir. &ldquo;Diambil&rdquo; berarti potongan dokumen ini
        ditawarkan ke model; &ldquo;dikutip&rdquo; berarti dokumen muncul sebagai referensi jawaban.
      </p>
      {isPending ? (
        <p role="status" className="text-ink-2">
          Memuat metrik…
        </p>
      ) : isError ? (
        <p role="alert" className="text-destructive">
          Metrik penggunaan gagal dimuat.
        </p>
      ) : !usage || usage.retrievals === 0 ? (
        <p className="text-ink-2">
          Dokumen ini belum pernah diambil dalam percakapan. Unggah dokumen yang relevan atau cek
          konfigurasi embedding bila pencarian semantik diharapkan.
        </p>
      ) : (
        <dl className="grid grid-cols-2 gap-3">
          <div className="border-border rounded-lg border p-3">
            <dt className="text-ink-2 text-xs">Diambil</dt>
            <dd className="text-ink tabular-nums text-lg">
              {usage.retrievals.toLocaleString('id-ID')}
            </dd>
          </div>
          <div className="border-border rounded-lg border p-3">
            <dt className="text-ink-2 text-xs">Dikutip</dt>
            <dd className="text-ink tabular-nums text-lg">
              {usage.citations.toLocaleString('id-ID')}
            </dd>
          </div>
          <div className="border-border rounded-lg border p-3">
            <dt className="text-ink-2 text-xs">Rasio kutipan</dt>
            <dd className="text-ink tabular-nums text-lg">
              {(citationRate(usage) * 100).toFixed(1)}%
            </dd>
          </div>
          <div className="border-border rounded-lg border p-3">
            <dt className="text-ink-2 text-xs">Kontribusi semantik</dt>
            <dd className="text-ink tabular-nums text-lg">
              {(semanticShare(usage) * 100).toFixed(1)}%
            </dd>
          </div>
        </dl>
      )}
    </section>
  )
}
