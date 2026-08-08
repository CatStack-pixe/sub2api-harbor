import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { DeepSeekBalanceResult } from '@/api/admin/accounts'
import { invalidateDeepSeekBalanceCache } from '@/utils/deepSeekBalanceQuery'

const { getDeepSeekBalanceMock } = vi.hoisted(() => ({
  getDeepSeekBalanceMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getDeepSeekBalance: getDeepSeekBalanceMock,
    },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import DeepSeekBalanceCell from '../DeepSeekBalanceCell.vue'

describe('DeepSeekBalanceCell', () => {
  beforeEach(() => {
    getDeepSeekBalanceMock.mockReset()
    invalidateDeepSeekBalanceCache()
  })

  it('automatically probes and displays provider balances', async () => {
    getDeepSeekBalanceMock.mockResolvedValueOnce({
      is_available: true,
      balance_infos: [
        { currency: 'CNY', total_balance: '10.50', granted_balance: '2.00', topped_up_balance: '8.50' },
        { currency: 'USD', total_balance: '1.25', granted_balance: '0', topped_up_balance: '1.25' },
      ],
      fetched_at: 1,
    })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'deepseek' } as never },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()

    expect(getDeepSeekBalanceMock).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('CNY 10.50')
    expect(wrapper.text()).toContain('USD 1.25')
  })

  it('keeps the button available for an explicit refresh', async () => {
    getDeepSeekBalanceMock
      .mockResolvedValueOnce({
        is_available: true,
        balance_infos: [{ currency: 'CNY', total_balance: '10.50' }],
        fetched_at: 1,
      })
      .mockResolvedValueOnce({
        is_available: true,
        balance_infos: [{ currency: 'CNY', total_balance: '9.25' }],
        fetched_at: 2,
      })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'deepseek' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('CNY 9.25')
  })

  it('waits for auto-load and refreshes when the parent token changes', async () => {
    getDeepSeekBalanceMock.mockResolvedValue({
      is_available: true,
      balance_infos: [{ currency: 'CNY', total_balance: '10.50' }],
      fetched_at: 1,
    })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: {
        account: { id: 42, platform: 'deepseek' } as never,
        autoLoad: false,
        refreshToken: 0,
      },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()
    expect(getDeepSeekBalanceMock).not.toHaveBeenCalled()

    await wrapper.setProps({ autoLoad: true })
    await flushPromises()
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ refreshToken: 1 })
    await flushPromises()
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(2)
  })

  it('ignores a stale response after the account changes', async () => {
    let resolveFirst!: (value: DeepSeekBalanceResult) => void
    getDeepSeekBalanceMock
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({
        is_available: true,
        balance_infos: [{ currency: 'CNY', total_balance: '22.00' }],
        fetched_at: 2,
      })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'deepseek' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    await wrapper.setProps({ account: { id: 43, platform: 'deepseek' } as never })
    await flushPromises()
    expect(wrapper.text()).toContain('CNY 22.00')

    resolveFirst({
      is_available: true,
      balance_infos: [{ currency: 'CNY', total_balance: '10.50' }],
      fetched_at: 1,
    })
    await flushPromises()

    expect(getDeepSeekBalanceMock.mock.calls.map(([accountId]) => accountId)).toEqual([42, 43])
    expect(wrapper.text()).toContain('CNY 22.00')
    expect(wrapper.text()).not.toContain('CNY 10.50')
  })

  it('does not reload when the parent replaces an unchanged account row', async () => {
    getDeepSeekBalanceMock.mockResolvedValue({
      is_available: true,
      balance_infos: [{ currency: 'CNY', total_balance: '10.50' }],
      fetched_at: 1,
    })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'deepseek', name: 'old' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    await wrapper.setProps({
      account: { id: 42, platform: 'deepseek', name: 'new' } as never,
    })
    await flushPromises()

    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('CNY 10.50')
  })

  it('runs a pending explicit refresh after automatic loading finishes', async () => {
    let resolveFirst!: (value: DeepSeekBalanceResult) => void
    getDeepSeekBalanceMock
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({
        is_available: true,
        balance_infos: [{ currency: 'CNY', total_balance: '9.25' }],
        fetched_at: 2,
      })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: {
        account: { id: 42, platform: 'deepseek' } as never,
        refreshToken: 0,
      },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    await wrapper.setProps({ refreshToken: 1 })
    resolveFirst({
      is_available: true,
      balance_infos: [{ currency: 'CNY', total_balance: '10.50' }],
      fetched_at: 1,
    })
    await flushPromises()

    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('CNY 9.25')
  })

  it('cancels a hidden row query and reloads when it becomes visible again', async () => {
    let firstSignal!: AbortSignal
    getDeepSeekBalanceMock
      .mockImplementationOnce((_accountId: number, signal: AbortSignal) => {
        firstSignal = signal
        return new Promise((_resolve, reject) => {
          signal.addEventListener('abort', () => reject(new DOMException('cancelled', 'AbortError')))
        })
      })
      .mockResolvedValueOnce({
        is_available: true,
        balance_infos: [{ currency: 'CNY', total_balance: '9.25' }],
        fetched_at: 2,
      })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: {
        account: { id: 42, platform: 'deepseek' } as never,
        autoLoad: true,
      },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    await wrapper.setProps({ autoLoad: false })
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)

    await wrapper.setProps({ autoLoad: true })
    await flushPromises()
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('CNY 9.25')
  })

  it('shows an automatic query error without hiding the refresh control', async () => {
    getDeepSeekBalanceMock.mockRejectedValueOnce({
      response: { data: { message: 'provider unavailable' } },
    })

    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'deepseek' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('provider unavailable')
    expect(wrapper.find('button').exists()).toBe(true)
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
  })

  it('does not render or probe for another account platform', async () => {
    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'openai' } as never },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.find('button').exists()).toBe(false)
    await flushPromises()
    expect(getDeepSeekBalanceMock).not.toHaveBeenCalled()
  })
})
