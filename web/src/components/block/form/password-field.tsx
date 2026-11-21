import { IconEye, IconEyeOff } from '@tabler/icons-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input, InputWrapper } from '@/components/ui/input'
import { useFieldContext } from '@/hooks/form-context'

interface PasswordFieldProps {
  label?: string
  placeholder?: string
  asterisk?: boolean
}

export default function PasswordField({ label, placeholder, asterisk }: PasswordFieldProps) {
  const field = useFieldContext<string>()
  const [visible, setVisible] = useState(false)
  const errors = field.state.meta.errors
  const isInvalid = !!errors?.length

  return (
    <Field data-invalid={isInvalid}>
      <FieldLabel htmlFor={field.name} className="gap-1">
        {label}
        {asterisk && <span className="text-red">*</span>}
      </FieldLabel>
      <InputWrapper className="h-9">
        <Input
          id={field.name}
          name={field.name}
          type={visible ? 'text' : 'password'}
          value={field.state.value}
          onBlur={field.handleBlur}
          onChange={(e) => field.handleChange(e.target.value)}
          aria-invalid={isInvalid}
          placeholder={placeholder}
          variant="lg"
        />
        <Button
          type="button"
          variant="ghost"
          size="sm"
          mode="icon"
          className="text-ink-2 hover:text-ink"
          onClick={() => setVisible((v) => !v)}
          aria-label={visible ? 'Sembunyikan kata sandi' : 'Tampilkan kata sandi'}
        >
          {visible ? <IconEyeOff className="size-4" /> : <IconEye className="size-4" />}
        </Button>
      </InputWrapper>
      {isInvalid && <FieldError errors={errors} />}
    </Field>
  )
}
