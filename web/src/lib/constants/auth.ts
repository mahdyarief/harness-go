/** Application roles issued by the Go API (see server/internal/lib/constant). */
export const ROLES = {
  SUPER_USER: 'super_user',
  USER: 'user',
} as const

export type RoleName = (typeof ROLES)[keyof typeof ROLES]

export const AUTH_STORAGE_KEYS = {
  REFRESH_TOKEN: 'hsgRefreshToken',
  ACCESS_TOKEN: 'hsgAccessToken',
  ID_TOKEN: 'hsgIdToken',
  AUTH_STORAGE: 'hsgAuthStorage',
} as const

export enum AUTH_ERROR_TYPE {
  INVALID_LOGIN_CREDENTIALS = 'INVALID_LOGIN_CREDENTIALS',
  INVALID_CREDENTIALS = 'INVALID_CREDENTIALS',
}

export enum AUTH_PROVIDER {
  GOOGLE = 'google',
  CUSTOM = 'custom',
}
