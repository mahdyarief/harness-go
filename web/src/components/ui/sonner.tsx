import { useTheme } from 'next-themes'
import * as React from 'react'
import { Toaster as Sonner } from 'sonner'

type ToasterProps = React.ComponentProps<typeof Sonner>

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = 'system' } = useTheme()

  return (
    <Sonner
      theme={theme as ToasterProps['theme']}
      className="group toaster [&_[data-type=success]>[data-icon]]:text-green [&_[data-type=success]_[data-title]]:text-green [&_[data-type=info]_[data-title]]:text-info [&_[data-type=error]>[data-icon]]:text-red [&_[data-type=error]_[data-title]]:text-red"
      toastOptions={{
        classNames: {
          toast:
            'group toast group-[.toaster]:bg-surface group-[.toaster]:text-ink! group-[.toaster]:border-line group-[.toaster]:rounded-control group-[.toaster]:shadow-overlay has-[[role=alert]]:border-0! has-[[role=alert]]:shadow-none! has-[[role=alert]]:bg-transparent!',
          description: 'group-[.toast]:text-ink-2',
          actionButton:
            'group-[.toast]:rounded-chip! group-[.toast]:bg-ink group-[.toast]:text-surface',
          cancelButton:
            'group-[.toast]:rounded-chip! group-[.toast]:bg-field group-[.toast]:text-ink!',
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
