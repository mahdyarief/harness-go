import * as React from 'react'

import { cn } from '@/lib/utils'

function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        'bg-surface text-ink placeholder:text-ink-3 focus-visible:ring-line-strong aria-invalid:ring-red/40 flex min-h-20 w-full rounded-control border-0 px-3 py-2 text-sm shadow-btn transition-[color,box-shadow] ease-[cubic-bezier(0.23,1,0.32,1)] focus-visible:ring-[3px] focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    />
  )
}

export { Textarea }
