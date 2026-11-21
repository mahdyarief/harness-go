'use client'

import { IconDots, IconEdit, IconEye, IconTrash } from '@tabler/icons-react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

interface RowColumnActionProps {
  onShow?: () => void
  onEdit?: () => void
  onDelete?: () => void
  dropdown?: React.ReactNode
  actions?: React.ReactNode
}

export default function RowColumnAction({
  onShow,
  onEdit,
  onDelete,
  dropdown,
  actions,
}: RowColumnActionProps) {
  return (
    <div className="flex flex-row flex-wrap items-center gap-3">
      {onEdit && (
        <Button mode="link" underline="solid" className="text-ink" onClick={onEdit}>
          <IconEdit className="text-ink-2 size-4" />
          <span>Ubah</span>
        </Button>
      )}

      {actions}

      {(onShow || onDelete || dropdown) && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button mode="link" underline="solid" className="text-ink">
              <IconDots className="text-ink-2 size-4" />
              <span>Lainnya</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="min-w-40" align="end">
            <DropdownMenuGroup>
              {onShow && (
                <DropdownMenuItem onClick={onShow}>
                  <IconEye />
                  Lihat
                </DropdownMenuItem>
              )}

              {dropdown}

              {onDelete && (
                <DropdownMenuItem variant="destructive" onClick={onDelete}>
                  <IconTrash />
                  Hapus
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}
