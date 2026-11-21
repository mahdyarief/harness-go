import { createFileRoute } from '@tanstack/react-router'
import { useQueryState } from 'nuqs'

import TokenBalanceTable from '@/components/block/platform/token/table'

export const Route = createFileRoute('/(app)/platform/tokens/')({
  component: RouteComponent,
})

function RouteComponent() {
  const [page, setPage] = useQueryState('page')
  const [pageSize, setPageSize] = useQueryState('pageSize')

  const parsedPage = Number.parseInt(page ?? '', 10)
  const pageIndex = Number.isFinite(parsedPage) && parsedPage >= 0 ? parsedPage : 0

  const parsedPageSize = Number.parseInt(pageSize ?? '', 10)
  const limit = Number.isFinite(parsedPageSize) && parsedPageSize >= 0 ? parsedPageSize : 10

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-ink text-2xl font-semibold tracking-tight">Alokasi Token</h1>
        <p className="text-ink-2 text-[13px]">
          Kelola saldo token untuk setiap tenant. Saldo berkurang otomatis saat pengguna
          memakai AI chat.
        </p>
      </div>

      <TokenBalanceTable
        pageIndex={pageIndex}
        pageSize={limit}
        onPageChange={(p) => {
          void setPage(String(p))
        }}
        onPageSizeChange={(s) => {
          void setPageSize(String(s))
        }}
      />
    </div>
  )
}
