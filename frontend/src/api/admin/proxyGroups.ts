/**
 * Admin proxy group API endpoints.
 */

import { apiClient } from '../client'
import type {
  CreateProxyGroupRequest,
  ProxyGroup,
  UpdateProxyGroupRequest
} from '@/types'

export async function list(): Promise<ProxyGroup[]> {
  const { data } = await apiClient.get<ProxyGroup[]>('/admin/proxy-groups')
  return data
}

export async function create(payload: CreateProxyGroupRequest): Promise<ProxyGroup> {
  const { data } = await apiClient.post<ProxyGroup>('/admin/proxy-groups', payload)
  return data
}

export async function update(id: number, payload: UpdateProxyGroupRequest): Promise<ProxyGroup> {
  const { data } = await apiClient.put<ProxyGroup>(`/admin/proxy-groups/${id}`, payload)
  return data
}

export async function deleteGroup(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/proxy-groups/${id}`)
  return data
}

export const proxyGroupsAPI = {
  list,
  create,
  update,
  delete: deleteGroup
}

export default proxyGroupsAPI
