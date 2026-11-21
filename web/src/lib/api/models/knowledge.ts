export interface KnowledgeIndexing {
  status: 'not_configured' | 'pending' | 'indexing' | 'ready' | 'partial' | 'failed'
  indexed_chunks: number
  eligible_chunks: number
  profile_revision: string
  updated_at: string
  error_code?: string
}

export interface KnowledgeEmbeddingConfig {
  endpoint: string
  model: 'BAAI/bge-m3'
  revision: string
  dimensions: 1024
  enabled: boolean
  has_credential: boolean
  updated_at: string
}

export interface KnowledgeEmbeddingInput {
  endpoint: string
  revision: string
  enabled: boolean
  credential?: string
  clear_credential: boolean
}

export interface KnowledgeExtractionMetadata {
  status: 'complete' | 'limited'
  extractor_version: string
  warnings: string[]
  coverage: {
    unit: string
    scope: string
    processed: number
    total: number | null
  }[]
}

/** Draft menunggu review; published dapat dipakai retrieval agen. */
export type KnowledgeStatus = 'draft' | 'published'

export interface KnowledgeDocument {
  id: string
  tenant_id: string | null
  category_id: string | null
  category_name: string | null
  title: string
  filename: string
  format: string
  size_bytes: number
  char_count: number
  chunk_count: number
  extraction_metadata: KnowledgeExtractionMetadata | null
  status: KnowledgeStatus
  uploaded_by: string | null
  created_at: string
  updated_at: string
}

/** One category used to group documents inside a library. */
export interface KnowledgeCategory {
  id: string
  tenant_id: string | null
  name: string
  slug: string
  created_at: string
  updated_at: string
}

/** null selects the platform library; a string selects one tenant library. */
export type KnowledgeScope = string | null

export interface KnowledgeListParams {
  offset: number
  limit: number
  search?: string
  sorted?: string
  category_id?: string
  uncategorized?: boolean
}

export interface KnowledgePreview {
  data: {
    document: KnowledgeDocument
    chunks: { position: number; heading_path: string; locator: string; content: string }[]
  }
  metadata: { total: number; offset: number; limit: number }
}

export interface KnowledgeList {
  data: KnowledgeDocument[]
  metadata: { total: number; offset: number; limit: number }
}

/** Aggregated usage of one knowledge document over a time window. */
export interface KnowledgeDocumentUsage {
  document: KnowledgeDocument
  citations: number
  retrievals: number
  semantic_citations: number
  semantic_retrievals: number
  last_used_at: string
}

export function citationRate(usage: KnowledgeDocumentUsage): number {
  return usage.retrievals > 0 ? usage.citations / usage.retrievals : 0
}

export function semanticShare(usage: KnowledgeDocumentUsage): number {
  return usage.retrievals > 0 ? usage.semantic_retrievals / usage.retrievals : 0
}

/** One usage row in the windowed ranking of every document. */
export type KnowledgeUsageWindow = KnowledgeDocumentUsage

export interface KnowledgeUsageParams {
  days?: number
}
