import type { ApiItemResponse } from '@/types/api'
import type { ExaConfigFormDto } from '../dtos/platform/exa-config/schema'
import type { ExaConfig, ExaTestResult } from '../models/exa-config'

import { api } from '../client-fetch'
import { axiosErrorMessage } from '../axios-error'

export const exaConfigService = {
  get: () => api.get<ApiItemResponse<ExaConfig>>('/api/v1/platform/exa-config'),
  update: async (body: ExaConfigFormDto): Promise<void> => {
    try {
      // PUT returns a message only, not the configuration read model.
      await api.put<{ message?: string }>('/api/v1/platform/exa-config', body)
    } catch (error) {
      // Axios errors retain request bodies, including write-only API keys, so
      // only the server's own message is surfaced — never the request.
      const message = axiosErrorMessage(error)
      throw new Error(`Konfigurasi Exa gagal disimpan. ${message}`)
    }
  },
  test: async (): Promise<ExaTestResult> => {
    try {
      const response = await api.post<ApiItemResponse<ExaTestResult>>(
        '/api/v1/platform/exa-test',
        {},
        { validateStatus: (status) => [200, 409, 502, 504].includes(status) },
      )
      if (response.status === 409) return { status: 'disabled' }
      if (response.status === 504) return { status: 'timeout' }
      const status = response.data.data?.status
      if (response.status === 200)
        return { status: status === 'completed' ? 'completed' : 'failed' }
      if (
        status === 'provider_auth_failed' ||
        status === 'rate_limited' ||
        status === 'invalid_response'
      ) {
        return { status }
      }
      return { status: 'failed' }
    } catch {
      throw new Error('Uji koneksi Exa gagal')
    }
  },
} as const
