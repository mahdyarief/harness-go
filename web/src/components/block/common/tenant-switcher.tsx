import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useQueryState } from 'nuqs'
import { useEffect, useSyncExternalStore } from 'react'

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { queries } from '@/lib/api/queries'
import {
  getActiveTenantId,
  resolveActiveTenant,
  setActiveTenantId,
  subscribeActiveTenant,
} from '@/lib/auth/active-tenant'
import { getStoredAccessToken } from '@/lib/auth/token-storage'
import { ROLES } from '@/lib/constants/auth'

/** Cached data that depends on the active tenant; membership and profile do not. */
const isTenantScoped = (query: { queryKey: readonly unknown[] }) =>
  query.queryKey[0] !== 'tenants' && query.queryKey[0] !== 'profile'

/**
 * Header switcher for members of more than one tenant. Hidden for platform
 * admins (they act on the platform, not inside one tenant) and for users with a
 * single membership. The pick is sent as X-Tenant-ID; the API re-checks the
 * membership, so this is a preference rather than a permission.
 */
export default function TenantSwitcher() {
  const queryClient = useQueryClient()
  const [, setSessionId] = useQueryState('session')
  const profile = useQuery(queries.auth.profile())
  const isUser = profile.data?.data?.role === ROLES.USER
  const tenants = useQuery({ ...queries.tenants.list(), enabled: isUser })

  const memberships = tenants.data ?? []
  const stored = useSyncExternalStore(subscribeActiveTenant, getActiveTenantId, () => null)
  const selected = resolveActiveTenant(memberships, stored)
  const resolvedTenant = isUser ? (selected?.id ?? null) : null

  // A sole membership is safe to pin; multiple memberships require a choice.
  useEffect(() => {
    if (
      getStoredAccessToken() &&
      profile.isSuccess &&
      (!isUser || tenants.isSuccess) &&
      stored !== resolvedTenant
    ) {
      setActiveTenantId(resolvedTenant)
      void queryClient.invalidateQueries({ predicate: isTenantScoped })
    }
  }, [profile.isSuccess, isUser, tenants.isSuccess, stored, resolvedTenant, queryClient])

  if (!isUser || memberships.length < 2) return null

  const handleChange = (id: string) => {
    if (id === selected?.id) return
    setActiveTenantId(id)
    void setSessionId(null)
    // Everything cached belongs to the previous tenant; the membership list
    // and profile are tenant-independent and stay.
    void queryClient.resetQueries({ predicate: isTenantScoped })
  }

  return (
    <Select value={selected?.id ?? ''} onValueChange={handleChange}>
      <SelectTrigger size="sm" aria-label="Ganti tenant" className="w-auto max-w-44 sm:max-w-56">
        <SelectValue placeholder="Pilih tenant" />
      </SelectTrigger>
      <SelectContent align="end">
        {memberships.map((tenant) => (
          <SelectItem key={tenant.id} value={tenant.id}>
            {tenant.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
