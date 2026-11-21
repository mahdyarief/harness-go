import type { ApiItemResponse } from '@/types/api'
import type { LlmConfigFormDto } from '../dtos/platform/llm-config/schema'
import type { LlmConfig, LlmTestResult } from '../models/llm-config'

import { api } from '../client-fetch'
import { axiosErrorMessage } from '../axios-error'

export const llmConfigService = {
  get: () => api.get<ApiItemResponse<LlmConfig>>('/api/v1/platform/llm-config'),
  update: async (body: LlmConfigFormDto): Promise<void> => {
    try {
      // PUT returns a message only, not the configuration read model.
      await api.put<{ message?: string }>('/api/v1/platform/llm-config', body)
    } catch (error) {
      // Axios errors retain request bodies, including write-only credentials, so
      // only the server's own message is surfaced — never the request.
      const message = axiosErrorMessage(error)
      throw new Error(`Konfigurasi LLM gagal disimpan. ${message}`)
    }
  },
  test: async (): Promise<LlmTestResult> => {
    try {
      // Project the response before it enters mutation state; never retain quotes.
      const response = await api.post<ApiItemResponse<LlmTestResult>>(
        '/api/v1/platform/llm-test',
        {},
        { validateStatus: (status) => [200, 409, 502, 504].includes(status) },
      )
      if (response.status === 409) return { status: 'disabled' }
      if (response.status === 504) return { status: 'timeout' }
      const status = response.data.data?.status
      if (response.status === 200) return { status: status === 'completed' ? 'completed' : 'failed' }
      if (status === 'provider_auth_failed' || status === 'rate_limited' || status === 'invalid_response') {
        return { status }
      }
      return { status: 'failed' }
    } catch {
      throw new Error('Uji koneksi gagal')
    }
  },
} as const
