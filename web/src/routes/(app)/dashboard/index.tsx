import { IconArrowUpRight, IconMessageChatbot } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'

import HealthCard from '@/components/block/dashboard/health-card'
import { Button } from '@/components/ui/button'
import { queries } from '@/lib/api/queries'
import { getSidebarMenu } from '@/data/sidebar-menu'

export const Route = createFileRoute('/(app)/dashboard/')({ component: RouteComponent })

function RouteComponent() {
  const { data } = useQuery(queries.auth.profile())
  const user = data?.data
  const links = getSidebarMenu(user?.role).navMenu.overview
    .flatMap((item) => item.items.length ? item.items : [item])
    .filter((item) => item.url !== '/dashboard' && item.url !== '/chat')
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <p className="text-ink-2 text-xs font-medium">Ruang kerja / Ringkasan</p>
          <h1 className="text-ink text-2xl font-semibold tracking-tight">Selamat datang{user?.name ? `, ${user.name}` : ''}.</h1>
          <p className="text-ink-2 text-[13px]">Kelola akses dan konfigurasi, atau lanjutkan percakapan AI Anda.</p>
        </div>
        <Button asChild><Link to="/chat"><IconMessageChatbot className="size-4" />Buka Chat AI</Link></Button>
      </header>
      <section aria-labelledby="workspace-heading" className="grid gap-6 lg:grid-cols-[1fr_320px]">
        <div className="bg-surface shadow-card overflow-hidden rounded-card">
          <h2 id="workspace-heading" className="border-line text-ink border-b px-5 py-4 text-[13px] font-semibold">Kelola ruang kerja</h2>
          <div className="divide-line divide-y">
            {links.map((item) => (
              <Link key={item.url} to={item.url} className="hover:bg-hover-2 focus-visible:ring-line-strong flex items-center gap-3 px-5 py-4 outline-none focus-visible:ring-2 focus-visible:ring-inset">
                {item.icon && <item.icon className="text-ink-2 size-4" aria-hidden="true" />}
                <span className="text-ink flex-1 text-[13px] font-medium">{item.title}</span><IconArrowUpRight className="text-ink-2 size-4" aria-hidden="true" />
              </Link>
            ))}
          </div>
        </div>
        <HealthCard />
      </section>
    </div>
  )
}
