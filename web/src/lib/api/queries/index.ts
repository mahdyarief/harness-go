import { knowledgeQueries } from './knowledge'
import { authQueries } from './auth'
import { auditQueries } from './audit'
import { categoryQueries } from './category'
import { chatQueries } from './chat'
import { exaConfigQueries } from './exa-config'
import { healthQueries } from './health'
import { llmConfigQueries } from './llm-config'
import { tenantQueries } from './tenant'
import { tokenQueries } from './token'
import { userQueries } from './user'

export const queries = {
  knowledge: knowledgeQueries,
  health: healthQueries,
  auth: authQueries,
  audit: auditQueries,
  categories: categoryQueries,
  chat: chatQueries,
  exaConfig: exaConfigQueries,
  llmConfig: llmConfigQueries,
  tenants: tenantQueries,
  tokens: tokenQueries,
  users: userQueries,
} as const
