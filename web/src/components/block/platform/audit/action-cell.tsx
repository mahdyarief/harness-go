import { useState } from 'react'

import RowColumnAction from '@/components/block/common/row-column-action'
import type { Models } from '@/lib/api/models'

import { AuditDetailDrawer } from './detail-drawer'

/** Row action: opens the read-only detail drawer for one audit entry. */
export function AuditActionCell({ record }: { record: Models.AuditLog }) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <RowColumnAction onShow={() => setOpen(true)} />
      <AuditDetailDrawer open={open} onOpenChange={setOpen} record={record} />
    </>
  )
}
