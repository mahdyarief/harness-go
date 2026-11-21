'use client'

import { useState } from 'react'

import type { Models } from '@/lib/api/models'

import RowColumnAction from '../../common/row-column-action'
import { HistoryDrawer } from './history-drawer'
import { TopUpDrawer } from './topup-drawer'

export function TokenBalanceActionCell({ record }: { record: Models.TokenBalance }) {
  const [openTopUp, setOpenTopUp] = useState(false)
  const [openHistory, setOpenHistory] = useState(false)

  return (
    <>
      <RowColumnAction onEdit={() => setOpenTopUp(true)} onShow={() => setOpenHistory(true)} />
      <TopUpDrawer
        open={openTopUp}
        onOpenChange={setOpenTopUp}
        tenantId={record.tenant_id}
        tenantName={record.tenant_name ?? record.tenant_id}
      />
      <HistoryDrawer
        open={openHistory}
        onOpenChange={setOpenHistory}
        tenantId={record.tenant_id}
        tenantName={record.tenant_name ?? record.tenant_id}
      />
    </>
  )
}
