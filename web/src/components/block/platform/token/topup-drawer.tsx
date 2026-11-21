import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { tokenQueries } from '@/lib/api/queries/token'
import { topUpSchema, type TopUpDto } from '@/lib/api/dtos/token/schema'
import { parseDto } from '@/lib/api/dtos/parse'
import { toastAxiosError } from '@/lib/api/axios-error'
import { useAppForm } from '@/hooks/form'

type TopUpDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Tenant the tokens are granted to; taken from the row, never typed. */
  tenantId: string
  tenantName: string
}

export function TopUpDrawer({ open, onOpenChange, tenantId, tenantName }: TopUpDrawerProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" size="md" className="p-0">
        {open && (
          <TopUpForm tenantId={tenantId} tenantName={tenantName} onOpenChange={onOpenChange} />
        )}
      </SheetContent>
    </Sheet>
  )
}

function TopUpForm({ tenantId, tenantName, onOpenChange }: Omit<TopUpDrawerProps, 'open'>) {
  const mutation = useMutation(tokenQueries.topUp())

  const form = useAppForm({
    defaultValues: { amount: '', note: '' },
    validators: {
      onSubmit: topUpSchema,
      onChange: topUpSchema,
    },
    onSubmit: async ({ value }) => {
      try {
        const parsed = parseDto(topUpSchema, value)
        const input: TopUpDto = {
          amount: Number(parsed.amount),
          note: parsed.note || undefined,
        }
        await mutation.mutateAsync({ tenantId, input })
        toast.success(`Token berhasil ditambahkan ke ${tenantName}`)
        onOpenChange(false)
        form.reset()
      } catch (error) {
        toastAxiosError(error)
      }
    },
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
      className="flex h-full flex-col"
    >
      <SheetHeader>
        <SheetTitle>Tambah token</SheetTitle>
        <SheetDescription>
          Tambahkan saldo token untuk tenant <strong>{tenantName}</strong>. Saldo berkurang otomatis
          saat pengguna memakai AI chat.
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-4 overflow-y-auto p-4">
        <form.AppField
          name="amount"
          children={(field) => (
            <field.NumberField
              asterisk
              label="Jumlah token"
              placeholder="Contoh: 1.000.000"
              min={1}
              allowNegative={false}
              decimalScale={0}
            />
          )}
        />
        <form.AppField
          name="note"
          children={(field) => (
            <field.TextField
              label="Catatan"
              placeholder="Opsional — alasan atau konteks penambahan"
              maxLength={500}
            />
          )}
        />
      </div>

      <SheetFooter>
        <form.AppForm>
          <form.SubmitButton label="Tambah token" />
        </form.AppForm>
      </SheetFooter>
    </form>
  )
}
