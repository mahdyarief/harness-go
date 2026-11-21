import { cn } from '@/lib/utils'

export type IconBadgeTone = 'emerald'
export type IconBadgeVariant = 'soft' | 'filled'

const toneClasses: Record<IconBadgeVariant, string> = {
  soft: 'bg-green-tint text-green',
  filled: 'bg-green text-on-accent',
}

interface IconBadgeProps {
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>
  variant?: IconBadgeVariant
  className?: string
  iconClassName?: string
}

export default function IconBadge({
  icon: Icon,
  variant = 'soft',
  className,
  iconClassName,
}: IconBadgeProps) {
  return (
    <span
      className={cn(
        'flex h-9 w-9 shrink-0 items-center justify-center rounded-control',
        toneClasses[variant],
        className,
      )}
    >
      <Icon className={cn('h-4 w-4', iconClassName)} />
    </span>
  )
}
