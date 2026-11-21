import { useMemo } from 'react'
import { IconFileSpreadsheet, IconFileText, IconFileTypePdf, IconTable } from '@tabler/icons-react'

import { Button } from '@/components/ui/button'
import type { MessageReference } from '@/lib/api/models/chat'

const FORMAT_ICON: Record<string, typeof IconFileText> = {
  pdf: IconFileTypePdf,
  xlsx: IconFileSpreadsheet,
  csv: IconTable,
}

const FORMAT_LABEL: Record<string, string> = {
  md: 'Markdown',
  txt: 'Teks',
  csv: 'CSV',
  docx: 'Word',
  xlsx: 'Excel',
  pdf: 'PDF',
}

type ReferenceChipsProps = {
  references: MessageReference[]
  onSelect?: (reference: MessageReference) => void
}

type DocumentGroup = {
  documentId: string
  title: string
  filename: string
  format: string
  parts: MessageReference[]
}

/**
 * Reference list appended to every assistant answer that used the knowledge
 * tool. The entries come from the tool result, not from the model's prose, so
 * each chip points at a chunk that actually exists. Parts of the same document
 * are grouped so a long answer citing five pages does not repeat the title.
 */
export default function ReferenceChips({ references, onSelect }: ReferenceChipsProps) {
  const groups = useMemo(() => {
    const byDocument = new Map<string, DocumentGroup>()
    for (const reference of references) {
      const group = byDocument.get(reference.document_id)
      if (group) {
        group.parts.push(reference)
      } else {
        byDocument.set(reference.document_id, {
          documentId: reference.document_id,
          title: reference.title,
          filename: reference.filename,
          format: reference.format,
          parts: [reference],
        })
      }
    }
    return [...byDocument.values()]
  }, [references])

  return (
    <section
      aria-label="Dokumen referensi"
      className="border-line mt-1 flex max-w-[90%] flex-col gap-2 border-t pt-3"
    >
      <h3 className="text-ink-3 px-1 text-[11px] font-medium">Referensi</h3>
      {groups.map((group) => (
        <div key={group.documentId} className="flex flex-col gap-1.5 px-1">
          <div className="flex items-baseline gap-2">
            <ReferenceIcon format={group.format} />
            <span className="text-ink truncate text-[12px] font-medium">{group.title}</span>
            <span className="text-ink-3 shrink-0 text-[11px]">
              {FORMAT_LABEL[group.format] ?? group.format}
            </span>
          </div>
          <div className="flex flex-wrap gap-1.5 pl-6">
            {group.parts.map((part) => (
              <Button
                key={part.chunk_id}
                type="button"
                variant="outline"
                size="sm"
                className="rounded-chip h-6 px-2 text-[11px]"
                title={part.heading_path || part.filename}
                onClick={() => onSelect?.(part)}
              >
                {part.locator || `Bagian ${part.position + 1}`}
              </Button>
            ))}
          </div>
        </div>
      ))}
    </section>
  )
}

function ReferenceIcon({ format }: { format: string }) {
  const Icon = FORMAT_ICON[format] ?? IconFileText
  return <Icon className="text-ink-3 size-3.5 shrink-0" />
}
