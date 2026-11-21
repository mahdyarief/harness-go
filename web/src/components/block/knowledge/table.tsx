import {
  IconArchive,
  IconBook,
  IconPlus,
  IconRefresh,
  IconTag,
  IconWorldUpload,
} from '@tabler/icons-react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import { toastAxiosError } from '@/lib/api/axios-error'
import EmptyState from '@/components/block/common/empty-state'
import RowColumnAction from '@/components/block/common/row-column-action'
import { features } from '@/components/block/common/react-table'
import { useTable, type ReactTable } from '@tanstack/react-table'
import { DataGrid, DataGridContainer } from '@/components/ui/data-grid'
import { DataGridTable } from '@/components/ui/data-grid-table'
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area'
import SectionCard from '@/components/block/common/section-card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { queries } from '@/lib/api/queries'
import {
  citationRate,
  type KnowledgeDocument,
  type KnowledgeDocumentUsage,
  type KnowledgeScope,
  type KnowledgeStatus,
} from '@/lib/api/models/knowledge'
import { formatDateTime } from '@/lib/date'
import type { AppColumnDef } from '@/types/column'
import UploadDrawer from './upload-drawer'
import CategoryDrawer from './category-drawer'
import ExtractionDetail from './extraction-detail'
import { extractionStatus } from './extraction'

// Select values cannot be empty, so the two "no specific category" states get
// explicit sentinels; both map back to omitted query parameters.
const ALL_CATEGORIES = '__all__'
const NO_CATEGORY = '__none__'

