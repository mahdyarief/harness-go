import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useIsMutating, useMutation, useQuery } from '@tanstack/react-query'
import { useTable, type ReactTable } from '@tanstack/react-table'
import { features } from '@/components/block/common/react-table'
import { Button } from '@/components/ui/button'
import { DataGrid, DataGridContainer } from '@/components/ui/data-grid'
import { DataGridTable } from '@/components/ui/data-grid-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { parseDto } from '@/lib/api/dtos/parse'
import { toastAxiosError } from '@/lib/api/axios-error'
import { authQueries } from '@/lib/api/queries/auth'
import { tenantQueries } from '@/lib/api/queries/tenant'
import { userQueries } from '@/lib/api/queries/user'
import {
  assignMemberSchema,
  createTenantSchema,
  type TenantRole,
} from '@/lib/api/dtos/tenant/schema'
import type { TenantMembership } from '@/lib/api/models/tenant'
import type { AppColumnDef } from '@/types/column'

import TenantDirectory from './directory'

const roleLabel = (role: TenantRole) => (role === 'tenant_admin' ? 'Admin tenant' : 'Reviewer')

// The picker lists mirrored users by email, but the API still takes the UUID:
// the value here is user.id, the label is what the admin recognises.
const USER_OPTIONS_PARAMS = { offset: 0, limit: 100 }

function UserPicker({
  id,
  value,
  onValueChange,
  disabled,
  invalid,
  describedBy,
}: {
  id: string
  value: string
  onValueChange: (value: string) => void
  disabled?: boolean
  invalid?: boolean
  describedBy?: string
}) {
  const users = useQuery(userQueries.list(USER_OPTIONS_PARAMS))
  const options = users.data?.data ?? []
  const valueIsListed = options.some((user) => user.id === value)

  // The form may start with a default id that the mirror does not know about.
  // Leaving it would submit an invisible value, so clear it once the list has
  // loaded and the selection cannot be shown.
  useEffect(() => {
    if (users.isSuccess && value && !valueIsListed) onValueChange('')
  }, [users.isSuccess, value, valueIsListed, onValueChange])

  if (users.isError) {
    return (
      <div className="flex items-center gap-2">
        <p role="alert" className="text-ink text-[13px]">
          Daftar pengguna tidak dapat dimuat.
        </p>
        <Button variant="outline" size="sm" onClick={() => void users.refetch()}>
          Coba lagi
        </Button>
      </div>
    )
  }

  return (
    <Select value={value} onValueChange={onValueChange} disabled={disabled || users.isPending}>
      <SelectTrigger
        id={id}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        className="w-full"
      >
        <SelectValue placeholder={users.isPending ? 'Memuat pengguna…' : 'Pilih pengguna'} />
      </SelectTrigger>
      <SelectContent>
        {(users.data?.data ?? []).length === 0 ? (
          <div className="text-ink-2 px-2 py-1.5 text-[13px]">Belum ada pengguna.</div>
        ) : (
          (users.data?.data ?? []).map((user) => (
            <SelectItem key={user.id} value={user.id}>
              {user.name} · {user.email}
            </SelectItem>
          ))
        )}
      </SelectContent>
    </Select>
  )
}

function CreateTenant({ userId }: { userId: string }) {
  const [name, setName] = useState('')
  const [adminId, setAdminId] = useState(userId)
  const [message, setMessage] = useState('')
  const [isError, setIsError] = useState(false)
  const [invalidFields, setInvalidFields] = useState<string[]>([])
  const mutation = useMutation(tenantQueries.create())
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (mutation.isPending) return
    setMessage('')
    setIsError(false)
    setInvalidFields([])
    const result = createTenantSchema.safeParse({
      name,
      admin_user_id: adminId,
    })
    if (!result.success) {
      setIsError(true)
      const fields = result.error.issues.map((issue) => String(issue.path[0]))
      setInvalidFields(fields)
      setMessage(result.error.issues.map((issue) => issue.message).join('\n'))
      document.getElementById(fields[0] === 'name' ? 'tenant-name' : 'admin-id')?.focus()
      return
    }
    try {
      const tenant = await mutation.mutateAsync(
        parseDto(createTenantSchema, { name, admin_user_id: adminId }),
      )
      setName('')
      setMessage(
        `Tenant ${tenant.name} berhasil dibuat. Daftar hanya menampilkan keanggotaan Anda.`,
      )
    } catch (error) {
      setIsError(true)
      toastAxiosError(error)
      setMessage('Gagal membuat tenant. Periksa izin dan pilihan admin, lalu coba lagi.')
    }
  }
  return (
    <section className="bg-surface rounded-card shadow-card p-5 space-y-4">
      <div>
        <h2 className="text-ink font-semibold">Buat tenant</h2>
        <p className="text-[13px] text-ink-2">
          Pilih pengguna yang sudah ada sebagai admin awal tenant.
        </p>
      </div>
      <form onSubmit={submit} noValidate className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="tenant-name">Nama tenant</Label>
          <Input
            id="tenant-name"
            disabled={mutation.isPending}
            aria-invalid={invalidFields.includes('name')}
            aria-describedby={invalidFields.includes('name') ? 'tenant-message' : undefined}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={100}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="admin-id">Admin awal</Label>
          <UserPicker
            id="admin-id"
            value={adminId}
            onValueChange={setAdminId}
            disabled={mutation.isPending}
            invalid={invalidFields.includes('admin_user_id')}
            describedBy={invalidFields.includes('admin_user_id') ? 'tenant-message' : undefined}
          />
        </div>
        <div>
          <Button disabled={mutation.isPending} type="submit">
            {mutation.isPending ? 'Menyimpan…' : 'Buat tenant'}
          </Button>
        </div>
      </form>
      {message && (
        <p
          id="tenant-message"
          role={isError ? 'alert' : 'status'}
          className="whitespace-pre-line text-[13px] text-ink"
        >
          {message}
        </p>
      )}
    </section>
  )
}
type MemberRow = {
  id: string
  name: string
  email: string
  role: TenantRole
}

