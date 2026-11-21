import { createFileRoute } from '@tanstack/react-router'

import ChatPage from '@/components/block/chat/chat-page'

export const Route = createFileRoute('/(app)/chat/')({
  component: ChatPage,
})