export default function KnowledgeLibrary({
  scope,
  scopeName,
  canManage,
}: {
  scope: KnowledgeScope
  scopeName: string
  canManage: boolean
}) {
  const [search, setSearch] = useState('')
  const [categoryFilter, setCategoryFilter] = useState(ALL_CATEGORIES)
  const [page, setPage] = useState(0)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [categoriesOpen, setCategoriesOpen] = useState(false)
  const [detail, setDetail] = useState<{
    scope: KnowledgeScope
    document: KnowledgeDocument
  } | null>(null)
  const [deleting, setDeleting] = useState<KnowledgeDocument | null>(null)
  const categories = useQuery(queries.knowledge.categories(scope))
  const list = useQuery(
    queries.knowledge.list(scope, {
      offset: page * 10,
      limit: 10,
      search: search.trim() || undefined,
      category_id:
        categoryFilter === ALL_CATEGORIES || categoryFilter === NO_CATEGORY
          ? undefined
          : categoryFilter,
      uncategorized: categoryFilter === NO_CATEGORY ? true : undefined,
    }),
  )
  const remove = useMutation(queries.knowledge.remove())
  const setCategory = useMutation(queries.knowledge.setCategory())
  const setStatus = useMutation(queries.knowledge.setStatus())
  // Only categories from the same library may be attached to a document.
  const assignable = useMemo(
    () => (categories.data?.data ?? []).filter((category) => category.tenant_id === scope),
    [categories.data, scope],
  )
  const usage = useQuery(queries.knowledge.usage(scope))
  const usageByDocument = useMemo(() => {
    const map = new Map<string, KnowledgeDocumentUsage>()
    for (const row of usage.data ?? []) map.set(row.document.id, row)
    return map
  }, [usage.data])
  const columns = useMemo<AppColumnDef<KnowledgeDocument>[]>(
    () => [
      {
        accessorKey: 'title',
        header: 'Dokumen',
        size: 280,
        minSize: 200,
        cell: ({ row }) => (
          <div className="min-w-0 w-full py-1">
            <p className="text-ink truncate font-medium" title={row.original.title}>
              {row.original.title}
            </p>
            <p className="text-ink-2 truncate text-xs" title={row.original.filename}>
              {row.original.filename}
            </p>
          </div>
        ),
      },
      {
        id: 'scope',
        header: 'Pustaka',
        size: 90,
        minSize: 80,
        cell: ({ row }) => (row.original.tenant_id === null ? 'Global' : 'Tenant'),
      },
      {
        id: 'status',
        header: 'Status',
        size: 120,
        minSize: 100,
        cell: ({ row }) =>
          row.original.status === 'published' ? (
            <Badge variant="success" appearance="light" size="sm">
              Diterbitkan
            </Badge>
          ) : (
            <Badge variant="warning" appearance="light" size="sm">
              Draft
            </Badge>
          ),
      },
      {
        id: 'category',
        header: 'Kategori',
        size: 180,
        minSize: 140,
        cell: ({ row }) => {
          const editable = canManage && row.original.tenant_id === scope
          if (!editable) {
            return row.original.category_name ? (
              <span className="inline-flex max-w-full min-w-0 items-center gap-1 rounded-md border px-2 py-1 text-xs">
                <IconTag className="size-3 shrink-0" />
                <span className="truncate" title={row.original.category_name}>
                  {row.original.category_name}
                </span>
              </span>
            ) : (
              <span className="text-ink-2 text-xs">Tanpa kategori</span>
            )
          }
          return (
            <Select
              value={row.original.category_id ?? NO_CATEGORY}
              disabled={setCategory.isPending || categories.isPending}
              onValueChange={(value) => {
                if (setCategory.isPending) return
                setCategory.mutate(
                  {
                    scope,
                    id: row.original.id,
                    categoryId: value === NO_CATEGORY ? null : value,
                  },
                  {
                    onSuccess: () => toast.success('Kategori dokumen diperbarui.'),
                    onError: (error) => toastAxiosError(error),
                  },
                )
              }}
            >
              <SelectTrigger className="w-full" aria-label={`Kategori ${row.original.title}`}>
                <SelectValue placeholder="Tanpa kategori" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_CATEGORY}>Tanpa kategori</SelectItem>
                {assignable.map((category) => (
                  <SelectItem key={category.id} value={category.id}>
                    {category.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )
        },
      },
      {
        accessorKey: 'format',
        header: 'Format',
        size: 90,
        minSize: 80,
        cell: ({ row }) => <span className="text-xs uppercase">{row.original.format}</span>,
      },
      {
        accessorKey: 'size_bytes',
        header: 'Ukuran',
        size: 100,
        minSize: 90,
        cell: ({ row }) => (
          <span className="tabular-nums">
            {(row.original.size_bytes / 1024).toLocaleString('id-ID', { maximumFractionDigits: 1 })}{' '}
            KiB
          </span>
        ),
      },
      {
        id: 'extraction',
        header: 'Hasil ekstraksi',
        size: 240,
        minSize: 200,
        cell: ({ row }) => (
          <div className="min-w-0 w-full space-y-1 py-1">
            <p className="text-ink truncate text-sm">
              {extractionStatus(row.original.extraction_metadata)}
            </p>
            <p className="text-ink-2 truncate text-xs">Hanya teks yang didukung.</p>
            <Button
              variant="outline"
              size="sm"
              aria-label={`Detail ekstraksi ${row.original.title}`}
              onClick={() => setDetail({ scope, document: row.original })}
            >
              Detail
            </Button>
          </div>
        ),
      },
      {
        accessorKey: 'char_count',
        header: 'Karakter',
        size: 100,
        minSize: 80,
        cell: ({ row }) => row.original.char_count.toLocaleString('id-ID'),
      },
      {
        id: 'usage',
        header: 'Dikutip',
        size: 110,
        minSize: 90,
        cell: ({ row }) => {
          const usage = usageByDocument.get(row.original.id)
          return (
            <div className="min-w-0 w-full space-y-1 py-1">
              <p className="text-ink tabular-nums text-sm">
                {usage?.citations ?? 0} / {usage?.retrievals ?? 0}
              </p>
              <p className="text-ink-2 truncate text-xs">
                {usage ? `${(citationRate(usage) * 100).toFixed(1)}% kutipan` : 'Belum ada kutipan'}
              </p>
            </div>
          )
        },
      },
      {
        accessorKey: 'chunk_count',
        header: 'Potongan',
        size: 90,
        minSize: 80,
        cell: ({ row }) => row.original.chunk_count.toLocaleString('id-ID'),
      },
      {
        accessorKey: 'created_at',
        header: 'Diunggah',
        size: 130,
        minSize: 110,
        cell: ({ row }) => (
          <span className="whitespace-nowrap text-xs">
            {formatDateTime(row.original.created_at)}
          </span>
        ),
      },
      ...(canManage
        ? [
            {
              id: 'actions',
              header: 'Aksi',
              size: 120,
              minSize: 120,
              cell: ({ row }: { row: { original: KnowledgeDocument } }) =>
                row.original.tenant_id === scope ? (
                  <RowColumnAction
                    dropdown={
                      <DropdownMenuItem
                        disabled={setStatus.isPending}
                        aria-label={`${
                          row.original.status === 'published' ? 'Jadikan draft' : 'Terbitkan'
                        } ${row.original.title}`}
                        onClick={() => {
                          const next: KnowledgeStatus =
                            row.original.status === 'published' ? 'draft' : 'published'
                          setStatus.mutate(
                            { scope, id: row.original.id, status: next },
                            {
                              onSuccess: () =>
                                toast.success(
                                  next === 'published'
                                    ? 'Dokumen diterbitkan dan kini menjadi referensi AI.'
                                    : 'Dokumen dikembalikan ke draft.',
                                ),
                              onError: (error) => toastAxiosError(error),
                            },
                          )
                        }}
                      >
                        {row.original.status === 'published' ? (
                          <IconArchive />
                        ) : (
                          <IconWorldUpload />
                        )}
                        {row.original.status === 'published' ? 'Jadikan draft' : 'Terbitkan'}
                      </DropdownMenuItem>
                    }
                    onDelete={() => {
                      remove.reset()
                      setDeleting(row.original)
                    }}
                  />
                ) : null,
            },
          ]
        : []),
    ],
    [
      assignable,
      canManage,
      categories.isPending,
      remove,
      scope,
      setCategory,
      setStatus,
      usageByDocument,
    ],
  )

  const table = useTable({
    features,
    columns,
    data: list.data?.data ?? [],
    getRowId: (row) => row.id,
    rowCount: list.data?.metadata.total ?? 0,
    manualPagination: true,
    manualSorting: true,
    enableSorting: false,
    state: { pagination: { pageIndex: page, pageSize: 10 } },
  })

  return (
    <SectionCard
      title={scopeName}
      description={
        scope === null
          ? 'Referensi untuk percakapan platform dan tenant. Hanya superuser yang dapat mengelola pustaka global.'
          : canManage
            ? 'Percakapan memakai pustaka global dan dokumen tenant. Anda hanya dapat mengelola dokumen milik tenant ini.'
            : 'Percakapan memakai pustaka global dan dokumen tenant. Hanya admin tenant yang dapat mengelola dokumen tenant.'
      }
      toolbar={
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={list.isFetching} onClick={() => void list.refetch()}>
            <IconRefresh className="size-4" />
            Muat ulang
          </Button>
          {canManage && (
            <>
              <Button variant="outline" onClick={() => setCategoriesOpen(true)}>
                <IconTag className="size-4" />
                Kelola kategori
              </Button>
              <Button onClick={() => setUploadOpen(true)}>
                <IconPlus className="size-4" />
                Unggah dokumen
              </Button>
            </>
          )}
        </div>
      }
    >
      <div className="mb-5 flex flex-wrap items-end gap-4">
        <div className="min-w-56 flex-1 space-y-2 sm:max-w-sm">
          <label htmlFor="knowledge-search" className="text-sm font-medium">
            Cari dokumen
          </label>
          <Input
            id="knowledge-search"
            value={search}
            placeholder="Cari judul atau nama berkas…"
            onChange={(event) => {
              setSearch(event.target.value)
              setPage(0)
            }}
          />
        </div>
        <div className="space-y-2">
          <label htmlFor="knowledge-category-filter" className="text-sm font-medium">
            Kategori
          </label>
          <Select
            value={categoryFilter}
            onValueChange={(value) => {
              setCategoryFilter(value)
              setPage(0)
            }}
          >
            <SelectTrigger id="knowledge-category-filter" className="w-56">
              <SelectValue placeholder="Semua kategori" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_CATEGORIES}>Semua kategori</SelectItem>
              <SelectItem value={NO_CATEGORY}>Tanpa kategori</SelectItem>
              {(categories.data?.data ?? []).map((category) => (
                <SelectItem key={category.id} value={category.id}>
                  {category.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      {list.isError ? (
        <div role="alert" className="space-y-3 py-5">
          <p className="text-destructive text-sm">
            Dokumen gagal dimuat. Periksa akses Anda atau coba lagi.
          </p>
          <Button variant="outline" onClick={() => void list.refetch()}>
            Coba lagi
          </Button>
        </div>
      ) : list.isPending ? (
        <div role="status" className="space-y-3 py-4" aria-label="Memuat dokumen">
          {Array.from({ length: 5 }, (_, index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      ) : !list.data.data.length ? (
        <EmptyState
          icon={IconBook}
          title={
            search.trim() || categoryFilter !== ALL_CATEGORIES
              ? 'Dokumen tidak ditemukan'
              : page
                ? 'Tidak ada dokumen di halaman ini'
                : 'Belum ada dokumen'
          }
          description={
            search.trim() || categoryFilter !== ALL_CATEGORIES
              ? 'Coba ubah kata pencarian atau filter kategori.'
              : canManage
                ? 'Unggah dokumen untuk menambahkan referensi AI.'
                : 'Admin tenant belum menambahkan dokumen referensi.'
          }
          action={
            page > 0 ? (
              <Button variant="outline" onClick={() => setPage(0)}>
                Kembali ke halaman pertama
              </Button>
            ) : undefined
          }
        />
      ) : (
        <DataGrid
          // Match the existing ReactTable adapter for TanStack v9's feature-typed table.
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          table={table as unknown as ReactTable<any, KnowledgeDocument>}
          recordCount={list.data.metadata.total}
          tableLayout={{
            dense: true,
            width: 'fixed',
            columnsResizable: true,
            columnsPinnable: true,
            columnsMovable: true,
            columnsVisibility: true,
          }}
          tableClassNames={{ edgeCell: 'px-5' }}
        >
          <div className="w-full min-w-0 overflow-hidden">
            <DataGridContainer border={false}>
              <ScrollArea>
                <DataGridTable />
                <ScrollBar orientation="horizontal" />
              </ScrollArea>
            </DataGridContainer>
            <div className="border-line border-t px-5 py-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-ink-2 text-sm" role="status">
                  {page * 10 + 1}-{page * 10 + list.data.data.length} dari{' '}
                  {list.data.metadata.total} dokumen
                </p>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    disabled={page === 0 || list.isFetching}
                    onClick={() => setPage(page - 1)}
                  >
                    Sebelumnya
                  </Button>
                  <Button
                    variant="outline"
                    disabled={(page + 1) * 10 >= list.data.metadata.total || list.isFetching}
                    onClick={() => setPage(page + 1)}
                  >
                    Berikutnya
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </DataGrid>
      )}
      {detail && detail.scope === scope && (
        <ExtractionDetail
          key={JSON.stringify([scope, detail.document.id])}
          scope={scope}
          document={detail.document}
          canManage={canManage && detail.document.tenant_id === scope}
          onClose={() => setDetail(null)}
        />
      )}
      {canManage && categoriesOpen && (
        <CategoryDrawer
          scope={scope}
          scopeName={scopeName}
          open={categoriesOpen}
          onOpenChange={setCategoriesOpen}
        />
      )}
      {canManage && uploadOpen && (
        <UploadDrawer
          scope={scope}
          scopeName={scopeName}
          open={uploadOpen}
          onOpenChange={setUploadOpen}
        />
      )}
      <AlertDialog
        open={!!deleting}
        onOpenChange={(next) => {
          if (!next && !remove.isPending) setDeleting(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Hapus dokumen?</AlertDialogTitle>
            <AlertDialogDescription>
              Dokumen “{deleting?.title}” akan dihapus dan tidak lagi menjadi referensi AI. Tindakan
              ini tidak dapat dibatalkan.
            </AlertDialogDescription>
          </AlertDialogHeader>
          {remove.isError && (
            <p role="alert" className="text-destructive text-sm">
              Dokumen gagal dihapus. Periksa akses Anda, lalu coba lagi.
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
                if (!deleting || !canManage || deleting.tenant_id !== scope) return
                remove.mutate(
                  { scope, id: deleting.id },
                  {
                    onSuccess: () => {
                      setDeleting(null)
                      if (list.data?.data.length === 1 && page > 0) setPage(page - 1)
                      toast.success('Dokumen berhasil dihapus.')
                    },
                  },
                )
              }}
            >
              {remove.isPending ? 'Menghapus…' : 'Hapus dokumen'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  )
}
