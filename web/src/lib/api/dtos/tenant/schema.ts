import { z } from 'zod'

export const tenantRoleSchema = z.enum(['tenant_admin', 'reviewer'])
export const createTenantSchema = z.object({
  name: z.string().trim().min(1, 'Nama wajib diisi').max(100),
  admin_user_id: z.string().trim().uuid('Masukkan UUID yang valid'),
})
export const assignMemberSchema = z.object({
  user_id: z.string().trim().uuid('Masukkan UUID yang valid'),
  role: tenantRoleSchema,
})
export type CreateTenantDto = z.infer<typeof createTenantSchema>
export type AssignMemberDto = z.infer<typeof assignMemberSchema>
export type TenantRole = z.infer<typeof tenantRoleSchema>
