import { IconBox } from '@tabler/icons-react'
import { useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldGroup } from '@/components/ui/field'
import { useAppForm } from '@/hooks/form'
import { SignInSchema } from '@/lib/api/dtos/auth/schema'
import { APP_NAME } from '@/lib/app-name'
import { signInWithEmail } from '@/lib/auth/email-auth'
import { cn } from '@/lib/utils'

export default function LoginForm({ className, ...props }: React.ComponentProps<'div'>) {
  const router = useRouter()
  const [isLoading, setIsLoading] = useState(false)

  const form = useAppForm({
    defaultValues: {
      email: '',
      password: '',
    },
    validators: {
      onSubmit: SignInSchema,
      onChange: SignInSchema,
    },
    onSubmit: async ({ value }) => {
      setIsLoading(true)

      try {
        await signInWithEmail(value)
        await router.invalidate()
        await router.navigate({ to: '/dashboard' })
      } catch (error) {
        toast.error(error instanceof Error ? error.message : 'Terjadi kesalahan')
      } finally {
        setIsLoading(false)
      }
    },
  })

  return (
    <div className={cn('flex flex-col gap-6', className)} {...props}>
      <FieldGroup>
        <div className="flex flex-col items-center gap-2 text-center">
          <div className="bg-ink text-surface flex size-10 items-center justify-center rounded-control">
            <IconBox className="size-6" />
          </div>
          <h1 className="text-ink text-xl font-semibold tracking-tight">Masuk ke {APP_NAME}</h1>
          <FieldDescription>Gunakan akun Anda untuk melanjutkan ke ruang kerja.</FieldDescription>
        </div>

        <form
          className="flex flex-col gap-6"
          onSubmit={(e) => {
            e.preventDefault()
            form.handleSubmit()
          }}
        >
          <form.AppField
            name="email"
            children={(field) => (
              <field.TextField label="Email" placeholder="Masukkan email Anda" asterisk />
            )}
          />

          <form.AppField
            name="password"
            children={(field) => (
              <field.PasswordField label="Kata sandi" placeholder="Masukkan kata sandi" asterisk />
            )}
          />

          <Field>
            <Button type="submit" disabled={isLoading}>
              {isLoading ? 'Memproses…' : 'Masuk'}
            </Button>
          </Field>
        </form>
      </FieldGroup>
    </div>
  )
}
