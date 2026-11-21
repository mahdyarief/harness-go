import { createFileRoute } from '@tanstack/react-router'

import CategoryTable from '@/components/block/master/category/table'

export const Route = createFileRoute('/(app)/master/categories/')({
  component: RouteComponent,
})

function RouteComponent() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-ink text-2xl font-semibold tracking-tight">Kategori</h1>
        <p className="text-ink-2 text-[13px]">
          Data induk yang dipakai bersama di seluruh aplikasi. Cari, tambah, dan tinjau entri di sini.
        </p>
      </div>

      <CategoryTable />
    </div>
  )
}
