import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Proxy } from '@/types'
import ProxySelector from '../ProxySelector.vue'

const { testProxy } = vi.hoisted(() => ({
  testProxy: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxies: {
      testProxy,
    },
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

type ProxyOverrides = Partial<Proxy> & {
  proxy_group_id?: number | null
  proxy_group_name?: string | null
}

const makeProxy = (id: number, overrides: ProxyOverrides = {}): Proxy =>
  ({
    id,
    name: `Proxy ${id}`,
    protocol: 'http',
    host: `proxy-${id}.example.com`,
    port: 8000 + id,
    username: null,
    status: 'active',
    expires_at: null,
    fallback_mode: 'none',
    expiry_warn_days: 7,
    created_at: '2026-08-08T00:00:00Z',
    updated_at: '2026-08-08T00:00:00Z',
    proxy_group_id: null,
    proxy_group_name: null,
    ...overrides,
  }) as Proxy

const mountSelector = (proxies: Proxy[], modelValue: number | null = null) =>
  mount(ProxySelector, {
    props: { modelValue, proxies },
    global: {
      stubs: {
        Icon: {
          props: ['name'],
          template: '<i :data-icon="name" />',
        },
      },
    },
  })

const openSelector = async (wrapper: ReturnType<typeof mountSelector>) => {
  await wrapper.get('.select-trigger').trigger('click')
  await flushPromises()
}

const renderedProxyNames = (wrapper: ReturnType<typeof mountSelector>) =>
  wrapper.findAll('[data-testid="proxy-option"]').map((option) => option.text())

afterEach(() => {
  testProxy.mockReset()
})

describe('ProxySelector filtering and bounded rendering', () => {
  it('filters proxy options by proxy group, including ungrouped proxies', async () => {
    const proxies = [
      makeProxy(1, { proxy_group_id: 10, proxy_group_name: 'Primary' }),
      makeProxy(2, { proxy_group_id: 20, proxy_group_name: 'Backup' }),
      makeProxy(3),
    ]
    const wrapper = mountSelector(proxies)
    await openSelector(wrapper)

    const groupFilter = wrapper.get<HTMLSelectElement>('[data-testid="proxy-group-filter"]')
    expect(groupFilter.findAll('option').map((option) => option.attributes('value'))).toEqual([
      'all',
      'ungrouped',
      'group:10',
      'group:20',
    ])

    await groupFilter.setValue('group:20')
    expect(renderedProxyNames(wrapper)).toHaveLength(1)
    expect(renderedProxyNames(wrapper)[0]).toContain('Proxy 2')

    await groupFilter.setValue('ungrouped')
    expect(renderedProxyNames(wrapper)).toHaveLength(1)
    expect(renderedProxyNames(wrapper)[0]).toContain('Proxy 3')
  })

  it('renders at most 100 matching options and asks the user to continue searching', async () => {
    const wrapper = mountSelector(Array.from({ length: 125 }, (_, index) => makeProxy(index + 1)))
    await openSelector(wrapper)

    expect(wrapper.findAll('[data-testid="proxy-option"]')).toHaveLength(100)
    expect(wrapper.get('[data-testid="proxy-result-limit-hint"]').text()).toContain(
      'admin.proxies.resultLimitHint',
    )
  })

  it('keeps the current selection visible when it falls outside the first 100 matches', async () => {
    const proxies = Array.from({ length: 125 }, (_, index) => makeProxy(index + 1))
    const wrapper = mountSelector(proxies, 125)
    await openSelector(wrapper)

    const options = wrapper.findAll('[data-testid="proxy-option"]')
    expect(options).toHaveLength(100)
    expect(options.some((option) => option.text().includes('Proxy 125'))).toBe(true)
    expect(wrapper.get('[data-testid="proxy-option"].select-option-selected').text()).toContain(
      'Proxy 125',
    )
  })
})

describe('ProxySelector batch testing', () => {
  it('tests only the current group and search results', async () => {
    const proxies = [
      makeProxy(1, { name: 'Needle primary', proxy_group_id: 10, proxy_group_name: 'Primary' }),
      makeProxy(2, { name: 'Other primary', proxy_group_id: 10, proxy_group_name: 'Primary' }),
      makeProxy(3, { host: 'needle.backup.test', proxy_group_id: 20, proxy_group_name: 'Backup' }),
      makeProxy(4, { name: 'Needle backup', proxy_group_id: 20, proxy_group_name: 'Backup' }),
    ]
    testProxy.mockResolvedValue({ success: true, message: 'ok' })
    const wrapper = mountSelector(proxies)
    await openSelector(wrapper)

    await wrapper.get<HTMLSelectElement>('[data-testid="proxy-group-filter"]').setValue('group:20')
    await wrapper.get<HTMLInputElement>('.select-search-input').setValue('needle')
    await wrapper.get('[data-testid="proxy-batch-test"]').trigger('click')
    await flushPromises()

    expect(testProxy.mock.calls.map(([id]) => id).sort((a, b) => a - b)).toEqual([3, 4])
  })

  it('disables batch testing when the current filtered result exceeds 100 proxies', async () => {
    const wrapper = mountSelector(Array.from({ length: 101 }, (_, index) => makeProxy(index + 1)))
    await openSelector(wrapper)

    const batchButton = wrapper.get<HTMLButtonElement>('[data-testid="proxy-batch-test"]')
    expect(batchButton.element.disabled).toBe(true)
    expect(batchButton.attributes('title')).toContain('admin.proxies.batchTestLimitExceeded')

    await batchButton.trigger('click')
    expect(testProxy).not.toHaveBeenCalled()
  })

  it('limits batch proxy tests to five concurrent requests', async () => {
    const resolvers: Array<() => void> = []
    let activeRequests = 0
    let peakRequests = 0

    testProxy.mockImplementation(
      () =>
        new Promise((resolve) => {
          activeRequests += 1
          peakRequests = Math.max(peakRequests, activeRequests)
          resolvers.push(() => {
            activeRequests -= 1
            resolve({ success: true, message: 'ok' })
          })
        }),
    )

    const wrapper = mountSelector(Array.from({ length: 12 }, (_, index) => makeProxy(index + 1)))
    await openSelector(wrapper)
    await wrapper.get('[data-testid="proxy-batch-test"]').trigger('click')

    expect(testProxy).toHaveBeenCalledTimes(5)
    expect(peakRequests).toBe(5)

    while (resolvers.length > 0) {
      const currentBatch = resolvers.splice(0)
      currentBatch.forEach((resolve) => resolve())
      await flushPromises()
    }

    expect(testProxy).toHaveBeenCalledTimes(12)
    expect(activeRequests).toBe(0)
    expect(peakRequests).toBeLessThanOrEqual(5)
  })
})
