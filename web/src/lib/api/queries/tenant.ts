import { mutationOptions, queryOptions } from '@tanstack/react-query'
import { getQueryClient } from '@/lib/providers/react-query'
import type { AssignMemberDto, CreateTenantDto } from '../dtos/tenant/schema'
import { tenantServices } from '../services/tenant'

export const tenantKeys = {
  list: ['tenants', 'mine'] as const,
  members: (id: string) => ['tenants', id, 'members'] as const,
  platformList: ['tenants', 'platform'] as const,
  platformDirectory: ['tenants', 'platform', 'directory'] as const,
}
export const tenantQueries = {
  list: () => queryOptions({ queryKey: tenantKeys.list, queryFn: tenantServices.list }),
  platformList: () =>
    queryOptions({ queryKey: tenantKeys.platformList, queryFn: tenantServices.platformList }),
  platformDirectory: () =>
    queryOptions({
      queryKey: tenantKeys.platformDirectory,
      queryFn: tenantServices.platformDirectory,
    }),
  members: (id: string) =>
    queryOptions({
      queryKey: tenantKeys.members(id),
      queryFn: () => tenantServices.members(id),
      enabled: Boolean(id),
    }),
  create: () =>
    mutationOptions({
      mutationFn: (input: CreateTenantDto) => tenantServices.create(input),
      onSuccess: async () => {
        await Promise.all([
          getQueryClient().invalidateQueries({ queryKey: tenantKeys.list }),
          getQueryClient().invalidateQueries({ queryKey: tenantKeys.platformDirectory }),
        ])
      },
    }),
  assignMember: () =>
    mutationOptions({
      mutationKey: ['tenants', 'assign-member'],
      mutationFn: ({ id, input }: { id: string; input: AssignMemberDto }) =>
        tenantServices.assignMember(id, input),
      onSuccess: async (_data, { id }) => {
        await Promise.all([
          getQueryClient().invalidateQueries({ queryKey: tenantKeys.members(id) }),
          getQueryClient().invalidateQueries({ queryKey: tenantKeys.list }),
          getQueryClient().invalidateQueries({ queryKey: tenantKeys.platformDirectory }),
        ])
      },
    }),
}
