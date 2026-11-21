import { cva, type VariantProps } from 'class-variance-authority'
import { ChevronDown, type LucideIcon } from 'lucide-react'
import { Slot as SlotPrimitive } from 'radix-ui'
import * as React from 'react'

import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'cursor-pointer group whitespace-nowrap focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-surface inline-flex items-center justify-center has-data-[arrow=true]:justify-between text-sm font-medium transition-[background-color,color,box-shadow,transform] duration-150 enabled:active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50 [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        primary:
          'bg-ink text-surface shadow-btn hover:bg-ink/90 data-[state=open]:bg-ink/90',
        mono: 'bg-ink text-surface hover:bg-ink/90 data-[state=open]:bg-ink/90',
        destructive:
          'bg-red text-on-accent hover:bg-red/90 data-[state=open]:bg-red/90',
        secondary:
          'bg-secondary text-secondary-foreground hover:bg-secondary/90 data-[state=open]:bg-secondary/90',
        outline:
          'bg-surface text-ink shadow-btn hover:bg-hover data-[state=open]:bg-hover',
        dashed:
          'text-ink border border-line-strong border-dashed bg-surface hover:bg-hover data-[state=open]:text-ink',
        ghost:
          'text-ink hover:bg-hover-2 data-[state=open]:bg-hover-2',
        dim: 'text-ink-2 hover:text-ink data-[state=open]:text-ink',
        foreground: '',
        inverse: '',
      },
      appearance: {
        default: '',
        ghost: '',
      },
      underline: {
        solid: '',
        dashed: '',
      },
      underlined: {
        solid: '',
        dashed: '',
      },
      size: {
        lg: 'h-10 px-4 text-sm gap-1.5 [&_svg:not([class*=size-])]:size-4',
        md: 'h-9 px-3 gap-1.5 text-sm [&_svg:not([class*=size-])]:size-4',
        sm: 'h-8 px-2.5 gap-1.25 text-xs [&_svg:not([class*=size-])]:size-3.5',
        xs: 'h-7 px-2 gap-1 text-xs [&_svg:not([class*=size-])]:size-3.5',
        icon: 'size-9 [&_svg:not([class*=size-])]:size-4 shrink-0',
      },
      autoHeight: {
        true: '',
        false: '',
      },
      radius: {
        md: 'rounded-control',
        full: 'rounded-full',
      },
      mode: {
        default: 'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
        icon: 'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 shrink-0',
        link: 'text-primary h-auto p-0 bg-transparent rounded-none hover:bg-transparent data-[state=open]:bg-transparent',
        input: `
            justify-start font-normal hover:bg-surface [&_svg]:transition-colors [&_svg]:hover:text-ink data-[state=open]:bg-surface 
            focus-visible:border-ring focus-visible:outline-hidden focus-visible:ring-[3px] focus-visible:ring-ring/30 
            [[data-state=open]>&]:border-ring [[data-state=open]>&]:outline-hidden [[data-state=open]>&]:ring-[3px] 
            [[data-state=open]>&]:ring-ring/30 
            aria-invalid:border-destructive/60 aria-invalid:ring-destructive/10 dark:aria-invalid:border-destructive dark:aria-invalid:ring-destructive/20
            in-data-[invalid=true]:border-destructive/60 in-data-[invalid=true]:ring-destructive/10  dark:in-data-[invalid=true]:border-destructive dark:in-data-[invalid=true]:ring-destructive/20
          `,
      },
      placeholder: {
        true: 'text-ink-2',
        false: '',
      },
    },
    compoundVariants: [
      // Icons opacity for default mode
      {
        variant: 'ghost',
        mode: 'default',
        className: '[&_svg:not([role=img]):not([class*=text-]):not([class*=opacity-])]:opacity-60',
      },
      {
        variant: 'outline',
        mode: 'default',
        className: '[&_svg:not([role=img]):not([class*=text-]):not([class*=opacity-])]:opacity-60',
      },
      {
        variant: 'dashed',
        mode: 'default',
        className: '[&_svg:not([role=img]):not([class*=text-]):not([class*=opacity-])]:opacity-60',
      },
      {
        variant: 'secondary',
        mode: 'default',
        className: '[&_svg:not([role=img]):not([class*=text-]):not([class*=opacity-])]:opacity-60',
      },

      // Icons opacity for default mode
      {
        variant: 'outline',
        mode: 'input',
        className: '[&_svg:not([role=img]):not([class*=text-]):not([class*=opacity-])]:opacity-60',
      },
      {
        variant: 'outline',
        mode: 'icon',
        className: '[&_svg:not([role=img]):not([class*=text-]):not([class*=opacity-])]:opacity-60',
      },

      // Auto height
      {
        size: 'xs',
        autoHeight: true,
        className: 'h-auto min-h-7',
      },
      {
        size: 'md',
        autoHeight: true,
        className: 'h-auto min-h-9',
      },
      {
        size: 'sm',
        autoHeight: true,
        className: 'h-auto min-h-8',
      },
      {
        size: 'lg',
        autoHeight: true,
        className: 'h-auto min-h-10',
      },

      // Shadow support
      {
        variant: 'primary',
        mode: 'default',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'mono',
        mode: 'default',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'secondary',
        mode: 'default',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'outline',
        mode: 'default',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'dashed',
        mode: 'default',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'destructive',
        mode: 'default',
        appearance: 'default',
        className: '',
      },

      // Shadow support
      {
        variant: 'primary',
        mode: 'icon',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'mono',
        mode: 'icon',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'secondary',
        mode: 'icon',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'outline',
        mode: 'icon',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'dashed',
        mode: 'icon',
        appearance: 'default',
        className: '',
      },
      {
        variant: 'destructive',
        mode: 'icon',
        appearance: 'default',
        className: '',
      },

      // Link
      {
        variant: 'primary',
        mode: 'link',
        underline: 'solid',
        className:
          'font-medium text-primary hover:text-primary/90 [&_svg:not([role=img]):not([class*=text-])]:opacity-60 hover:underline hover:underline-offset-4 hover:decoration-solid',
      },
      {
        variant: 'primary',
        mode: 'link',
        underline: 'dashed',
        className:
          'font-medium text-primary hover:text-primary/90 [&_svg:not([role=img]):not([class*=text-])]:opacity-60 hover:underline hover:underline-offset-4 hover:decoration-dashed decoration-1',
      },
      {
        variant: 'primary',
        mode: 'link',
        underlined: 'solid',
        className:
          'font-medium text-primary hover:text-primary/90 [&_svg:not([role=img]):not([class*=text-])]:opacity-60 underline underline-offset-4 decoration-solid',
      },
      {
        variant: 'primary',
        mode: 'link',
        underlined: 'dashed',
        className:
          'font-medium text-primary hover:text-primary/90 [&_svg]:opacity-60 underline underline-offset-4 decoration-dashed decoration-1',
      },

      {
        variant: 'inverse',
        mode: 'link',
        underline: 'solid',
        className:
          'font-medium text-inherit [&_svg:not([role=img]):not([class*=text-])]:opacity-60 hover:underline hover:underline-offset-4 hover:decoration-solid',
      },
      {
        variant: 'inverse',
        mode: 'link',
        underline: 'dashed',
        className:
          'font-medium text-inherit [&_svg:not([role=img]):not([class*=text-])]:opacity-60 hover:underline hover:underline-offset-4 hover:decoration-dashed decoration-1',
      },
      {
        variant: 'inverse',
        mode: 'link',
        underlined: 'solid',
        className:
          'font-medium text-inherit [&_svg:not([role=img]):not([class*=text-])]:opacity-60 underline underline-offset-4 decoration-solid',
      },
      {
        variant: 'inverse',
        mode: 'link',
        underlined: 'dashed',
        className:
          'font-medium text-inherit [&_svg:not([role=img]):not([class*=text-])]:opacity-60 underline underline-offset-4 decoration-dashed decoration-1',
      },

      {
        variant: 'foreground',
        mode: 'link',
        underline: 'solid',
        className:
          'font-medium text-ink [&_svg:not([role=img]):not([class*=text-])]:opacity-60 hover:underline hover:underline-offset-4 hover:decoration-solid',
      },
      {
        variant: 'foreground',
        mode: 'link',
        underline: 'dashed',
        className:
          'font-medium text-ink [&_svg:not([role=img]):not([class*=text-])]:opacity-60 hover:underline hover:underline-offset-4 hover:decoration-dashed decoration-1',
      },
      {
        variant: 'foreground',
        mode: 'link',
        underlined: 'solid',
        className:
          'font-medium text-ink [&_svg:not([role=img]):not([class*=text-])]:opacity-60 underline underline-offset-4 decoration-solid',
      },
      {
        variant: 'foreground',
        mode: 'link',
        underlined: 'dashed',
        className:
          'font-medium text-ink [&_svg:not([role=img]):not([class*=text-])]:opacity-60 underline underline-offset-4 decoration-dashed decoration-1',
      },

      // Ghost
      {
        variant: 'primary',
        appearance: 'ghost',
        className:
          'bg-transparent text-primary/90 hover:bg-primary/5 data-[state=open]:bg-primary/5',
      },
      {
        variant: 'destructive',
        appearance: 'ghost',
        className:
          'bg-transparent text-red hover:bg-red-tint data-[state=open]:bg-red-tint',
      },
      {
        variant: 'ghost',
        mode: 'icon',
        className: 'text-ink-2',
      },

      // Size
      {
        size: 'xs',
        mode: 'icon',
        className: 'w-7 h-7 p-0 [[&_svg:not([class*=size-])]:size-3.5',
      },
      {
        size: 'sm',
        mode: 'icon',
        className: 'w-8 h-8 p-0 [[&_svg:not([class*=size-])]:size-3.5',
      },
      {
        size: 'md',
        mode: 'icon',
        className: 'w-9 h-9 p-0 [&_svg:not([class*=size-])]:size-4',
      },
      {
        size: 'icon',
        className: 'w-9 h-9 p-0 [&_svg:not([class*=size-])]:size-4',
      },
      {
        size: 'lg',
        mode: 'icon',
        className: 'w-10 h-10 p-0 [&_svg:not([class*=size-])]:size-4',
      },

      // Input mode
      {
        mode: 'input',
        placeholder: true,
        variant: 'outline',
        className: 'font-normal text-ink-2',
      },
      {
        mode: 'input',
        variant: 'outline',
        size: 'sm',
        className: 'gap-1.25',
      },
      {
        mode: 'input',
        variant: 'outline',
        size: 'md',
        className: 'gap-1.5',
      },
      {
        mode: 'input',
        variant: 'outline',
        size: 'lg',
        className: 'gap-1.5',
      },
    ],
    defaultVariants: {
      variant: 'primary',
      mode: 'default',
      size: 'md',
      radius: 'md',
      appearance: 'default',
    },
  },
)

function Button({
  className,
  selected,
  variant,
  radius,
  appearance,
  mode,
  size,
  autoHeight,
  underlined,
  underline,
  asChild = false,
  placeholder = false,
  ...props
}: React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    selected?: boolean
    asChild?: boolean
  }) {
  const Comp = asChild ? SlotPrimitive.Slot : 'button'
  return (
    <Comp
      data-slot="button"
      className={cn(
        buttonVariants({
          variant,
          size,
          radius,
          appearance,
          mode,
          autoHeight,
          placeholder,
          underlined,
          underline,
          className,
        }),
        asChild && props.disabled && 'pointer-events-none opacity-50',
      )}
      {...(selected && { 'data-state': 'open' })}
      {...props}
    />
  )
}

interface ButtonArrowProps extends React.SVGProps<SVGSVGElement> {
  icon?: LucideIcon // Allows passing any Lucide icon
}

function ButtonArrow({ icon: Icon = ChevronDown, className, ...props }: ButtonArrowProps) {
  return <Icon data-slot="button-arrow" className={cn('ms-auto -me-1', className)} {...props} />
}

export { Button, ButtonArrow, buttonVariants }
