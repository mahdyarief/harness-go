import { IconFileText, IconFileTypePdf, IconTable, IconFileSpreadsheet } from '@tabler/icons-react'

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import type { MessageReference } from '@/lib/api/models/chat'
import { cn } from '@/lib/utils'
import Markdown from './markdown'

// Formats whose passages are authored as markdown (tables, lists, headings) and
// should be rendered as such. PDF and Word arrive as layout-less plain text.
const MARKDOWN_FORMATS = new Set(['md', 'txt', 'csv', 'xlsx'])

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

type ReferencePreviewProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  reference: MessageReference | null
}

/**
 * Preview of the single knowledge passage the assistant cited. The full-text
 * search result already carries the excerpt of the cited chunk, so we render
 * exactly that — not the surrounding document — and format it per source type
 * so tables and headings read cleanly instead of as one plain block.
 */
export function ReferencePreview({ open, onOpenChange, reference }: ReferencePreviewProps) {
  if (!open || !reference) return null

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" size="2xl" className="flex h-full flex-col gap-0 p-0">
        <SheetHeader className="border-line border-b">
          <SheetTitle className="flex items-center gap-2">
            <ReferenceIcon format={reference.format} />
            {reference.title}
          </SheetTitle>
          <SheetDescription>
            {FORMAT_LABEL[reference.format] ?? reference.format} · {reference.filename}
            {reference.locator ? ` · ${reference.locator}` : ''}
          </SheetDescription>
        </SheetHeader>
        <div
          role="log"
          aria-label={`Bagian yang dikutip dari ${reference.title}`}
          className="bg-inset min-h-0 flex-1 overflow-y-auto px-4 py-4"
        >
          <div className="flex min-w-0 flex-col gap-2">
            <div className="text-ink-3 flex items-center justify-between gap-2 text-[11px] font-medium">
              <span className="truncate">
                {reference.heading_path || `Bagian ${reference.position + 1}`}
              </span>
              <span className="text-ink-2 shrink-0">Dikutip</span>
            </div>
            <div
              className={cn(
                'rounded-card border border-ink-2 bg-field p-4 text-[13px] leading-relaxed shadow-hairline ring-1 ring-ink-2/20',
                !MARKDOWN_FORMATS.has(reference.format) && 'whitespace-pre-wrap text-ink',
              )}
            >
              {reference.excerpt ? (
                MARKDOWN_FORMATS.has(reference.format) ? (
                  <Markdown>{reference.excerpt}</Markdown>
                ) : (
                  reference.excerpt
                )
              ) : (
                <p className="text-ink-2">Kutipan tidak tersedia untuk bagian ini.</p>
              )}
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ReferenceIcon({ format }: { format: string }) {
  const Icon = FORMAT_ICON[format] ?? IconFileText
  return <Icon className="text-ink-3 size-4 shrink-0" />
}
