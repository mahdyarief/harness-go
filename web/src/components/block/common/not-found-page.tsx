import { IconArrowLeft, IconError404 } from '@tabler/icons-react'
import { Link } from '@tanstack/react-router'

import EmptyState from '@/components/block/common/empty-state'
import { Button } from '@/components/ui/button'

/** Full-page 404 for routes that do not exist. */
export default function NotFoundPage() {
  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <EmptyState
        icon={IconError404}
        title="Halaman tidak ditemukan"
        description="Halaman yang Anda cari tidak ada atau sudah dipindahkan."
        action={
          <Button asChild>
            <Link to="/dashboard">
              <IconArrowLeft />
              <span>Kembali ke dasbor</span>
            </Link>
          </Button>
        }
      />
    </div>
  )
}
