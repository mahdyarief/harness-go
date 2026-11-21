import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'

import UserTable from '@/components/block/platform/user/table'
import { Button } from '@/components/ui/button'
import { authQueries } from '@/lib/api/queries/auth'

export const Route = createFileRoute('/(app)/platform/users/')({
  component: RouteComponent,
})

function RouteComponent() {
  const profile = useQuery(authQueries.profile())

  if (profile.isPending) return <p role="status">Memeriksa akses…</p>
  if (profile.isError)
    return (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>Gagal memeriksa akses.</p>
        <Button
          variant="outline"
          disabled={profile.isFetching}
          onClick={() => void profile.refetch()}
        >
          Coba lagi
        </Button>
      </div>
    )
  // Hiding the menu is not authorization; the API enforces the same rule.
  if (profile.data?.data?.role !== 'super_user')
    return <p role="alert">Anda tidak memiliki akses ke halaman ini.</p>

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-ink text-2xl font-semibold tracking-tight">Pengguna</h1>
        <p className="text-ink-2 text-[13px]">
          Semua akun yang dikenal platform. Admin tenant dipilih dari daftar ini.
        </p>
      </div>

      <UserTable />
    </div>
  )
}
