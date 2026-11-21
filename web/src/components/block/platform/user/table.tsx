import { IconPlus, IconSearch } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useQueryState } from 'nuqs'
import { useEffect, useMemo, useState } from 'react'

import EmptyState from '@/components/block/common/empty-state'
import ReactTable from '@/components/block/common/react-table'
import SectionCard from '@/components/block/common/section-card'
import { Button } from '@/components/ui/button'
import { Input, InputWrapper } from '@/components/ui/input'
import { useDebounce } from '@/hooks/use-debounce'
import { usePaginationQuery } from '@/hooks/use-pagination-query'
import { toastAxiosError } from '@/lib/api/axios-error'
import { queries } from '@/lib/api/queries'

import { UserColumn } from './column'
import { CreateUserDrawer } from './form'

export default function UserTable() {
  const [openAdd, setOpenAdd] = useState(false)
  const { offset, limit, pageIndex } = usePaginationQuery()

  const [search, setSearch] = useQueryState('search')
  const debouncedSearch = useDebounce({ value: search ?? '', delay: 500 })

  const params = useMemo(
    () => ({
      offset,
      limit,
      ...(debouncedSearch ? { search: debouncedSearch } : {}),
    }),
    [offset, limit, debouncedSearch],
  )

  const { data, isFetching, isLoading, isError, error } = useQuery(queries.users.list(params))
  const loading = isLoading || isFetching
  const total = data?.metadata?.total ?? 0
  const users = data?.data ?? []

  useEffect(() => {
    if (isError) toastAxiosError(error)
  }, [isError, error])

  const columns = UserColumn({ loading })

  return (
    <SectionCard
      title="Pengguna"
      description="Kelola akun platform dan keanggotaan tenant. Akun baru dapat langsung masuk menggunakan email dan kata sandi yang diberikan."
      flush={loading || users.length > 0}
      toolbar={
        <div className="flex w-full flex-col items-stretch gap-2 sm:w-auto sm:flex-row sm:items-center">
          <InputWrapper className="w-full sm:w-72">
            <IconSearch />
            <Input
              type="search"
              aria-label="Cari pengguna berdasarkan nama atau email"
              placeholder="Cari nama atau email…"
              value={search ?? ''}
              onChange={(e) => void setSearch(e.target.value || null)}
            />
          </InputWrapper>

          <Button size="sm" onClick={() => setOpenAdd(true)}>
            <IconPlus />
            <span>Tambah pengguna</span>
          </Button>
        </div>
      }
    >
      {!loading && users.length === 0 ? (
        <EmptyState
          title="Belum ada pengguna"
          description="Sesuaikan pencarian, atau buat akun pertama."
          action={
            <Button onClick={() => setOpenAdd(true)}>
              <IconPlus />
              <span>Tambah pengguna</span>
            </Button>
          }
        />
      ) : (
        <ReactTable
          columns={columns}
          data={users}
          total={total}
          pageIndex={pageIndex}
          pageSize={limit}
        />
      )}

      <CreateUserDrawer open={openAdd} onOpenChange={setOpenAdd} />
    </SectionCard>
  )
}
