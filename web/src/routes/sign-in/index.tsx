import { IconBox, IconLock } from '@tabler/icons-react'
import { createFileRoute } from '@tanstack/react-router'

import LoginForm from '@/components/block/auth/login-form'
import ThemeToggle from '@/components/block/common/theme-toggle'
import { APP_NAME } from '@/lib/app-name'
import { redirectIfAuthenticated } from '@/lib/auth/handler'

export const Route = createFileRoute('/sign-in/')({
  beforeLoad: () => redirectIfAuthenticated('/dashboard'),
  component: RouteComponent,
})

function RouteComponent() {
  return (
    <div className="bg-canvas text-ink flex min-h-svh flex-col">
      <header className="flex h-20 items-center justify-between px-6 sm:px-10">
        <div className="flex items-center gap-2.5 text-[13px] font-semibold tracking-tight">
          <IconBox className="size-5" aria-hidden="true" />{APP_NAME}
        </div>
        <ThemeToggle />
      </header>
      <main className="grid flex-1 place-items-center px-5 py-10">
        <div className="w-full max-w-[400px]">
          <div className="bg-surface rounded-card shadow-card p-6 sm:p-8">
            <LoginForm />
          </div>
          <p className="text-ink-2 mt-6 flex items-center justify-center gap-2 text-xs">
            <IconLock className="size-3.5" aria-hidden="true" />Akses sesuai peran dan keanggotaan tenant.
          </p>
        </div>
      </main>
      <footer className="text-ink-2 px-6 py-6 text-center text-xs">
        {APP_NAME} · Platform administrasi dan ruang kerja AI
      </footer>
    </div>
  )
}
