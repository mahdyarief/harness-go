import { useMemo, useState } from 'react'

import type { Models } from '@/lib/api/models'
import type { AppColumnDef, BaseColumnProps } from '@/types/column'

import { formatDateTime } from '@/lib/date'

import CellItem from '../../common/cell-item'
import RowColumnAction from '../../common/row-column-action'
import { EditUserDrawer } from './form'

type ColumnType = AppColumnDef<Models.User>

// Declared once, and never swapped for a skeleton: the cell owns the row's open
// drawer, so remounting it on a background refetch would discard it.
const actionCell: ColumnType['cell'] = ({ row }) => <ActionCell record={row.original} />

export function UserColumn({ loading }: BaseColumnProps) {
  const columns = useMemo<ColumnType[]>(
    () => [
      {
        accessorKey: 'name',
        header: 'Nama',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="font-medium">{info.getValue() as string}</span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'email',
        header: 'Email',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="font-mono text-sm">{info.getValue() as string}</span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'role',
        header: 'Peran',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="text-sm">{info.getValue() as string}</span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'created_at',
        header: 'Dibuat',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="text-sm tabular-nums">
              {formatDateTime(info.getValue() as string)}
            </span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'actions',
        header: 'Aksi',
        cell: actionCell,
        size: 120,
        minSize: 120,
      },
    ],
    [loading],
  )

  return columns
}

interface ActionCellProps {
  record: Models.User
}

function ActionCell({ record }: ActionCellProps) {
  const [openEdit, setOpenEdit] = useState(false)

  return (
    <>
      <RowColumnAction onEdit={() => setOpenEdit(true)} />

      <EditUserDrawer open={openEdit} onOpenChange={setOpenEdit} record={record} />
    </>
  )
}
