import { Tabs as TabsPrimitive } from 'radix-ui'

import { cn } from '@/lib/utils'

function Tabs({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn('flex flex-col gap-3', className)}
      {...props}
    />
  )
}

function TabsList({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        'bg-field text-ink-2 inline-flex h-11 w-fit max-w-full items-center gap-1 rounded-control p-1',
        className,
      )}
      {...props}
    />
  )
}

function TabsTrigger({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        'inline-flex h-full cursor-pointer items-center justify-center gap-1.5 rounded-chip px-3 text-sm font-medium whitespace-nowrap transition-colors ease-[cubic-bezier(0.23,1,0.32,1)]',
        'hover:text-ink focus-visible:ring-line-strong focus-visible:ring-2 focus-visible:outline-hidden',
        'data-[state=active]:bg-surface data-[state=active]:text-ink data-[state=active]:shadow-btn',
        'disabled:pointer-events-none disabled:opacity-60 [&_svg]:size-4 [&_svg]:shrink-0',
        className,
      )}
      {...props}
    />
  )
}

function TabsContent({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn(
        'focus-visible:ring-line-strong rounded-control focus-visible:ring-2 focus-visible:outline-hidden',
        className,
      )}
      {...props}
    />
  )
}

export { Tabs, TabsContent, TabsList, TabsTrigger }
