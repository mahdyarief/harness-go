import { useQuery } from '@tanstack/react-query'
import { useQueryState } from 'nuqs'
import { useEffect, useMemo } from 'react'

import EmptyState from '@/components/block/common/empty-state'
import ReactTable from '@/components/block/common/react-table'
import SectionCard from '@/components/block/common/section-card'
import SelectInput from '@/components/block/common/select-input'
import { Input, InputWrapper } from '@/components/ui/input'
import { useDebounce } from '@/hooks/use-debounce'
import { usePaginationQuery } from '@/hooks/use-pagination-query'
import { toastAxiosError } from '@/lib/api/axios-error'
import type { AuditFilterDto } from '@/lib/api/dtos/platform/audit/schema'
import { queries } from '@/lib/api/queries'

import { useAuditColumns } from './column'

/** Action filter options; the values are the server's stable action slugs. */
const ACTION_OPTIONS = [
  { value: 'tenant.created', label: 'Tenant dibuat' },
  { value: 'tenant.member_assigned', label: 'Anggota tenant ditambahkan' },
  { value: 'token.topup', label: 'Top-up token' },
  { value: 'llm_config.updated', label: 'Konfigurasi LLM diperbarui' },
  { value: 'user.created', label: 'Pengguna dibuat' },
  { value: 'user.updated', label: 'Pengguna diperbarui' },
]

export default function AuditTable() {
  const { offset, limit, pageIndex } = usePaginationQuery()

  const [search, setSearch] = useQueryState('search')
  const [action, setAction] = useQueryState('action')
  const debouncedSearch = useDebounce({ value: search ?? '', delay: 500 })

  const params = useMemo<AuditFilterDto>(
    () => ({
      offset,
      limit,
      ...(debouncedSearch ? { search: debouncedSearch } : {}),
      ...(action ? { action } : {}),
    }),
    [offset, limit, debouncedSearch, action],
  )

  const { data, isFetching, isLoading, isError, error } = useQuery(queries.audit.list(params))
  const loading = isLoading || isFetching
  const total = data?.metadata?.total ?? 0
  const entries = data?.data ?? []

  useEffect(() => {
    if (isError) toastAxiosError(error)
  }, [isError, error])

  const columns = useAuditColumns({ loading })

  return (
    <SectionCard
      title="Audit Trail"
      description="Jejak aktivitas platform yang tidak dapat diubah. Setiap baris tercatat bersama mutasi yang mendahuluinya."
      flush={loading || entries.length > 0}
      toolbar={
        <div className="flex w-full flex-col items-stretch gap-2 sm:w-auto sm:flex-row sm:items-center">
          <InputWrapper className="w-full sm:w-72">
            <Input
              type="search"
              aria-label="Cari jejak audit"
              placeholder="Cari aksi, entitas, IP, atau request…"
              value={search ?? ''}
              onChange={(e) => void setSearch(e.target.value || null)}
            />
          </InputWrapper>

          <SelectInput
            placeholder="Semua aksi"
            className="w-full sm:w-56"
            options={ACTION_OPTIONS}
            defaultValue={action ?? undefined}
            onSelect={(value) => void setAction(value || null)}
          />
        </div>
      }
    >
      {!loading && entries.length === 0 ? (
        <EmptyState
          title="Belum ada jejak audit"
          description="Sesuaikan pencarian atau filter, atau lakukan aksi terlebih dahulu."
        />
      ) : (
        <ReactTable
          columns={columns}
          data={entries}
          total={total}
          pageIndex={pageIndex}
          pageSize={limit}
        />
      )}
    </SectionCard>
  )
}
