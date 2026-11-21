const STORAGE_KEY = 'hsgActiveTenant'
let fallbackTenant: string | null | undefined

/** Header the API reads to pick which membership a request acts in. */
export const ACTIVE_TENANT_EVENT = 'hsg:active-tenant-changed'

export const ACTIVE_TENANT_HEADER = 'X-Tenant-ID'

/** Tenant the user picked in the switcher; null means "server default". */
export function getActiveTenantId(): string | null {
  if (fallbackTenant !== undefined) return fallbackTenant
  try {
    return window.localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

export function setActiveTenantId(id: string | null) {
  if (typeof window === 'undefined') return
  try {
    if (id) window.localStorage.setItem(STORAGE_KEY, id)
    else window.localStorage.removeItem(STORAGE_KEY)
    fallbackTenant = undefined
  } catch {
    // Keep UI and request headers aligned even when persistence is blocked.
    fallbackTenant = id
  }
  window.dispatchEvent(new Event(ACTIVE_TENANT_EVENT))
}

/** Headers to merge into a request so it acts in the selected tenant. */
export function subscribeActiveTenant(onChange: () => void) {
  window.addEventListener(ACTIVE_TENANT_EVENT, onChange)
  window.addEventListener('storage', onChange)
  return () => {
    window.removeEventListener(ACTIVE_TENANT_EVENT, onChange)
    window.removeEventListener('storage', onChange)
  }
}

// Only a sole membership is unambiguous without an explicit selection.
export function resolveActiveTenant<T extends { id: string }>(
  memberships: readonly T[],
  stored: string | null | undefined,
): T | undefined {
  return (
    memberships.find((tenant) => tenant.id === stored) ??
    (memberships.length === 1 ? memberships[0] : undefined)
  )
}

export function activeTenantHeaders(): Record<string, string> {
  const id = getActiveTenantId()
  return id ? { [ACTIVE_TENANT_HEADER]: id } : {}
}
