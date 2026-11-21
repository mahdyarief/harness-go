import { IconBox } from '@tabler/icons-react'
import { Link } from '@tanstack/react-router'
import { X } from 'lucide-react'
import React from 'react'

import type { Models } from '@/lib/api/models'
import type { UserInfo } from '@/types/menu'

import { Button } from '@/components/ui/button'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { getSidebarMenu } from '@/data/sidebar-menu'
import { APP_NAME } from '@/lib/app-name'

import NavMain from './nav-main'
import NavUser from './nav-user'

type AppSidebarProps = React.ComponentProps<typeof Sidebar> & {
  /** The signed-in user from GET /api/v1/me; undefined while it loads. */
  user?: Models.User | null
  /** True when the profile request failed and no data is available. */
  userError?: boolean
  /** Refetches the profile request, wired to the error state's retry. */
  onRetryUser?: () => void
}

export default function AppSidebar({ user, userError, onRetryUser, ...props }: AppSidebarProps) {
  const { isMobile, setOpenMobile } = useSidebar()
  const userInfo: UserInfo | undefined = user
    ? {
        name: user.name,
        email: user.email,
        avatar: user.image ?? '',
      }
    : undefined

  const menu = getSidebarMenu(user?.role)

  return (
    <Sidebar collapsible="icon" variant="sidebar" {...props}>
      <SidebarHeader className="relative">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild className="pr-11">
              <Link to="/dashboard" onClick={() => isMobile && setOpenMobile(false)}>
                <div className="bg-ink text-surface flex aspect-square size-8 items-center justify-center rounded-control">
                  <IconBox className="size-5" />
                </div>
                <div className="flex flex-col gap-1 leading-none">
                  <span className="text-ink text-[13px] font-medium">{APP_NAME}</span>
                  <span className="text-ink-2 text-xs">
                    {user?.role === 'super_user' ? 'Platform admin' : 'Ruang kerja'}
                  </span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        {isMobile && (
          <Button
            variant="ghost"
            size="icon"
            aria-label="Tutup menu"
            className="text-ink-2 hover:text-ink absolute top-1/2 right-2 -translate-y-1/2"
            onClick={() => setOpenMobile(false)}
          >
            <X />
          </Button>
        )}
      </SidebarHeader>
      <SidebarContent>
        <NavMain title="Menu" items={menu.navMenu.overview} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={userInfo} userError={userError} onRetryUser={onRetryUser} />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