function MembersTable({ rows }: { rows: MemberRow[] }) {
  const columns = useMemo<AppColumnDef<MemberRow>[]>(
    () => [
      {
        id: 'user',
        header: 'Pengguna',
        cell: ({ row }) => (
          <>
            <span className="text-ink font-medium">{row.original.name}</span>
            <span className="text-ink-2 block text-xs">{row.original.email}</span>
          </>
        ),
      },
      {
        id: 'role',
        header: 'Peran',
        cell: ({ row }) => roleLabel(row.original.role),
      },
    ],
    [],
  )
  const table = useTable({
    features,
    columns,
    data: rows,
    // The members endpoint returns the whole list, so the table shows every row
    // and never renders a pagination control.
    state: { pagination: { pageIndex: 0, pageSize: Math.max(rows.length, 1) } },
    getRowId: (row) => row.id,
  })

  return (
    <DataGrid
      // DataGrid's prop type is pinned to ReactTable<any, T>, which the
      // feature-typed table TanStack v9 returns is not assignable to.
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      table={table as unknown as ReactTable<any, MemberRow>}
      recordCount={rows.length}
    >
      <DataGridContainer border={false}>
        <DataGridTable />
      </DataGridContainer>
    </DataGrid>
  )
}

function Members({ tenant, currentUserId }: { tenant: TenantMembership; currentUserId: string }) {
  const members = useQuery(tenantQueries.members(tenant.id))
  const memberRows = useMemo<MemberRow[]>(
    () =>
      (members.data ?? []).map((member) => ({
        id: member.user_id,
        name: member.name,
        email: member.email,
        role: member.role,
      })),
    [members.data],
  )
  const mutation = useMutation(tenantQueries.assignMember())
  const [userId, setUserId] = useState('')
  const [role, setRole] = useState<TenantRole>('reviewer')
  const [message, setMessage] = useState('')
  const [isError, setIsError] = useState(false)
  const [invalidFields, setInvalidFields] = useState<string[]>([])
  const editable = tenant.role === 'tenant_admin'
  // A tenant where the admin is the only member has nobody to promote or
  // demote, so the assignment form stays hidden until another member exists.
  const hasOtherMembers = (members.data ?? []).some((member) => member.user_id !== currentUserId)
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!editable) return
    if (mutation.isPending) return
    setMessage('')
    setIsError(false)
    setInvalidFields([])
    const result = assignMemberSchema.safeParse({ user_id: userId, role })
    if (!result.success) {
      setIsError(true)
      const fields = result.error.issues.map((issue) => String(issue.path[0]))
      setInvalidFields(fields)
      setMessage(result.error.issues.map((issue) => issue.message).join('\n'))
      document.getElementById(fields[0] === 'role' ? 'member-role' : 'member-id')?.focus()
      return
    }
    try {
      await mutation.mutateAsync({
        id: tenant.id,
        input: parseDto(assignMemberSchema, { user_id: userId, role }),
      })
      setUserId('')
      setRole('reviewer')
      setMessage('Keanggotaan berhasil disimpan.')
    } catch (error) {
      setIsError(true)
      toastAxiosError(error)
      setMessage('Gagal menyimpan anggota. Periksa izin dan pilihan pengguna, lalu coba lagi.')
    }
  }
  return (
    <section
      className="bg-surface rounded-card shadow-card p-5 space-y-4"
      aria-label={`Anggota ${tenant.name}`}
    >
      <div>
        <h2 className="text-ink font-semibold">Anggota {tenant.name}</h2>
        <p className="text-[13px] text-ink-2">
          Peran Anda: {roleLabel(tenant.role)}.{' '}
          {!editable && 'Hanya admin tenant yang dapat mengubah anggota.'}
        </p>
      </div>
      {members.isPending ? (
        <p role="status">Memuat anggota…</p>
      ) : members.isError ? (
        <div role="alert">
          <p>Anggota tidak dapat dimuat.</p>
          <Button variant="outline" onClick={() => void members.refetch()}>
            Coba lagi
          </Button>
        </div>
      ) : members.data.length === 0 ? (
        <p>Belum ada anggota.</p>
      ) : (
        <MembersTable rows={memberRows} />
      )}
      {editable && hasOtherMembers && (
        <form
          onSubmit={submit}
          noValidate
          className="grid gap-4 border-line border-t pt-4 sm:grid-cols-2"
        >
          <h3 className="text-ink font-medium sm:col-span-2">Tetapkan atau ubah peran anggota</h3>
          <div className="space-y-2">
            <Label htmlFor="member-id">Pengguna</Label>
            <Input
              id="member-id"
              disabled={mutation.isPending}
              aria-invalid={invalidFields.includes('user_id')}
              aria-describedby={invalidFields.includes('user_id') ? 'member-message' : undefined}
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              placeholder="UUID pengguna"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="member-role">Peran</Label>
            <Select
              disabled={mutation.isPending}
              value={role}
              onValueChange={(value) => setRole(value as TenantRole)}
            >
              <SelectTrigger
                id="member-role"
                aria-invalid={invalidFields.includes('role')}
                aria-describedby={invalidFields.includes('role') ? 'member-message' : undefined}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="reviewer">Reviewer</SelectItem>
                <SelectItem value="tenant_admin">Admin tenant</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Button disabled={mutation.isPending} type="submit">
              {mutation.isPending ? 'Menyimpan…' : 'Simpan anggota'}
            </Button>
          </div>
        </form>
      )}
      {message && (
        <p
          id="member-message"
          role={isError ? 'alert' : 'status'}
          className="whitespace-pre-line text-[13px] text-ink"
        >
          {message}
        </p>
      )}
    </section>
  )
}
function MyTenants({ currentUserId }: { currentUserId: string }) {
  const tenants = useQuery(tenantQueries.list())
  const [selectedId, setSelectedId] = useState('')
  const assigningMember = useIsMutating({ mutationKey: ['tenants', 'assign-member'] }) > 0
  const selected = tenants.data?.find((tenant) => tenant.id === selectedId)
  return (
    <>
      <section className="bg-surface rounded-card shadow-card p-5 space-y-4">
        <h2 className="text-ink font-semibold">Tenant saya</h2>
        {tenants.isPending ? (
          <p role="status">Memuat tenant…</p>
        ) : tenants.isError ? (
          <div role="alert">
            <p>Tenant tidak dapat dimuat.</p>
            <Button variant="outline" onClick={() => void tenants.refetch()}>
              Coba lagi
            </Button>
          </div>
        ) : tenants.data.length === 0 ? (
          <p>Anda belum menjadi anggota tenant.</p>
        ) : (
          <div className="space-y-2">
            <Label htmlFor="tenant-select">Pilih tenant untuk melihat anggota</Label>
            <Select value={selectedId} onValueChange={setSelectedId} disabled={assigningMember}>
              <SelectTrigger id="tenant-select" className="w-full">
                <SelectValue placeholder="Pilih tenant" />
              </SelectTrigger>
              <SelectContent>
                {tenants.data.map((tenant) => (
                  <SelectItem key={tenant.id} value={tenant.id}>
                    {tenant.name} · {roleLabel(tenant.role)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </section>
      {selected && <Members key={selected.id} tenant={selected} currentUserId={currentUserId} />}
    </>
  )
}

export default function TenantPage() {
  const profile = useQuery(authQueries.profile())
  const isSuperUser = profile.data?.data.role === 'super_user'
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-ink text-2xl font-semibold tracking-tight">Tenant</h1>
        <p className="text-[13px] text-ink-2">
          {isSuperUser
            ? 'Setiap tenant di platform beserta pengguna yang menjadi anggotanya.'
            : 'Lihat tenant dan anggota sesuai keanggotaan Anda.'}
        </p>
      </header>
      {profile.isPending ? (
        <p role="status">Memuat profil…</p>
      ) : profile.isError ? (
        <div role="alert">
          Profil tidak dapat dimuat.{' '}
          <Button variant="outline" onClick={() => void profile.refetch()}>
            Coba lagi
          </Button>
        </div>
      ) : isSuperUser ? (
        <>
          <CreateTenant userId={profile.data.data.id} />
          <TenantDirectory />
        </>
      ) : (
        <MyTenants currentUserId={profile.data.data.id} />
      )}
    </div>
  )
}
