'use client'

import { cva, type VariantProps } from 'class-variance-authority'
import { Avatar as AvatarPrimitive } from 'radix-ui'
import * as React from 'react'

import { cn } from '@/lib/utils'

const avatarStatusVariants = cva(
  'flex items-center rounded-full size-2 border-2 border-surface',
  {
    variants: {
      variant: {
        online: 'bg-green',
        offline: 'bg-ink-3',
        busy: 'bg-red',
        away: 'bg-ink-2',
      },
    },
    defaultVariants: {
      variant: 'online',
    },
  },
)

function Avatar({ className, ...props }: React.ComponentProps<typeof AvatarPrimitive.Root>) {
  return (
    <AvatarPrimitive.Root
      data-slot="avatar"
      className={cn('relative flex size-10 shrink-0', className)}
      {...props}
    />
  )
}

function AvatarImage({ className, ...props }: React.ComponentProps<typeof AvatarPrimitive.Image>) {
  return (
    <div className={cn('relative overflow-hidden rounded-full', className)}>
      <AvatarPrimitive.Image
        data-slot="avatar-image"
        className={cn('aspect-square h-full w-full')}
        {...props}
      />
    </div>
  )
}

function AvatarFallback({
  className,
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Fallback>) {
  return (
    <AvatarPrimitive.Fallback
      data-slot="avatar-fallback"
      className={cn(
        'border-line bg-hover-2 text-ink flex h-full w-full items-center justify-center rounded-full border text-xs',
        className,
      )}
      {...props}
    />
  )
}

function AvatarIndicator({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-slot="avatar-indicator"
      className={cn('absolute flex size-6 items-center justify-center', className)}
      {...props}
    />
  )
}

function AvatarStatus({
  className,
  variant,
  ...props
}: React.HTMLAttributes<HTMLDivElement> & VariantProps<typeof avatarStatusVariants>) {
  return (
    <div
      data-slot="avatar-status"
      className={cn(avatarStatusVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Avatar, AvatarFallback, AvatarImage, AvatarIndicator, AvatarStatus, avatarStatusVariants }
