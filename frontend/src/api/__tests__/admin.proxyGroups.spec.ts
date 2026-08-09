import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put, deleteRequest } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  deleteRequest: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    put,
    delete: deleteRequest
  }
}))

import * as proxyGroups from '@/api/admin/proxyGroups'
import { batchGroup, exportData, list as listProxies } from '@/api/admin/proxies'

describe('admin proxy group API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
    deleteRequest.mockReset()
  })

  it('uses the proxy group CRUD endpoints', async () => {
    get.mockResolvedValueOnce({ data: [{ id: 1, name: 'Primary' }] })
    post.mockResolvedValueOnce({ data: { id: 2, name: 'New' } })
    put.mockResolvedValueOnce({ data: { id: 2, name: 'Renamed' } })
    deleteRequest.mockResolvedValueOnce({ data: { message: 'ok' } })

    await expect(proxyGroups.list()).resolves.toEqual([{ id: 1, name: 'Primary' }])
    await proxyGroups.create({ name: 'New' })
    await proxyGroups.update(2, { name: 'Renamed' })
    await proxyGroups.deleteGroup(2)

    expect(get).toHaveBeenCalledWith('/admin/proxy-groups')
    expect(post).toHaveBeenCalledWith('/admin/proxy-groups', { name: 'New' })
    expect(put).toHaveBeenCalledWith('/admin/proxy-groups/2', { name: 'Renamed' })
    expect(deleteRequest).toHaveBeenCalledWith('/admin/proxy-groups/2')
  })

  it('sends the batch group request including an ungrouped target', async () => {
    post.mockResolvedValueOnce({ data: { updated: 3 } })

    await expect(batchGroup([3, 2, 3, 1], null)).resolves.toEqual({ updated: 3 })

    expect(post).toHaveBeenCalledWith('/admin/proxies/batch-group', {
      ids: [3, 2, 1],
      proxy_group_id: null
    })
  })

  it('passes a group filter to proxy list requests', async () => {
    get.mockResolvedValueOnce({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 0 } })

    await listProxies(1, 20, { group_id: 7 })

    expect(get).toHaveBeenCalledWith(
      '/admin/proxies',
      expect.objectContaining({
        params: expect.objectContaining({ group_id: 7 })
      })
    )
  })

  it('serializes the ungrouped filter for JSON exports', async () => {
    get.mockResolvedValueOnce({ data: { exported_at: '', proxies: [], accounts: [] } })

    await exportData({ filters: { ungrouped: true } })

    expect(get).toHaveBeenCalledWith('/admin/proxies/data', {
      params: { ungrouped: 'true' }
    })
  })
})
