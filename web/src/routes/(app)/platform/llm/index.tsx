import { createFileRoute } from '@tanstack/react-router'

import LlmConfigPage from '@/components/block/platform/llm/config'

export const Route = createFileRoute('/(app)/platform/llm/')({
  component: LlmConfigPage,
})
