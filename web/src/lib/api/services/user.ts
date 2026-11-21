import { HTTP_METHOD } from '@/types/api'

import type { UserMethods, UserResources } from './types/user'

import { api } from '../client-fetch'
import type { UserTenantMembership } from '../models/user'
import { clientResource } from '../resource'

const path = '/api/v1/users'

// The platform admin provisions users here; Authula still owns the identities,
// and the handler mirrors every write back into the users table.
const methods: UserMethods = [HTTP_METHOD.GET, HTTP_METHOD.POST, HTTP_METHOD.PUT]

type Envelope<T> = { data: T }

const resources = (): UserResources => {
  return {
    ...clientResource(path, methods),
  }
}

export const userServices = {
  ...resources(),
  // The edit drawer prefills the tenant picker from this; null means the user
  // belongs to no tenant yet.
  async membership(userId: string) {
    return (
      await api.get<Envelope<UserTenantMembership | null>>(
        `${path}/${encodeURIComponent(userId)}/membership`,
      )
    ).data.data
  },
}