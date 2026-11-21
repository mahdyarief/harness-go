import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardHeading,
  CardTitle,
  CardToolbar,
} from '@/components/ui/card'
import { cn } from '@/lib/utils'

interface SectionCardProps {
  title: string
  description?: string
  toolbar?: React.ReactNode
  /** Drop the content padding for children that own their own inset, such as a data grid. */
  flush?: boolean
  children: React.ReactNode
}

export default function SectionCard({
  title,
  description,
  toolbar,
  flush = false,
  children,
}: SectionCardProps) {
  return (
    <Card className="bg-surface shadow-card rounded-card w-full min-w-0 overflow-hidden">
      <CardHeader
        className={cn(
          'flex flex-col items-stretch gap-3 py-4 lg:flex-row lg:items-center',
          description ? 'min-h-22' : 'min-h-15',
        )}
      >
        <CardHeading className="min-w-0 flex-1">
          <CardTitle className="text-ink text-base leading-6 font-semibold tracking-tight">
            {title}
          </CardTitle>
          <CardDescription className={cn('text-ink-2 text-sm', !description && 'sr-only')}>
            {description}
          </CardDescription>
        </CardHeading>

        {toolbar && <CardToolbar className="w-full shrink-0 lg:w-auto">{toolbar}</CardToolbar>}
      </CardHeader>
      <CardContent className={cn(flush ? 'p-0' : 'p-5')}>{children}</CardContent>
    </Card>
  )
}
