import { useQuery } from '@tanstack/react-query'
import { useEffect, useState, useSyncExternalStore } from 'react'
import EmbeddingConfigDrawer from './embedding-config-drawer'
import EmptyState from '@/components/block/common/empty-state'
import { Button } from '@/components/ui/button'
import { queries } from '@/lib/api/queries'
import { getStoredAccessToken } from '@/lib/auth/token-storage'
import {
  getActiveTenantId,
  resolveActiveTenant,
  setActiveTenantId,
  subscribeActiveTenant,
} from '@/lib/auth/active-tenant'
import KnowledgeLibrary from './table'

export default function KnowledgePage() {
  const [configOpen, setConfigOpen] = useState(false)
  const storedTenant = useSyncExternalStore(subscribeActiveTenant, getActiveTenantId, () => null)
  const profile = useQuery(queries.auth.profile())
  const isPlatform = profile.data?.data?.role === 'super_user'
  const memberships = useQuery({
    ...queries.tenants.list(),
    enabled: profile.isSuccess && !isPlatform,
  })
  const tenant = resolveActiveTenant(memberships.data ?? [], storedTenant)
  const resolvedTenant = isPlatform ? null : (tenant?.id ?? null)
  useEffect(() => {
    if (
      getStoredAccessToken() &&
      profile.isSuccess &&
      (isPlatform || memberships.isSuccess) &&
      storedTenant !== resolvedTenant
    ) {
      setActiveTenantId(resolvedTenant)
    }
  }, [profile.isSuccess, isPlatform, memberships.isSuccess, storedTenant, resolvedTenant])
  // Do not mount scoped queries until storage (and the interceptor) agrees.
  const pending =
    profile.isPending ||
    (!isPlatform && memberships.isPending) ||
    (profile.isSuccess && (isPlatform || memberships.isSuccess) && storedTenant !== resolvedTenant)
  const failed = profile.isError || (!isPlatform && memberships.isError)
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-ink text-2xl font-semibold tracking-tight">Basis pengetahuan</h1>
        <p className="text-ink-2 text-[13px]">
          {isPlatform
            ? 'Pustaka global menjadi referensi percakapan pribadi superuser dan percakapan tenant.'
            : 'Percakapan tenant menggunakan pustaka global dan dokumen milik tenant aktif.'}
        </p>
      </header>
      {isPlatform && !failed && !pending && (
        <div>
          <Button variant="outline" onClick={() => setConfigOpen(true)}>
            Konfigurasi embedding
          </Button>
        </div>
      )}
      {isPlatform && configOpen && !failed && !pending && (
        <EmbeddingConfigDrawer onClose={() => setConfigOpen(false)} />
      )}
      {failed ? (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>Gagal memeriksa akses basis pengetahuan.</p>
          <Button
            variant="outline"
            onClick={() => {
              void profile.refetch()
              if (!isPlatform) void memberships.refetch()
            }}
          >
            Coba lagi
          </Button>
        </div>
      ) : pending ? (
        <p role="status" className="text-ink-2 text-sm">
          Memeriksa akses…
        </p>
      ) : isPlatform || tenant ? (
        <KnowledgeLibrary
          key={isPlatform ? 'platform' : `${tenant!.id}:${tenant!.role}`}
          scope={isPlatform ? null : tenant!.id}
          scopeName={isPlatform ? 'Pustaka global' : tenant!.name}
          canManage={isPlatform || tenant?.role === 'tenant_admin'}
        />
      ) : (
        <EmptyState
          title={memberships.data?.length ? 'Pilih tenant' : 'Belum ada tenant'}
          description={
            memberships.data?.length
              ? 'Pilih tenant aktif melalui pemilih tenant untuk membaca basis pengetahuan.'
              : 'Anda perlu menjadi anggota tenant untuk membaca basis pengetahuan.'
          }
        />
      )}
    </div>
  )
}
