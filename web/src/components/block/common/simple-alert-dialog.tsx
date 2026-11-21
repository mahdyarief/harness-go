import { IconLoader2 } from '@tabler/icons-react'
import { Direction } from 'radix-ui'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

interface SimpleAlertDialogProps {
  title: string
  description?: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onCancel?: () => void
  onConfirm?: () => void
  cancelText?: string
  confirmText?: string
  variant?: 'primary' | 'destructive'
  /** Shows a spinner on the confirm button and blocks the dialog while a delete is in flight. */
  loading?: boolean
}

export default function SimpleAlertDialog({
  title,
  description,
  open,
  onOpenChange,
  onConfirm,
  onCancel,
  cancelText = 'Cancel',
  confirmText = 'Proceed',
  variant = 'destructive',
  loading = false,
}: SimpleAlertDialogProps) {
  const direction = Direction.useDirection()

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !loading) onOpenChange(next)
      }}
    >
      <AlertDialogContent dir={direction}>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description && <AlertDialogDescription>{description}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={loading} onClick={onCancel}>
            {cancelText}
          </AlertDialogCancel>
          <Button
            type="button"
            variant={variant}
            className={variant === 'destructive' ? 'text-on-accent' : ''}
            disabled={loading}
            onClick={onConfirm}
          >
            {loading && <IconLoader2 className="size-4 animate-spin" />}
            {loading ? `${confirmText}…` : confirmText}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
