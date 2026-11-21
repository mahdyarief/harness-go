import { mutationOptions, queryOptions } from '@tanstack/react-query'

import { DEFAULT_PAGINATE } from '@/lib/constants/paginate'
import { getQueryClient } from '@/lib/providers/react-query'

import type { PaginateDto } from '../dtos/paginate'
import type { CreateUserDto, UpdateUserDto } from '../dtos/user/schema'

import { services } from '../services'

export const USER_QUERY_KEY = 'users'

export const LIST_USER_QUERY_KEY = (params?: PaginateDto & { search?: string }) => {
  return [USER_QUERY_KEY, 'list', params]
}

export const GET_USER_MEMBERSHIP_QUERY_KEY = (id: string) => {
  return [USER_QUERY_KEY, 'membership', id]
}

const list = (params?: PaginateDto & { search?: string }) =>
  queryOptions({
    queryKey: LIST_USER_QUERY_KEY(params),
    queryFn: async () => {
      const res = await services.users.list(params ?? DEFAULT_PAGINATE)
      return res.data
    },
  })

const membership = (userId: string) =>
  queryOptions({
    queryKey: GET_USER_MEMBERSHIP_QUERY_KEY(userId),
    queryFn: () => services.users.membership(userId),
    enabled: Boolean(userId),
  })

const create = () => {
  const qc = getQueryClient()

  return mutationOptions({
    mutationFn: async (reqBody: CreateUserDto) => services.users.store(reqBody),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [USER_QUERY_KEY] })
    },
  })
}

const update = (id: string) => {
  const qc = getQueryClient()

  return mutationOptions({
    mutationFn: async (reqBody: UpdateUserDto) => services.users.update(id, reqBody),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [USER_QUERY_KEY] })
    },
  })
}

export const userQueries = { list, membership, create, update } as const
