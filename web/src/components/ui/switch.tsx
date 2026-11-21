import { Switch as SwitchPrimitive } from 'radix-ui'
import * as React from 'react'

import { cn } from '@/lib/utils'

function Switch({ className, ...props }: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      className={cn(
        'peer data-[state=checked]:bg-ink data-[state=unchecked]:bg-line-strong focus-visible:ring-line-strong inline-flex h-5 w-9 shrink-0 items-center rounded-full border-2 border-transparent shadow-hairline transition-all ease-[cubic-bezier(0.23,1,0.32,1)] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className={cn(
          'bg-surface pointer-events-none block size-4 rounded-full ring-0 shadow-btn transition-transform data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0',
        )}
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
