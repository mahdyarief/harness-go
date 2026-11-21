import { useQuery } from '@tanstack/react-query'

import SectionCard from '@/components/block/common/section-card'
import { Badge } from '@/components/ui/badge'
import { queries } from '@/lib/api/queries'

/** API liveness, polled by the health query. */
export default function HealthCard() {
  const { data, isLoading, isError } = useQuery(queries.health.get())
  const healthy = !isError && data?.status === 'ok'

  return (
    <SectionCard title="Status API" description="GET /health — diperbarui setiap 30 detik.">
      {isLoading ? (
        <p className="text-[13px] text-ink-2">Memeriksa…</p>
      ) : (
        <div className="space-y-1.5 text-[13px]">
          <div className="flex items-center justify-between gap-4">
            <span className="text-ink-2">Koneksi database</span>
            <Badge variant={healthy ? 'success' : 'destructive'}>
              {healthy ? 'Tersedia' : 'Tidak tersedia'}
            </Badge>
          </div>
          {isError ? (
            <p className="text-ink-2 text-xs">
              Pemeriksaan gagal — server tidak merespons. Coba muat ulang halaman.
            </p>
          ) : null}
        </div>
      )}
    </SectionCard>
  )
}
