import { createFileRoute } from '@tanstack/react-router'
import KnowledgePage from '@/components/block/knowledge/page'

export const Route = createFileRoute('/(app)/knowledge/')({
  component: KnowledgePage,
})
