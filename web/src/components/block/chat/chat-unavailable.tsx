import { IconBuildingCommunity, IconCoinOff } from '@tabler/icons-react'

import EmptyState from '@/components/block/common/empty-state'
import type { ChatAccessStatus } from '@/lib/api/models/chat'

type BlockedStatus = Exclude<ChatAccessStatus, 'ready'>

const COPY: Record<
  BlockedStatus,
  { title: string; description: string; icon: typeof IconCoinOff }
> = {
  no_tenant: {
    title: 'Chat belum dapat digunakan',
    description:
      'Akun Anda belum ditambahkan ke tenant. Hubungi admin platform agar akun Anda ditugaskan ke sebuah tenant.',
    icon: IconBuildingCommunity,
  },
  no_allocation: {
    title: 'Chat belum dapat digunakan',
    description:
      'Tenant Anda belum mendapat alokasi token. Hubungi admin platform untuk menambahkan alokasi, lalu muat ulang halaman ini.',
    icon: IconCoinOff,
  },
}

interface ChatUnavailableProps {
  status: BlockedStatus
}

export default function ChatUnavailable({ status }: ChatUnavailableProps) {
  const { title, description, icon } = COPY[status]
  return (
    <div
      role="status"
      className="bg-surface shadow-card flex min-h-[18rem] flex-1 items-center justify-center rounded-card px-4 py-10"
    >
      <EmptyState title={title} description={description} icon={icon} />
    </div>
  )
}
