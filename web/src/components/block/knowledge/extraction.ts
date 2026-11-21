import type { KnowledgeExtractionMetadata } from '@/lib/api/models/knowledge'

export const COVERAGE_LIMIT = 20

const warnings: Record<string, string> = {
  sheet_limit: 'Jumlah lembar yang diproses dibatasi.',
  row_limit: 'Jumlah baris yang diproses dibatasi.',
  column_limit: 'Jumlah kolom yang diproses dibatasi.',
  cell_truncated: 'Teks pada sebagian sel dipotong.',
  header_truncated: 'Teks header dipotong.',
  hidden_sheets: 'Lembar tersembunyi tidak disertakan.',
  images_or_objects: 'Gambar atau objek tidak diekstraksi sebagai teks.',
  unsupported_parts: 'Sebagian bagian dokumen tidak didukung.',
  unsupported_content: 'Sebagian konten tidak didukung.',
  heading_truncated: 'Teks judul bagian dipotong.',
  front_matter_omitted: 'Metadata pembuka dokumen tidak disertakan.',
  text_normalized: 'Format teks dinormalisasi saat ekstraksi.',
}

export function extractionWarning(code: string) {
  return Object.hasOwn(warnings, code)
    ? warnings[code]
    : `Catatan ekstraksi: ${code.replace(/[_\-\s]+/g, ' ').slice(0, 200)}`
}

export function extractionStatus(metadata: KnowledgeExtractionMetadata | null | undefined) {
  if (!metadata) return 'Metadata ekstraksi belum tersedia'
  return metadata.status === 'limited' ? 'Siap · Ekstraksi terbatas' : 'Siap digunakan'
}

export function coverageText(item: KnowledgeExtractionMetadata['coverage'][number]) {
  const processed = item.processed.toLocaleString('id-ID')
  const total = item.total?.toLocaleString('id-ID')
  const count =
    total === undefined
      ? `${processed} diproses; total tidak diketahui`
      : `${processed} dari ${total} diproses`
  const valid =
    Number.isFinite(item.processed) &&
    item.processed >= 0 &&
    item.total !== null &&
    Number.isFinite(item.total) &&
    item.total > 0 &&
    item.processed <= item.total
  return valid
    ? `${count} (${((item.processed / item.total!) * 100).toLocaleString('id-ID', { maximumFractionDigits: 1 })}%)`
    : count
}
