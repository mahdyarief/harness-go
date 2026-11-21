import { useQuery } from '@tanstack/react-query'
import { Link, useLocation } from '@tanstack/react-router'
import React, { useState } from 'react'

import ThemeToggle from '@/components/block/common/theme-toggle'
import TenantSwitcher from '@/components/block/common/tenant-switcher'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Separator } from '@/components/ui/separator'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { queries } from '@/lib/api/queries'
import { capitalizeFirstLetter } from '@/lib/string'

import AppSidebar from './app-sidebar'

interface SidebarLayoutProps {
  children: React.ReactNode
}

const SIDEBAR_COOKIE_NAME = 'sidebar_state'

/** Reads the state written by SidebarProvider's setOpen cookie, expanded by default. */
function readSidebarOpen(): boolean {
  const match = document.cookie.match(new RegExp(`(?:^|; )${SIDEBAR_COOKIE_NAME}=(true|false)`))
  return match ? match[1] === 'true' : true
}

export default function SidebarLayout({ children }: SidebarLayoutProps) {
  const pathname = useLocation().pathname
  const [sidebarOpen] = useState(readSidebarOpen)

  const { data: meData, isError, refetch } = useQuery(queries.auth.profile())

  const renderBreadcrumb = () => {
    const pathSegments = pathname.split('/').filter(Boolean)

    if (pathSegments.length === 0) {
      return null
    }

    if (pathSegments.length === 1) {
      return (
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbPage className="font-medium">
                {pathSegments[0] === 'knowledge'
                  ? 'Basis Pengetahuan'
                  : capitalizeFirstLetter(pathSegments[0])}
              </BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
      )
    }

    return (
      <Breadcrumb>
        <BreadcrumbList>
          {pathSegments.map((segment, index) => {
            const isLast = index === pathSegments.length - 1
            const href = '/' + pathSegments.slice(0, index + 1).join('/')
            const displaySegment = capitalizeFirstLetter(segment)

            const matchUUID = segment.match(
              /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
            )

            const newSegment = matchUUID ? '...' + displaySegment.slice(-8) : displaySegment

            return (
              <React.Fragment key={index}>
                <BreadcrumbItem className="hidden md:block">
                  {isLast ? (
                    <BreadcrumbPage className="font-medium">{newSegment}</BreadcrumbPage>
                  ) : (
                    <BreadcrumbLink className="font-medium" href={href} asChild>
                      <Link to={href}>{newSegment}</Link>
                    </BreadcrumbLink>
                  )}
                </BreadcrumbItem>
                {!isLast && <BreadcrumbSeparator className="hidden md:block" />}
              </React.Fragment>
            )
          })}
        </BreadcrumbList>
      </Breadcrumb>
    )
  }

  return (
    <SidebarProvider
      defaultOpen={sidebarOpen}
      style={
        {
          '--sidebar-width': '16rem',
        } as React.CSSProperties
      }
    >
      <a
        href="#main-content"
        className="bg-ink text-surface sr-only fixed left-4 top-4 z-50 rounded-control px-4 py-2 text-[13px] focus:not-sr-only"
      >
        Lewati navigasi
      </a>
      <AppSidebar
        user={meData?.data}
        userError={isError && !meData?.data}
        onRetryUser={() => void refetch()}
      />
      <SidebarInset>
        <header className="bg-surface border-line z-20 flex h-14 shrink-0 items-center gap-2 border-b">
          <div className="flex w-full items-center justify-between gap-2 px-3 sm:px-4">
            <div className="flex min-w-0 items-center gap-2">
              <SidebarTrigger className="-ml-1" />
              <Separator
                orientation="vertical"
                className="my-auto mr-2 data-[orientation=vertical]:h-4"
              />
              {renderBreadcrumb()}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <TenantSwitcher />
              <ThemeToggle />
            </div>
          </div>
        </header>
        <div
          id="main-content"
          tabIndex={-1}
          className="bg-canvas text-ink mx-auto flex w-full max-w-[100rem] min-h-0 min-w-0 flex-1 flex-col gap-5 overflow-y-auto p-4 sm:gap-6 sm:p-5 md:p-6 lg:p-8 2xl:px-10 focus:outline-none"
        >
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
