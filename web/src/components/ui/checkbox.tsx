import { cva, type VariantProps } from 'class-variance-authority'
import { Check, Minus } from 'lucide-react'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'
import * as React from 'react'

import { cn } from '@/lib/utils'

// Define the variants for the Checkbox using cva.
const checkboxVariants = cva(
  `
    group peer bg-surface shrink-0 rounded-chip border border-line-strong ring-offset-surface focus-visible:outline-none 
    focus-visible:ring-2 focus-visible:ring-line-strong focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 
    aria-invalid:border-destructive/60 aria-invalid:ring-destructive/10 dark:aria-invalid:border-destructive dark:aria-invalid:ring-destructive/20
    [[data-invalid=true]_&]:border-destructive/60 [[data-invalid=true]_&]:ring-destructive/10  dark:[[data-invalid=true]_&]:border-destructive dark:[[data-invalid=true]_&]:ring-destructive/20,
    data-[state=checked]:bg-ink data-[state=checked]:border-ink data-[state=checked]:text-surface data-[state=indeterminate]:bg-ink data-[state=indeterminate]:border-ink data-[state=indeterminate]:text-surface
    `,
  {
    variants: {
      size: {
        sm: 'size-4.5 [&_svg]:size-3',
        md: 'size-5 [&_svg]:size-3.5',
        lg: 'size-5.5 [&_svg]:size-4',
      },
    },
    defaultVariants: {
      size: 'md',
    },
  },
)

function Checkbox({
  className,
  size,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root> & VariantProps<typeof checkboxVariants>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(checkboxVariants({ size }), className)}
      {...props}
    >
      <CheckboxPrimitive.Indicator className={cn('flex items-center justify-center text-current')}>
        <Check className="group-data-[state=indeterminate]:hidden" />
        <Minus className="hidden group-data-[state=indeterminate]:block" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
