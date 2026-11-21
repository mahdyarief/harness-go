import {
  IconBuildingCommunity,
  IconChevronRight,
  IconSearch,
  IconUsersGroup,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import EmptyState from '@/components/block/common/empty-state'
import SectionCard from '@/components/block/common/section-card'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Input, InputWrapper } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { toastAxiosError } from '@/lib/api/axios-error'
import type { PlatformTenantGroup, PlatformTenantMember } from '@/lib/api/models/tenant'
import { tenantQueries } from '@/lib/api/queries/tenant'
import type { TenantRole } from '@/lib/api/dtos/tenant/schema'
import { cn } from '@/lib/utils'

const roleLabel = (role: TenantRole) => (role === 'tenant_admin' ? 'Admin tenant' : 'Reviewer')

function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  return parts
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? '')
    .join('')
}

function matches(member: PlatformTenantMember, query: string) {
  const haystack = `${member.name} ${member.email} ${member.user_id}`.toLowerCase()
  return haystack.includes(query)
}

function MemberRow({ member }: { member: PlatformTenantMember }) {
  const label = member.name || member.user_id
  return (
    <li className="flex items-center gap-3 py-2.5">
      <Avatar className="size-8">
        <AvatarFallback>{initials(label)}</AvatarFallback>
      </Avatar>
      <div className="min-w-0 flex-1">
        <p className="text-ink truncate text-[13px] font-medium">{label}</p>
        {member.email ? (
          <p className="text-ink-2 truncate font-mono text-xs">{member.email}</p>
        ) : (
          <p className="text-ink-3 truncate text-xs">Profil pengguna tidak tersedia</p>
        )}
      </div>
      <Badge
        variant={member.role === 'tenant_admin' ? 'info' : 'secondary'}
        appearance="light"
        size="sm"
        className="shrink-0"
      >
        {roleLabel(member.role)}
      </Badge>
    </li>
  )
}

function TenantGroup({ tenant, open }: { tenant: PlatformTenantGroup; open: boolean }) {
  const [expanded, setExpanded] = useState(open)
  return (
    <Collapsible
      open={expanded}
      onOpenChange={setExpanded}
      className="bg-surface rounded-control shadow-hairline overflow-hidden"
    >
      <CollapsibleTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          autoHeight
          className="hover:bg-hover-2 flex w-full items-center gap-3 rounded-none px-3 py-3 text-left"
        >
          <span className="bg-hover-2 text-ink-2 flex size-8 shrink-0 items-center justify-center rounded-control">
            <IconBuildingCommunity className="size-4" aria-hidden />
          </span>
          <span className="min-w-0 flex-1">
            <span className="text-ink block truncate text-[13px] font-medium">{tenant.name}</span>
            <span className="text-ink-2 block text-xs">
              {tenant.members.length === 0
                ? 'Belum ada anggota'
                : `${tenant.members.length} anggota`}
            </span>
          </span>
          <IconChevronRight
            className={cn(
              'text-ink-3 size-4 shrink-0 transition-transform duration-200',
              expanded && 'rotate-90',
            )}
            aria-hidden
          />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="border-line border-t px-3 pb-1">
          {tenant.members.length === 0 ? (
            <p className="text-ink-2 py-3 text-[13px]">Tenant ini belum memiliki anggota.</p>
          ) : (
            <ul className="divide-line divide-y">
              {tenant.members.map((member) => (
                <MemberRow key={member.user_id} member={member} />
              ))}
            </ul>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

/**
 * Every tenant on the platform, grouped with its members. Super-user only:
 * the API rejects the directory for any other role.
 */
export default function TenantDirectory() {
  const [search, setSearch] = useState('')
  const directory = useQuery(tenantQueries.platformDirectory())

  const query = search.trim().toLowerCase()
  const groups = useMemo(() => {
    const all = directory.data ?? []
    if (!query) return all
    return all.flatMap((tenant) => {
      const tenantMatches = tenant.name.toLowerCase().includes(query)
      const members = tenant.members.filter((member) => matches(member, query))
      // A tenant-name hit keeps all of its members; a member hit narrows the
      // list to the matching people.
      if (tenantMatches) return [tenant]
      return members.length > 0 ? [{ ...tenant, members }] : []
    })
  }, [directory.data, query])

  const tenantCount = directory.data?.length ?? 0
  const memberCount =
    directory.data?.reduce((total, tenant) => total + tenant.members.length, 0) ?? 0

  return (
    <SectionCard
      title="Direktori tenant"
      description="Semua tenant di platform beserta pengguna yang menjadi anggotanya."
      toolbar={
        <InputWrapper className="w-full lg:w-72">
          <IconSearch aria-hidden />
          <Input
            type="search"
            aria-label="Cari tenant atau pengguna"
            placeholder="Cari tenant, nama, atau email…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </InputWrapper>
      }
    >
      {directory.isPending ? (
        <div className="space-y-2" role="status" aria-label="Memuat direktori tenant">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-14 w-full" />
          ))}
        </div>
      ) : directory.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3 py-6">
          <p className="text-ink text-[13px]">Direktori tenant tidak dapat dimuat.</p>
          <Button
            variant="outline"
            size="sm"
            disabled={directory.isFetching}
            onClick={() => {
              void directory.refetch().catch(toastAxiosError)
            }}
          >
            Coba lagi
          </Button>
        </div>
      ) : groups.length === 0 ? (
        <EmptyState
          icon={IconUsersGroup}
          title={query ? 'Tidak ada hasil' : 'Belum ada tenant'}
          description={
            query
              ? 'Tidak ada tenant atau pengguna yang cocok dengan pencarian.'
              : 'Buat tenant pertama dari formulir di atas.'
          }
          action={
            query ? (
              <Button variant="outline" size="sm" onClick={() => setSearch('')}>
                Hapus pencarian
              </Button>
            ) : undefined
          }
        />
      ) : (
        <>
          <p className="text-ink-2 mb-3 text-xs tabular-nums">
            {tenantCount} tenant · {memberCount} anggota
          </p>
          <div className="flex flex-col gap-2">
            {groups.map((tenant) => (
              <TenantGroup key={tenant.id} tenant={tenant} open={false} />
            ))}
          </div>
        </>
      )}
    </SectionCard>
  )
}
