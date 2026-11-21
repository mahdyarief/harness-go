'use client'

import { useMemo } from 'react'

import type { Models } from '@/lib/api/models'
import type { AppColumnDef, BaseColumnProps } from '@/types/column'

import { formatDateTime } from '@/lib/date'

import CellItem from '../../common/cell-item'
import { AuditActionCell } from './action-cell'

type ColumnType = AppColumnDef<Models.AuditLog>

/** Shorten a UUID for table display without pretending it is the full id. */
function shortId(id: string | null): string {
  if (!id) return '-'
  return id.slice(0, 8)
}

export function useAuditColumns({ loading }: BaseColumnProps) {
  const columns = useMemo<ColumnType[]>(
    () => [
      {
        accessorKey: 'occurred_at',
        header: 'Waktu',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="text-sm tabular-nums">
              {formatDateTime(info.getValue() as string)}
            </span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'actor_name',
        header: 'Aktor',
        cell: ({ row }) => (
          <CellItem loading={loading}>
            <div className="flex flex-col">
              <span className="font-medium">
                {row.original.actor_name || shortId(row.original.actor_id)}
              </span>
              <span className="text-ink-2 text-xs">{row.original.actor_role || '-'}</span>
            </div>
          </CellItem>
        ),
      },
      {
        accessorKey: 'action',
        header: 'Aksi',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="font-mono text-xs">{info.getValue() as string}</span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'entity_type',
        header: 'Entitas',
        cell: ({ row }) => (
          <CellItem loading={loading}>
            <div className="flex flex-col">
              <span className="text-sm">{row.original.entity_type}</span>
              {row.original.entity_id && (
                <span className="text-ink-2 font-mono text-xs">
                  {shortId(row.original.entity_id)}
                </span>
              )}
            </div>
          </CellItem>
        ),
      },
      {
        accessorKey: 'tenant_id',
        header: 'Tenant',
        cell: ({ row }) => (
          <CellItem loading={loading}>
            <span className="text-ink-2 font-mono text-xs">
              {shortId(row.original.tenant_id)}
            </span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'ip',
        header: 'IP',
        cell: (info) => (
          <CellItem loading={loading}>
            <span className="text-ink-2 font-mono text-xs">{(info.getValue() as string) || '-'}</span>
          </CellItem>
        ),
      },
      {
        accessorKey: 'actions',
        header: 'Aksi',
        cell: ({ row }) => <AuditActionCell record={row.original} />,
        size: 120,
        minSize: 120,
      },
    ],
    [loading],
  )

  return columns
}
