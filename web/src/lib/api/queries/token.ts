import { mutationOptions, queryOptions } from '@tanstack/react-query'

import { DEFAULT_PAGINATE } from '@/lib/constants/paginate'
import { getQueryClient } from '@/lib/providers/react-query'

import type { PaginateDto } from '../dtos/paginate'
import type { TopUpDto } from '../dtos/token/schema'

import { tokenServices } from '../services/token'

export const TOKEN_QUERY_KEY = 'tokens'

export const tokenKeys = {
  balances: ['tokens', 'balances'] as const,
  balance: (tenantId: string) => ['tokens', 'balance', tenantId] as const,
  topups: (tenantId: string) => ['tokens', 'topups', tenantId] as const,
  usages: (tenantId: string) => ['tokens', 'usages', tenantId] as const,
}

export const tokenQueries = {
  balances: () =>
    queryOptions({
      queryKey: tokenKeys.balances,
      queryFn: tokenServices.listBalances,
    }),

  balance: (tenantId: string) =>
    queryOptions({
      queryKey: tokenKeys.balance(tenantId),
      queryFn: () => tokenServices.balance(tenantId),
      enabled: Boolean(tenantId),
    }),

  topups: (tenantId: string, params?: PaginateDto) =>
    queryOptions({
      queryKey: [...tokenKeys.topups(tenantId), params],
      queryFn: () => tokenServices.topups(tenantId, params ?? DEFAULT_PAGINATE),
      enabled: Boolean(tenantId),
    }),

  usages: (tenantId: string, params?: PaginateDto) =>
    queryOptions({
      queryKey: [...tokenKeys.usages(tenantId), params],
      queryFn: () => tokenServices.usages(tenantId, params ?? DEFAULT_PAGINATE),
      enabled: Boolean(tenantId),
    }),

  topUp: () => {
    const qc = getQueryClient()
    return mutationOptions({
      mutationFn: ({ tenantId, input }: { tenantId: string; input: TopUpDto }) =>
        tokenServices.topUp(tenantId, input),
      onSuccess: async (_data, { tenantId }) => {
        await Promise.all([
          qc.invalidateQueries({ queryKey: tokenKeys.balances }),
          qc.invalidateQueries({ queryKey: tokenKeys.balance(tenantId) }),
          qc.invalidateQueries({ queryKey: tokenKeys.topups(tenantId) }),
        ])
      },
    })
  },
}
