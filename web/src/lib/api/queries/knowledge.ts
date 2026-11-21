import { mutationOptions, queryOptions } from '@tanstack/react-query'
import { getQueryClient } from '@/lib/providers/react-query'
import type {
  KnowledgeEmbeddingInput,
  KnowledgeListParams,
  KnowledgeScope,
  KnowledgeStatus,
} from '../models/knowledge'
import { knowledgeServices } from '../services/knowledge'

export const knowledgeKeys = {
  all: ['knowledge'] as const,
  scope: (scope: KnowledgeScope) => [...knowledgeKeys.all, scope] as const,
  embeddingConfig: ['knowledge', 'embedding-config'] as const,
  indexing: (scope: KnowledgeScope, id: string) =>
    [...knowledgeKeys.scope(scope), 'indexing', id] as const,
  preview: (scope: KnowledgeScope, id: string, offset: number, limit = 10) =>
    [...knowledgeKeys.scope(scope), 'preview', id, offset, limit] as const,
  list: (scope: KnowledgeScope, params: KnowledgeListParams) =>
    [...knowledgeKeys.scope(scope), 'list', params] as const,
  usage: (scope: KnowledgeScope, days: number) =>
    [...knowledgeKeys.scope(scope), 'usage', days] as const,
  usageDocument: (scope: KnowledgeScope, id: string) =>
    [...knowledgeKeys.scope(scope), 'usage', 'document', id] as const,
  categories: (scope: KnowledgeScope) => [...knowledgeKeys.scope(scope), 'categories'] as const,
}

export const knowledgeQueries = {
  embeddingConfig: () =>
    queryOptions({
      queryKey: knowledgeKeys.embeddingConfig,
      queryFn: ({ signal }) => knowledgeServices.embeddingConfig(signal),
      retry: 1,
      refetchOnWindowFocus: false,
    }),
  saveEmbeddingConfig: () =>
    mutationOptions({
      mutationFn: (input: KnowledgeEmbeddingInput) => knowledgeServices.saveEmbeddingConfig(input),
      onSuccess: async () => {
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.all })
      },
    }),
  indexing: (scope: KnowledgeScope, id: string) => {
    let requests = 0
    return queryOptions({
      queryKey: knowledgeKeys.indexing(scope, id),
      staleTime: 0,
      gcTime: 0,
      queryFn: async ({ signal }) => {
        try {
          return await knowledgeServices.indexing(scope, id, signal)
        } finally {
          if (!signal.aborted) requests++
        }
      },
      retry: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      refetchIntervalInBackground: false,
      // React StrictMode dapat membatalkan mount awal sebelum request mencapai server.
      refetchInterval: (query) =>
        !query.state.error &&
        requests < 30 &&
        ['pending', 'indexing'].includes(query.state.data?.status ?? '')
          ? 5000
          : false,
    })
  },
  reindex: () =>
    mutationOptions({
      mutationFn: ({ scope, id }: { scope: KnowledgeScope; id: string }) =>
        knowledgeServices.reindex(scope, id),
      onSuccess: async (_data, { scope, id }) => {
        const client = getQueryClient()
        client.setQueryData(knowledgeKeys.indexing(scope, id), (previous) =>
          previous ? { ...previous, status: 'pending' as const } : previous,
        )
        await Promise.all([
          client.invalidateQueries({ queryKey: knowledgeKeys.indexing(scope, id) }),
          client.invalidateQueries({ queryKey: [...knowledgeKeys.scope(scope), 'list'] }),
        ])
      },
    }),
  preview: (scope: KnowledgeScope, id: string, offset = 0, limit = 10) =>
    queryOptions({
      queryKey: knowledgeKeys.preview(scope, id, offset, limit),
      queryFn: ({ signal }) => knowledgeServices.preview(scope, id, { offset, limit }, signal),
    }),
  list: (scope: KnowledgeScope, params: KnowledgeListParams) =>
    queryOptions({
      queryKey: knowledgeKeys.list(scope, params),
      queryFn: ({ signal }) => knowledgeServices.list(scope, params, signal),
    }),
  usage: (scope: KnowledgeScope, days = 30) =>
    queryOptions({
      queryKey: knowledgeKeys.usage(scope, days),
      queryFn: () => knowledgeServices.window(scope, { days }),
      staleTime: 60_000,
    }),
  usageDocument: (scope: KnowledgeScope, id: string) =>
    queryOptions({
      queryKey: knowledgeKeys.usageDocument(scope, id),
      queryFn: () => knowledgeServices.document(scope, id),
      staleTime: 60_000,
    }),
  categories: (scope: KnowledgeScope) =>
    queryOptions({
      queryKey: knowledgeKeys.categories(scope),
      queryFn: ({ signal }) => knowledgeServices.categories(scope, signal),
      staleTime: 60_000,
    }),
  createCategory: () =>
    mutationOptions({
      mutationFn: ({ scope, name }: { scope: KnowledgeScope; name: string }) =>
        knowledgeServices.createCategory(scope, name),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.categories(scope) })
      },
    }),
  renameCategory: () =>
    mutationOptions({
      mutationFn: ({ scope, id, name }: { scope: KnowledgeScope; id: string; name: string }) =>
        knowledgeServices.renameCategory(scope, id, name),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.categories(scope) })
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.scope(scope) })
      },
    }),
  deleteCategory: () =>
    mutationOptions({
      mutationFn: ({ scope, id }: { scope: KnowledgeScope; id: string }) =>
        knowledgeServices.deleteCategory(scope, id),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.categories(scope) })
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.scope(scope) })
      },
    }),
  setCategory: () =>
    mutationOptions({
      mutationFn: ({
        scope,
        id,
        categoryId,
      }: {
        scope: KnowledgeScope
        id: string
        categoryId: string | null
      }) => knowledgeServices.setCategory(scope, id, categoryId),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.scope(scope) })
      },
    }),
  setStatus: () =>
    mutationOptions({
      mutationFn: ({
        scope,
        id,
        status,
      }: {
        scope: KnowledgeScope
        id: string
        status: KnowledgeStatus
      }) => knowledgeServices.setStatus(scope, id, status),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({ queryKey: knowledgeKeys.scope(scope) })
      },
    }),
  upload: () =>
    mutationOptions({
      mutationFn: ({
        scope,
        title,
        file,
        categoryId,
      }: {
        scope: KnowledgeScope
        title: string
        file: File
        categoryId?: string
      }) => knowledgeServices.upload(scope, { title, file, categoryId }),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({
          queryKey: scope === null ? knowledgeKeys.all : knowledgeKeys.scope(scope),
        })
      },
    }),
  remove: () =>
    mutationOptions({
      mutationFn: ({ scope, id }: { scope: KnowledgeScope; id: string }) =>
        knowledgeServices.remove(scope, id),
      onSuccess: async (_data, { scope }) => {
        await getQueryClient().invalidateQueries({
          queryKey: scope === null ? knowledgeKeys.all : knowledgeKeys.scope(scope),
        })
      },
    }),
}
