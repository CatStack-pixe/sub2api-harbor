import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { invalidateKimiBalanceCache } from '@/utils/kimiBalanceQuery'

const { getKimiBalanceMock } = vi.hoisted(() => ({
  getKimiBalanceMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getKimiBalance: getKimiBalanceMock,
    },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import KimiBalanceCell from '../KimiBalanceCell.vue'

describe('KimiBalanceCell', () => {
  beforeEach(() => {
    getKimiBalanceMock.mockReset()
    invalidateKimiBalanceCache()
  })

  it('automatically probes and displays the available balance', async () => {
    getKimiBalanceMock.mockResolvedValueOnce({
      is_available: true,
      available_balance: 12.5,
      currency: 'CNY',
    })

    const wrapper = mount(KimiBalanceCell, {
      props: { account: { id: 42, platform: 'kimi' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    expect(getKimiBalanceMock).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('CNY 12.50')
  })

  it('uses a forced request when refreshed', async () => {
    getKimiBalanceMock
      .mockResolvedValueOnce({ is_available: true, available_balance: 12.5, currency: 'CNY' })
      .mockResolvedValueOnce({ is_available: true, available_balance: 9.25, currency: 'CNY' })

    const wrapper = mount(KimiBalanceCell, {
      props: { account: { id: 42, platform: 'kimi' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(getKimiBalanceMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('CNY 9.25')
  })
})
