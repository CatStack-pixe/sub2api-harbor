import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import {
  createRegistrationToken,
  listClients,
  listDeliveries,
  listRegistrationTokens,
  retryDelivery,
  revokeClient
} from '@/api/admin/remoteIngest'

describe('admin remote ingest API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 0 } })
    post.mockResolvedValue({ data: { id: 'result-id' } })
  })

  it('creates a registration token with its expiry and idempotency key', async () => {
    await createRegistrationToken(600, { idempotencyKey: 'token-operation' })

    expect(post).toHaveBeenCalledWith(
      '/admin/remote-ingest/registration-tokens',
      { expires_in_seconds: 600 },
      { headers: { 'Idempotency-Key': 'token-operation' } }
    )
  })

  it('lists tokens, clients, and deliveries with pagination and abort support', async () => {
    const signal = new AbortController().signal

    await listRegistrationTokens(2, 10, { status: 'available' }, { signal })
    await listClients(3, 20, { search: 'worker', status: 'active' }, { signal })
    await listDeliveries(4, 50, { search: 'external-1', status: 'probe_failed' }, { signal })

    expect(get).toHaveBeenNthCalledWith(1, '/admin/remote-ingest/registration-tokens', {
      params: { page: 2, page_size: 10, status: 'available' },
      signal
    })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/remote-ingest/clients', {
      params: { page: 3, page_size: 20, search: 'worker', status: 'active' },
      signal
    })
    expect(get).toHaveBeenNthCalledWith(3, '/admin/remote-ingest/deliveries', {
      params: { page: 4, page_size: 50, search: 'external-1', status: 'probe_failed' },
      signal
    })
  })

  it('revokes clients and retries deliveries with encoded IDs and idempotency keys', async () => {
    await revokeClient('client/id', { idempotencyKey: 'revoke-operation' })
    await retryDelivery('delivery/id', { idempotencyKey: 'retry-operation' })

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/admin/remote-ingest/clients/client%2Fid/revoke',
      undefined,
      { headers: { 'Idempotency-Key': 'revoke-operation' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/admin/remote-ingest/deliveries/delivery%2Fid/retry',
      undefined,
      { headers: { 'Idempotency-Key': 'retry-operation' } }
    )
  })
})
