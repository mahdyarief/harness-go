import { useTheme } from 'next-themes'

import { AnimatedThemeToggler } from '@/components/ui/animated-theme-toggler'
import { buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/** Light / dark switcher for the app header; next-themes owns persistence. */
export default function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme()

  return (
    <AnimatedThemeToggler
      theme={resolvedTheme === 'dark' ? 'dark' : 'light'}
      onThemeChange={setTheme}
      duration={500}
      aria-label="Ubah tema"
      className={cn(buttonVariants({ variant: 'ghost', size: 'icon' }), '[&_svg]:size-4')}
    />
  )
}
