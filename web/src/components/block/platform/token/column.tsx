'use client'

import { useMemo } from 'react'

import type { Models } from '@/lib/api/models'
import type { AppColumnDef, BaseColumnProps } from '@/types/column'

import { formatDateTime } from '@/lib/date'

import CellItem from '../../common/cell-item'
import { TokenBalanceActionCell } from './action-cell'

type ColumnType = AppColumnDef<Models.TokenBalance>

// Declared once, and never swapped for a skeleton: the cell owns the row's open
// drawers, so remounting it on a background refetch would discard them.
const actionCell: ColumnType['cell'] = ({ row }) => <TokenBalanceActionCell record={row.original} />

function formatToken(n: number): string {
  return n.toLocaleString('id-ID')
}

export function useTokenBalanceColumns({ loading }: BaseColumnProps) {
  const columns = useMemo<ColumnType[]>(
    () => [
      {
        accessorKey: 'tenant_name',
        header: 'Tenant',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="font-medium">{(info.getValue() as string) || '-'}</span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'balance',
        header: 'Saldo Token',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="font-mono text-sm tabular-nums">
              {formatToken(info.getValue() as number)}
            </span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'updated_at',
        header: 'Terakhir Diperbarui',
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
        size: 160,
        minSize: 160,
      },
    ],
    [loading],
  )

  return columns
}
