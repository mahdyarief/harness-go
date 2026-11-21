import z from 'zod'

import { requiredString } from '@/lib/validation'

export const ChatTurnSchema = z.object({
  content: requiredString('content').max(8000),
  stream: z.boolean().optional(),
  use_web_search: z.boolean().optional(),
  use_knowledge_search: z.boolean().optional(),
})

export type ChatTurnDto = z.infer<typeof ChatTurnSchema>

export const ChatSessionTitleSchema = z.object({
  title: requiredString('title').max(200),
})

export type ChatSessionTitleDto = z.infer<typeof ChatSessionTitleSchema>

export const CreateChatSessionSchema = z.object({
  title: z.string().max(200).optional(),
})

export type CreateChatSessionDto = z.infer<typeof CreateChatSessionSchema>
