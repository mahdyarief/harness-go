import type { ButtonHTMLAttributes } from 'react'

import { IconDeviceFloppy, IconLoader2 } from '@tabler/icons-react'
import { useSelector } from '@tanstack/react-form'

import { Button } from '@/components/ui/button'
import { useFormContext } from '@/hooks/form-context'
import { cn } from '@/lib/utils'

interface SubmitButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string
  icon?: typeof IconDeviceFloppy
  className?: string
}

export default function SubmitButton({
  label,
  icon: Icon,
  className,
  ...props
}: SubmitButtonProps) {
  const form = useFormContext()
  const isSubmitting = useSelector(form.store, (state) => state.isSubmitting)

  return (
    <Button
      type="submit"
      className={cn(
        'bg-ink text-surface shadow-btn hover:bg-ink/90 h-9 rounded-control px-4 text-[13px] transition-colors',
        className,
      )}
      disabled={isSubmitting}
      {...props}
    >
      {isSubmitting ? <IconLoader2 className="size-4 animate-spin" /> : null}
      {Icon ? <Icon className="size-4" /> : null}
      {label}
    </Button>
  )
}
