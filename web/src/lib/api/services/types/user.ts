import type { HTTP_METHOD, Resources } from '@/types/api'

import type { Models } from '../../models'

/**
 * The methods `userServices` actually builds. The platform admin provisions
 * users here (Authula user + email account + mirror row), so the directory is
 * writable; there is no delete because Authula owns the lifecycle.
 */
export type UserMethods = [HTTP_METHOD.GET, HTTP_METHOD.POST, HTTP_METHOD.PUT]

export type UserResources = Resources<UserMethods, Models.User>
