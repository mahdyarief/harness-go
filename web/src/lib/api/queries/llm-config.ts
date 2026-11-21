import { mutationOptions, queryOptions } from '@tanstack/react-query'

import { getQueryClient } from '@/lib/providers/react-query'
import type { LlmConfigFormDto } from '../dtos/platform/llm-config/schema'
import { llmConfigService } from '../services/llm-config'

export const LLM_CONFIG_QUERY_KEY = 'platform-llm-config'

export const llmConfigQueries = {
  get: () =>
    queryOptions({
      queryKey: [LLM_CONFIG_QUERY_KEY],
      queryFn: async () => (await llmConfigService.get()).data,
    }),
  update: () =>
    mutationOptions({
      gcTime: 0,
      retry: false,
      mutationFn: async (body: LlmConfigFormDto) => {
        try {
          await llmConfigService.update(body)
        } finally {
          // Mutation variables must not retain the write-only credential.
          delete body.credential
        }
      },
      onSuccess: () => getQueryClient().invalidateQueries({ queryKey: [LLM_CONFIG_QUERY_KEY] }),
    }),
  test: () =>
    mutationOptions({
      gcTime: 0,
      retry: false,
      mutationFn: () => llmConfigService.test(),
    }),
} as const
