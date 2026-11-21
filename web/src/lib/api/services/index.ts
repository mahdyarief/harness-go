import { knowledgeServices } from './knowledge'
import { authServices } from './auth'
import { auditServices } from './audit'
import { categoryServices } from './category'
import { chatServices } from './chat'
import { exaConfigService } from './exa-config'
import { healthServices } from './health'
import { llmConfigService } from './llm-config'
import { tenantServices } from './tenant'
import { tokenServices } from './token'
import { userServices } from './user'

export const services = {
  knowledge: knowledgeServices,
  health: healthServices,
  auth: authServices,
  audit: auditServices,
  categories: categoryServices,
  chat: chatServices,
  exaConfig: exaConfigService,
  llmConfig: llmConfigService,
  tenants: tenantServices,
  tokens: tokenServices,
  users: userServices,
} as const
