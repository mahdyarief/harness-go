import { IconPencil, IconPlus, IconRefresh, IconTag, IconTrash, IconX } from '@tabler/icons-react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { toastAxiosError } from '@/lib/api/axios-error'
import EmptyState from '@/components/block/common/empty-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { queries } from '@/lib/api/queries'
import type { KnowledgeCategory, KnowledgeScope } from '@/lib/api/models/knowledge'

export default function CategoryDrawer({
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
  const [draft, setDraft] = useState('')
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null)
  const [deleting, setDeleting] = useState<KnowledgeCategory | null>(null)
  const categories = useQuery(queries.knowledge.categories(scope))
  const create = useMutation(queries.knowledge.createCategory())
  const rename = useMutation(queries.knowledge.renameCategory())
  const remove = useMutation(queries.knowledge.deleteCategory())

  const pending = create.isPending || rename.isPending || remove.isPending

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!pending) onOpenChange(next)
      }}
    >
      <SheetContent side="right" size="md" showCloseButton={false}>
        <SheetHeader>
          <SheetTitle>Kelola kategori</SheetTitle>
          <SheetDescription>
            {scopeName}. Kategori hanya mengelompokkan dokumen; menghapusnya tidak menghapus
            dokumen.
          </SheetDescription>
        </SheetHeader>
        <div className="flex min-h-0 flex-1 flex-col">
          <form
            className="space-y-2 border-b px-6 py-5"
            onSubmit={(event) => {
              event.preventDefault()
              const name = draft.trim()
              if (!name || create.isPending) return
              create.mutate(
                { scope, name },
                {
                  onSuccess: () => {
                    setDraft('')
                    toast.success('Kategori berhasil dibuat.')
                  },
                  onError: (error) => toastAxiosError(error),
                },
              )
            }}
          >
            <Label htmlFor="knowledge-category-name">Nama kategori baru</Label>
            <div className="flex gap-2">
              <Input
                id="knowledge-category-name"
                value={draft}
                maxLength={80}
                placeholder="Contoh: Regulasi BPJS"
                disabled={create.isPending}
                onChange={(event) => setDraft(event.target.value)}
              />
              <Button type="submit" disabled={create.isPending || !draft.trim()}>
                <IconPlus className="size-4" />
                Tambah
              </Button>
            </div>
            {create.isError && (
              <p role="alert" className="text-destructive text-sm">
                Kategori gagal dibuat. Pastikan nama belum dipakai, lalu coba lagi.
              </p>
            )}
          </form>

          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
            {categories.isError ? (
              <EmptyState
                icon={IconRefresh}
                title="Kategori gagal dimuat"
                description="Periksa koneksi Anda atau coba muat ulang."
                action={
                  <Button variant="outline" onClick={() => void categories.refetch()}>
                    <IconRefresh className="size-4" />
                    Coba lagi
                  </Button>
                }
              />
            ) : categories.isPending ? (
              <div role="status" className="space-y-3" aria-label="Memuat kategori">
                {Array.from({ length: 4 }, (_, index) => (
                  <Skeleton key={index} className="h-10 w-full" />
                ))}
              </div>
            ) : !categories.data.data.length ? (
              <EmptyState
                icon={IconTag}
                title="Belum ada kategori"
                description="Tambahkan kategori untuk mulai mengelompokkan dokumen."
              />
            ) : (
              <ul className="space-y-2">
                {categories.data.data.map((category) => (
                  <li
                    key={category.id}
                    className="flex items-center gap-2 rounded-md border px-3 py-2"
                  >
                    {editing?.id === category.id ? (
                      <>
                        <Input
                          aria-label={`Nama baru untuk ${category.name}`}
                          value={editing.name}
                          maxLength={80}
                          disabled={rename.isPending}
                          onChange={(event) =>
                            setEditing({ id: category.id, name: event.target.value })
                          }
                        />
                        <Button
                          size="sm"
                          disabled={rename.isPending || !editing.name.trim()}
                          onClick={() => {
                            const name = editing.name.trim()
                            if (!name) return
                            rename.mutate(
                              { scope, id: category.id, name },
                              {
                                onSuccess: () => {
                                  setEditing(null)
                                  toast.success('Kategori diperbarui.')
                                },
                                onError: (error) => toastAxiosError(error),
                              },
                            )
                          }}
                        >
                          Simpan
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label="Batal ubah nama"
                          disabled={rename.isPending}
                          onClick={() => setEditing(null)}
                        >
                          <IconX className="size-4" />
                        </Button>
                      </>
                    ) : (
                      <>
                        <span className="min-w-0 flex-1 truncate text-sm font-medium">
                          {category.name}
                        </span>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`Ubah nama ${category.name}`}
                          disabled={pending}
                          onClick={() => {
                            rename.reset()
                            setEditing({ id: category.id, name: category.name })
                          }}
                        >
                          <IconPencil className="size-4" />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`Hapus ${category.name}`}
                          disabled={pending}
                          onClick={() => {
                            remove.reset()
                            setDeleting(category)
                          }}
                        >
                          <IconTrash className="size-4" />
                        </Button>
                      </>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </SheetContent>

      <AlertDialog
        open={!!deleting}
        onOpenChange={(next) => {
          if (!next && !remove.isPending) setDeleting(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Hapus kategori?</AlertDialogTitle>
            <AlertDialogDescription>
              Kategori “{deleting?.name}” akan dihapus. Dokumen di dalamnya tetap tersimpan dan
              kembali ke “Tanpa kategori”.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {remove.isError && (
            <p role="alert" className="text-destructive text-sm">
              Kategori gagal dihapus. Periksa akses Anda, lalu coba lagi.
            </p>
          )}
          <AlertDialogFooter>
            <Button variant="outline" disabled={remove.isPending} onClick={() => setDeleting(null)}>
              Batal
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => {
                if (!deleting) return
                remove.mutate(
                  { scope, id: deleting.id },
                  {
                    onSuccess: () => {
                      setDeleting(null)
                      toast.success('Kategori dihapus. Dokumen tetap tersimpan.')
                    },
                  },
                )
              }}
            >
              {remove.isPending ? 'Menghapus…' : 'Hapus kategori'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Sheet>
  )
}
