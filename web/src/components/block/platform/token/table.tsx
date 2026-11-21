import { useQuery } from '@tanstack/react-query'

import { tokenQueries } from '@/lib/api/queries/token'

import EmptyState from '../../common/empty-state'
import ReactTable from '../../common/react-table'
import SectionCard from '../../common/section-card'
import { useTokenBalanceColumns } from './column'

interface TokenBalanceTableProps {
  pageIndex: number
  pageSize: number
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
}

export default function TokenBalanceTable({
  pageIndex,
  pageSize,
  onPageChange,
  onPageSizeChange,
}: TokenBalanceTableProps) {
  const { data, isPending, isError } = useQuery(tokenQueries.balances())
  const balances = data ?? []
  const columns = useTokenBalanceColumns({ loading: isPending })

  if (isError) {
    return (
      <SectionCard title="Alokasi Token per Tenant">
        <p className="text-sm text-red">Gagal memuat data alokasi token.</p>
      </SectionCard>
    )
  }

  if (!isPending && balances.length === 0) {
    return (
      <SectionCard title="Alokasi Token per Tenant">
        <EmptyState
          title="Belum ada tenant"
          description="Buat tenant terlebih dahulu sebelum mengalokasikan token."
        />
      </SectionCard>
    )
  }

  return (
    <SectionCard
      title="Alokasi Token per Tenant"
      description="Saldo token tersedia untuk setiap tenant. Saldo berkurang otomatis saat pengguna memakai AI chat."
      flush
    >
      <ReactTable
        columns={columns}
        data={balances}
        total={balances.length}
        pageIndex={pageIndex}
        pageSize={pageSize}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
        getRowId={(row) => row.tenant_id}
      />
    </SectionCard>
  )
}
