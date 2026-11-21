import { useMutation } from '@tanstack/react-query'
import React, { useMemo, useState } from 'react'
import { toast } from 'sonner'

import type { Models } from '@/lib/api/models'
import type { AppColumnDef, BaseColumnProps } from '@/types/column'

import { toastAxiosError } from '@/lib/api/axios-error'
import { queries } from '@/lib/api/queries'

import CellItem from '../../common/cell-item'
import RowColumnAction from '../../common/row-column-action'
import SimpleAlertDialog from '../../common/simple-alert-dialog'
import { EditCategoryForm } from './form'

type ColumnType = AppColumnDef<Models.Category>

// Declared once, and never swapped for a skeleton: the cell owns the row's open
// dialogs, so remounting it on a background refetch would discard them.
const actionCell: ColumnType['cell'] = ({ row }) => <ActionCell record={row.original} />

export function CategoryColumn({ loading }: BaseColumnProps) {
  const columns = useMemo<ColumnType[]>(
    () => [
      {
        accessorKey: 'name',
        header: 'Nama',
        cell: (info) => {
          const value = info.getValue() as string
          return (
            <CellItem loading={loading}>
              <span>{value}</span>
            </CellItem>
          )
        },
      },
      {
        accessorKey: 'actions',
        header: 'Aksi',
        cell: actionCell,
        size: 160,
        minSize: 160,
      },
    ],
    [loading],
  )

  return columns
}

interface ActionCellProps {
  record: Models.Category
}

function ActionCell({ record }: ActionCellProps) {
  const [openEdit, setOpenEdit] = useState(false)
  const [openDelete, setOpenDelete] = useState(false)

  const deleteMutation = useMutation(queries.categories.delete())

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync(record.id)
      toast.success('Kategori berhasil dihapus')
      setOpenDelete(false)
    } catch (error) {
      toastAxiosError(error as Error)
    }
  }

  return (
    <React.Fragment>
      <RowColumnAction onEdit={() => setOpenEdit(true)} onDelete={() => setOpenDelete(true)} />

      <SimpleAlertDialog
        title="Hapus kategori ini?"
        description="Kategori akan dihapus permanen dan tidak dapat dikembalikan."
        open={openDelete}
        onOpenChange={setOpenDelete}
        onConfirm={handleDelete}
        confirmText="Hapus"
        variant="destructive"
        loading={deleteMutation.isPending}
      />

      <EditCategoryForm open={openEdit} onOpenChange={setOpenEdit} record={record} />
    </React.Fragment>
  )
}
