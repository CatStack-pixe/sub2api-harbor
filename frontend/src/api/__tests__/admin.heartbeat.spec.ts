import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put }
}))

import heartbeatAPI from '@/api/admin/heartbeat'

describe('admin heartbeat API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
  })

  it('reads configuration, options, and runtime status', async () => {
    const config = { enabled: true, default_group_id: 12 }
    const options = { groups: [], proxy_groups: [] }
    const status = { enabled: true, running: true, queued: 2 }
    get.mockResolvedValueOnce({ data: config })
    get.mockResolvedValueOnce({ data: options })
    get.mockResolvedValueOnce({ data: status })

    await expect(heartbeatAPI.getConfig()).resolves.toEqual(config)
    await expect(heartbeatAPI.getOptions()).resolves.toEqual(options)
    await expect(heartbeatAPI.getStatus()).resolves.toEqual(status)

    expect(get).toHaveBeenNthCalledWith(1, '/admin/heartbeat/config')
    expect(get).toHaveBeenNthCalledWith(2, '/admin/heartbeat/options')
    expect(get).toHaveBeenNthCalledWith(3, '/admin/heartbeat/status')
  })

  it('updates configuration through the dedicated endpoint', async () => {
    const payload = {
      enabled: false,
      vault_url: 'https://vault.example.test',
      allow_insecure_vault: false,
      allowed_source_ips: ['127.0.0.1'],
      default_group_id: 12,
      targets: [{ group_id: 12, proxy_group_id: 1 }],
      worker_count: 2,
      proxy_probe_workers: 4,
      proxy_probe_sample_size: 10,
      proxy_probe_timeout_seconds: 5,
      proxy_sweep_ttl_seconds: 300,
      max_attempts: 3
    }
    const response = { ...payload, config_source: 'database' }
    put.mockResolvedValueOnce({ data: response })

    await expect(heartbeatAPI.updateConfig(payload)).resolves.toEqual(response)
    expect(put).toHaveBeenCalledWith('/admin/heartbeat/config', payload)
  })
})
