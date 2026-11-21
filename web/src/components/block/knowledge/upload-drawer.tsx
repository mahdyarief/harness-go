import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { queries } from '@/lib/api/queries'
import type { KnowledgeScope } from '@/lib/api/models/knowledge'

// Keep in sync with server/internal/lib/kb/kb.go (MaxUploadBytes).
const MAX_SIZE = 3 * 1024 * 1024
const ACCEPT = '.md,.txt,.csv,.docx,.xlsx,.pdf'
// Select needs a non-empty value, so "no category" gets an explicit sentinel.
const NO_CATEGORY = '__none__'

export default function UploadDrawer({
  scope,
  scopeName,
  open,
  onOpenChange,
}: {
  scope: KnowledgeScope
  scopeName: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [title, setTitle] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [categoryId, setCategoryId] = useState(NO_CATEGORY)
  const [validation, setValidation] = useState('')
  const upload = useMutation(queries.knowledge.upload())
  const categories = useQuery(queries.knowledge.categories(scope))

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!upload.isPending) onOpenChange(next)
      }}
    >
      <SheetContent side="right" size="md" showCloseButton={false}>
        <SheetHeader>
          <SheetTitle>Unggah dokumen</SheetTitle>
          <SheetDescription>
            {scopeName}. Dokumen langsung aktif setelah berhasil diunggah.
          </SheetDescription>
        </SheetHeader>
        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault()
            if (!file || !title.trim() || upload.isPending) return
            upload.mutate(
              {
                scope,
                title: title.trim(),
                file,
                categoryId: categoryId === NO_CATEGORY ? undefined : categoryId,
              },
              {
                onSuccess: () => {
                  toast.success('Dokumen berhasil diunggah dan langsung aktif.')
                  onOpenChange(false)
                },
              },
            )
          }}
        >
          <div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
            <div className="space-y-2">
              <Label htmlFor="knowledge-title">Judul dokumen</Label>
              <Input
                id="knowledge-title"
                value={title}
                required
                maxLength={200}
                disabled={upload.isPending}
                onChange={(event) => setTitle(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="knowledge-category">Kategori (opsional)</Label>
              <Select value={categoryId} onValueChange={setCategoryId}>
                <SelectTrigger id="knowledge-category" disabled={upload.isPending}>
                  <SelectValue placeholder="Tanpa kategori" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NO_CATEGORY}>Tanpa kategori</SelectItem>
                  {(categories.data?.data ?? []).map((category) => (
                    <SelectItem key={category.id} value={category.id}>
                      {category.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-ink-2 text-sm">
                Boleh dikosongkan. Dokumen tetap dapat diunggah dan diindeks.
              </p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="knowledge-file">Berkas</Label>
              <Input
                id="knowledge-file"
                type="file"
                accept={ACCEPT}
                required
                disabled={upload.isPending}
                aria-describedby="knowledge-file-hint knowledge-upload-error"
                aria-invalid={!!validation}
                onChange={(event) => {
                  const selected = event.target.files?.[0]
                  upload.reset()
                  setFile(null)
                  if (!selected) {
                    setValidation('')
                    return
                  }
                  const extension = selected.name
                    .slice(selected.name.lastIndexOf('.'))
                    .toLowerCase()
                  const error = !ACCEPT.split(',').includes(extension)
                    ? 'Format tidak didukung. Pilih MD, TXT, CSV, DOCX, XLSX, atau PDF.'
                    : selected.size > MAX_SIZE
                      ? 'Ukuran berkas melebihi batas 3 MiB.'
                      : selected.size === 0
                        ? 'Berkas kosong tidak dapat diunggah.'
                        : ''
                  setValidation(error)
                  if (!error) setFile(selected)
                }}
              />
              <p id="knowledge-file-hint" className="text-ink-2 text-sm">
                MD, TXT, CSV, DOCX, XLSX, atau PDF. Maksimal 3 MiB per berkas.
              </p>
            </div>
            <div
              id="knowledge-upload-error"
              role={validation || upload.isError ? 'alert' : undefined}
            >
              {validation && <p className="text-destructive text-sm">{validation}</p>}
              {upload.isError && (
                <p className="text-destructive text-sm">
                  Dokumen gagal diunggah. Periksa berkas dan akses Anda, lalu coba lagi.
                </p>
              )}
            </div>
            {upload.isPending && (
              <p role="status" className="text-ink-2 text-sm">
                Mengunggah dan menyiapkan dokumen. Mohon tunggu.
              </p>
            )}
          </div>
          <SheetFooter>
            <Button
              type="button"
              variant="outline"
              disabled={upload.isPending}
              onClick={() => onOpenChange(false)}
            >
              Batal
            </Button>
            <Button type="submit" disabled={upload.isPending || !file || !title.trim()}>
              {upload.isPending ? 'Mengunggah…' : 'Unggah dokumen'}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}
