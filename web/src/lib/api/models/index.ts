/* eslint-disable @typescript-eslint/no-namespace -- namespace groups the model type aliases */
export namespace Models {
  export type KnowledgeDocument = import('./knowledge').KnowledgeDocument
  export type Health = import('./health').Health
  export type User = import('./user').User
  export type UserTenantMembership = import('./user').UserTenantMembership
  export type SignInData = import('./user').SignInData
  export type Category = import('./category').Category
  export type LlmConfig = import('./llm-config').LlmConfig
  export type ExaConfig = import('./exa-config').ExaConfig
  export type ChatSession = import('./chat').ChatSession
  export type ChatMessage = import('./chat').ChatMessage
  export type ChatSessionDetail = import('./chat').ChatSessionDetail
  export type TenantMembership = import('./tenant').TenantMembership
  export type PlatformTenant = import('./tenant').PlatformTenant
  export type TokenBalance = import('./token').TokenBalance
  export type TokenTopup = import('./token').TokenTopup
  export type TokenUsage = import('./token').TokenUsage
  export type AuditLog = import('./audit').AuditLog
}
