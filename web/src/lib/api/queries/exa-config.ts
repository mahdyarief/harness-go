import { mutationOptions, queryOptions } from '@tanstack/react-query'

import { getQueryClient } from '@/lib/providers/react-query'
import type { ExaConfigFormDto } from '../dtos/platform/exa-config/schema'
import { exaConfigService } from '../services/exa-config'

export const EXA_CONFIG_QUERY_KEY = 'platform-exa-config'

export const exaConfigQueries = {
  get: () =>
    queryOptions({
      queryKey: [EXA_CONFIG_QUERY_KEY],
      queryFn: async () => (await exaConfigService.get()).data,
    }),
  update: () =>
    mutationOptions({
      gcTime: 0,
      retry: false,
      mutationFn: async (body: ExaConfigFormDto) => {
        try {
          await exaConfigService.update(body)
        } finally {
          // Mutation variables must not retain the write-only API keys.
          delete body.keys
        }
      },
      onSuccess: () => getQueryClient().invalidateQueries({ queryKey: [EXA_CONFIG_QUERY_KEY] }),
    }),
  test: () =>
    mutationOptions({
      gcTime: 0,
      retry: false,
      mutationFn: () => exaConfigService.test(),
    }),
} as const
