import { createFileRoute } from '@tanstack/react-router'
import TenantPage from '@/components/block/tenants/tenant-page'
import { requireSession } from '@/lib/auth/handler'

export const Route = createFileRoute('/(app)/tenants/')({
  beforeLoad: async () => {
    await requireSession()
  },
  component: TenantPage,
})
