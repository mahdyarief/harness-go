import z from 'zod'

import { optionalString, requiredEnum, requiredString } from '@/lib/validation'

const PLATFORM_ROLE_VALUES = ['user', 'super_user'] as const
const TENANT_ROLE_VALUES = ['tenant_admin', 'reviewer'] as const

export type PlatformRole = (typeof PLATFORM_ROLE_VALUES)[number]
export type TenantRole = (typeof TENANT_ROLE_VALUES)[number]

// Cross-field rule: tenant_id and tenant_role must both be present or both
// absent. The Go validator checks each field on its own and leaves the pairing
// to the handler, so the drawer rejects a half-filled pair before it is sent.
const isPaired = (data: { tenant_id?: string; tenant_role?: string }) =>
  (data.tenant_id === '' || data.tenant_id === undefined) ===
  (data.tenant_role === '' || data.tenant_role === undefined)

// Empty means "no tenant"; anything else must be a role the membership CHECK allows.
const tenantRole = z
  .union([z.literal(''), requiredEnum(TENANT_ROLE_VALUES, 'tenant_role')])
  .optional()

const PAIRING_MESSAGE = 'Tenant dan peran tenant harus dipilih bersamaan.'

export const createUserSchema = z
  .object({
    name: requiredString('name').max(100, 'Nama maksimal 100 karakter.'),
    email: requiredString('email')
      .email('Alamat email tidak valid.')
      .max(254, 'Email maksimal 254 karakter.'),
    // Admin-set password, so no trimming: trailing whitespace is significant.
    password: z
      .string({ error: 'Kata sandi harus berupa teks.' })
      .regex(/^.{12,128}$/, 'Kata sandi harus 12–128 karakter.'),
    role: requiredEnum(PLATFORM_ROLE_VALUES, 'role'),
    tenant_id: optionalString('tenant_id'),
    tenant_role: tenantRole,
  })
  .refine(isPaired, { message: PAIRING_MESSAGE })

export const updateUserSchema = z
  .object({
    name: requiredString('name').max(100, 'Nama maksimal 100 karakter.'),
    email: requiredString('email')
      .email('Alamat email tidak valid.')
      .max(254, 'Email maksimal 254 karakter.'),
    // An empty password leaves the credentials untouched server-side.
    password: z
      .string({ error: 'Kata sandi harus berupa teks.' })
      .max(128, 'Kata sandi maksimal 128 karakter.')
      .refine((value) => value === '' || value.length >= 12, {
        message: 'Kata sandi minimal 12 karakter.',
      }),
    role: requiredEnum(PLATFORM_ROLE_VALUES, 'role'),
    tenant_id: optionalString('tenant_id'),
    tenant_role: tenantRole,
  })
  .refine(isPaired, { message: PAIRING_MESSAGE })

export type CreateUserDto = z.infer<typeof createUserSchema>
export type UpdateUserDto = z.infer<typeof updateUserSchema>
