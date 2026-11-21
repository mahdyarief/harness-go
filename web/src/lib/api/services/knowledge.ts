import { api } from '../client-fetch'
import type {
  KnowledgeCategory,
  KnowledgeDocument,
  KnowledgeEmbeddingConfig,
  KnowledgeEmbeddingInput,
  KnowledgeIndexing,
  KnowledgeList,
  KnowledgeListParams,
  KnowledgeDocumentUsage,
  KnowledgePreview,
  KnowledgeScope,
  KnowledgeStatus,
  KnowledgeUsageParams,
  KnowledgeUsageWindow,
} from '../models/knowledge'

function path(scope: KnowledgeScope) {
  return scope === null
    ? '/api/v1/platform/knowledge'
    : `/api/v1/tenants/${encodeURIComponent(scope)}/knowledge`
}

interface CategoryList {
  data: KnowledgeCategory[]
  metadata: { total: number; offset: number; limit: number }
}

export const knowledgeServices = {
  async embeddingConfig(signal?: AbortSignal) {
    return (
      await api.get<{ data: KnowledgeEmbeddingConfig }>(`${path(null)}/embedding-config`, {
        signal,
      })
    ).data.data
  },
  async saveEmbeddingConfig(input: KnowledgeEmbeddingInput) {
    await api.put(`${path(null)}/embedding-config`, input)
  },
  async indexing(scope: KnowledgeScope, id: string, signal?: AbortSignal) {
    return (
      await api.get<{ data: KnowledgeIndexing }>(
        `${path(scope)}/${encodeURIComponent(id)}/indexing`,
        { signal },
      )
    ).data.data
  },
  async reindex(scope: KnowledgeScope, id: string) {
    await api.post(`${path(scope)}/${encodeURIComponent(id)}/reindex`)
  },
  async preview(
    scope: KnowledgeScope,
    id: string,
    params: { offset: number; limit: number },
    signal?: AbortSignal,
  ) {
    return (
      await api.get<KnowledgePreview>(`${path(scope)}/${encodeURIComponent(id)}/preview`, {
        params,
        signal,
      })
    ).data
  },
  async list(scope: KnowledgeScope, params: KnowledgeListParams, signal?: AbortSignal) {
    return (await api.get<KnowledgeList>(path(scope), { params, signal })).data
  },
  async upload(scope: KnowledgeScope, input: { title: string; file: File; categoryId?: string }) {
    const body = new FormData()
    body.append('title', input.title)
    if (input.categoryId) body.append('category_id', input.categoryId)
    body.append('file', input.file)
    // Hapus default JSON agar browser menetapkan boundary FormData.
    return (
      await api.post<{ data: KnowledgeDocument }>(path(scope), body, {
        headers: { 'Content-Type': undefined },
      })
    ).data.data
  },
  async remove(scope: KnowledgeScope, id: string) {
    await api.delete(`${path(scope)}/${encodeURIComponent(id)}`)
  },
  async window(scope: KnowledgeScope, params: KnowledgeUsageParams) {
    return (await api.get<{ data: KnowledgeUsageWindow[] }>(`${path(scope)}/metrics`, { params }))
      .data.data
  },
  async document(scope: KnowledgeScope, id: string) {
    return (
      await api.get<{ data: KnowledgeDocumentUsage }>(
        `${path(scope)}/${encodeURIComponent(id)}/metrics`,
      )
    ).data.data
  },
  async categories(scope: KnowledgeScope, signal?: AbortSignal) {
    return (
      await api.get<CategoryList>(`${path(scope)}/categories`, {
        params: { limit: 100, sorted: 'name:asc' },
        signal,
      })
    ).data
  },
  async createCategory(scope: KnowledgeScope, name: string) {
    return (await api.post<{ data: KnowledgeCategory }>(`${path(scope)}/categories`, { name })).data
      .data
  },
  async renameCategory(scope: KnowledgeScope, id: string, name: string) {
    return (
      await api.patch<{ data: KnowledgeCategory }>(
        `${path(scope)}/categories/${encodeURIComponent(id)}`,
        { name },
      )
    ).data.data
  },
  async deleteCategory(scope: KnowledgeScope, id: string) {
    await api.delete(`${path(scope)}/categories/${encodeURIComponent(id)}`)
  },
  async setCategory(scope: KnowledgeScope, id: string, categoryId: string | null) {
    return (
      await api.patch<{ data: KnowledgeDocument }>(
        `${path(scope)}/${encodeURIComponent(id)}/category`,
        { category_id: categoryId },
      )
    ).data.data
  },
  async setStatus(scope: KnowledgeScope, id: string, status: KnowledgeStatus) {
    return (
      await api.patch<{ data: KnowledgeDocument }>(
        `${path(scope)}/${encodeURIComponent(id)}/status`,
        { status },
      )
    ).data.data
  },
}
