import type { AxiosItemResponse } from '@/types/api'

import type { Health } from '../../models/health'

/** /health answers the standard hsg envelope: `{ data: { status } }`. */
export type HealthResources = {
  get: () => Promise<AxiosItemResponse<Health>>
}
