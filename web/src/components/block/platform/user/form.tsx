import { useMutation, useQuery } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import type { Models } from '@/lib/api/models'
import { queries } from '@/lib/api/queries'
import {
  createUserSchema,
  updateUserSchema,
  type CreateUserDto,
  type UpdateUserDto,
} from '@/lib/api/dtos/user/schema'
import { parseDto } from '@/lib/api/dtos/parse'
import { toastAxiosError } from '@/lib/api/axios-error'
import { useAppForm } from '@/hooks/form'

import type { Option } from '@/types/select'

import SelectField from '../../form/select-field'

const PLATFORM_ROLE_OPTIONS: Option<unknown>[] = [
  { label: 'Pengguna', value: 'user' },
  { label: 'Super user', value: 'super_user' },
]

const TENANT_ROLE_OPTIONS: Option<unknown>[] = [
  { label: 'Admin tenant', value: 'tenant_admin' },
  { label: 'Reviewer', value: 'reviewer' },
]

type UserFormProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  isEdit: boolean
  userId?: string
  defaultValues: CreateUserDto | UpdateUserDto
}

function UserForm({ open, onOpenChange, isEdit, userId, defaultValues }: UserFormProps) {
  const mutation = useMutation(
    isEdit ? queries.users.update(userId!) : queries.users.create(),
  )

  const form = useAppForm({
    defaultValues,
    validators: {
      onSubmit: isEdit ? updateUserSchema : createUserSchema,
      onChange: isEdit ? updateUserSchema : createUserSchema,
    },
    onSubmit: async ({ value }) => {
      try {
        await mutation.mutateAsync(parseDto(isEdit ? updateUserSchema : createUserSchema, value))
        toast.success(`Pengguna berhasil ${isEdit ? 'diperbarui' : 'dibuat'}`)
        onOpenChange(false)
        form.reset()
      } catch (error) {
        toastAxiosError(error)
      }
    },
  })

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" size="md" className="p-0">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void form.handleSubmit()
          }}
          className="flex h-full flex-col"
        >
          <SheetHeader>
            <SheetTitle>{isEdit ? 'Ubah' : 'Buat'} pengguna</SheetTitle>
            <SheetDescription>
              {isEdit
                ? 'Perbarui profil, peran platform, dan keanggotaan tenant.'
                : 'Buat akun baru. Kata sandi diatur di sini dan email ditandai sudah terverifikasi.'}
            </SheetDescription>
          </SheetHeader>

          <div className="flex-1 space-y-4 overflow-y-auto p-4">
            <form.AppField
              name="name"
              children={(field) => (
                <field.TextField asterisk label="Nama" placeholder="mis. Budi Santoso" />
              )}
            />
            <form.AppField
              name="email"
              children={(field) => (
                <field.TextField
                  asterisk
                  label="Email"
                  placeholder="name@company.com"
                  disabled={isEdit}
                />
              )}
            />
            <form.AppField
              name="password"
              children={(field) => (
                <field.PasswordField
                  asterisk={!isEdit}
                  label={isEdit ? 'Kata sandi (kosongkan untuk mempertahankan)' : 'Kata sandi'}
                  placeholder="Minimal 12 karakter"
                />
              )}
            />
            <form.AppField
              name="role"
              children={(field) => (
                <field.SelectField
                  asterisk
                  label="Peran platform"
                  placeholder="Pilih peran"
                  options={PLATFORM_ROLE_OPTIONS}
                />
              )}
            />
            <form.AppField name="tenant_id" children={() => <TenantPickerField />} />
            <form.Subscribe selector={(state) => state.values.tenant_id}>
              {(tenantId) => (
                <form.AppField
                  name="tenant_role"
                  children={(field) => (
                    <field.SelectField
                      label="Peran tenant"
                      placeholder="Pilih peran tenant"
                      options={TENANT_ROLE_OPTIONS}
                      disabled={!tenantId}
                    />
                  )}
                />
              )}
            </form.Subscribe>
          </div>

          <SheetFooter>
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting] as const}
            >
              {([canSubmit, isSubmitting]) => (
                <Button type="submit" disabled={!canSubmit || isSubmitting}>
                  {isEdit ? 'Simpan perubahan' : 'Buat pengguna'}
                </Button>
              )}
            </form.Subscribe>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  )
}

// Every tenant on the platform, not just the caller's memberships, so the
// platform admin can place the account anywhere. The role select stays disabled
// until a tenant is chosen, which keeps the pair from being half-filled.
function TenantPickerField() {
  const { data: tenants, isPending } = useQuery(queries.tenants.platformList())

  const options: Option<unknown>[] = (tenants ?? []).map((tenant) => ({
    label: tenant.name,
    value: tenant.id,
  }))

  return (
    <SelectField
      label="Tenant"
      placeholder="Pilih tenant"
      options={options}
      loading={isPending}
    />
  )
}

type CreateUserDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CreateUserDrawer({ open, onOpenChange }: CreateUserDrawerProps) {
  // Mounted only while open so an abandoned draft never leaks into the next
  // create: useAppForm keeps its state for as long as it stays mounted.
  if (!open) return null

  return (
    <UserForm
      open
      onOpenChange={onOpenChange}
      isEdit={false}
      defaultValues={{
        name: '',
        email: '',
        password: '',
        role: 'user',
        tenant_id: '',
        tenant_role: '',
      }}
    />
  )
}

type EditUserDrawerProps = CreateUserDrawerProps & {
  record: Models.User
}

export function EditUserDrawer({ open, onOpenChange, record }: EditUserDrawerProps) {
  // Only fetched while the drawer is open: one query per row would otherwise run
  // for every user in the table.
  const { data: membership, isPending } = useQuery({
    ...queries.users.membership(record.id),
    enabled: open,
  })

  if (!open) return null

  // useAppForm reads defaultValues once, on mount. Waiting for the membership
  // keeps the tenant fields from being seeded empty and then never updating.
  if (isPending) {
    return (
      <Sheet open onOpenChange={onOpenChange}>
        <SheetContent side="right" size="md" className="p-0">
          <SheetHeader>
            <SheetTitle>Ubah pengguna</SheetTitle>
            <SheetDescription>Memuat keanggotaan tenant akun…</SheetDescription>
          </SheetHeader>
          <div className="space-y-4 p-4">
            {Array.from({ length: 6 }, (_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <UserForm
      open
      onOpenChange={onOpenChange}
      isEdit
      userId={record.id}
      defaultValues={{
        name: record.name,
        email: record.email,
        password: '',
        role: (record.role as UpdateUserDto['role']) ?? 'user',
        tenant_id: membership?.tenant_id ?? '',
        tenant_role: (membership?.role as UpdateUserDto['tenant_role']) ?? '',
      }}
    />
  )
}
