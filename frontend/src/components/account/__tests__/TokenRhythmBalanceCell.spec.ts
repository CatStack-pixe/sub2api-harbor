import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { invalidateTokenRhythmBalanceCache } from '@/utils/tokenRhythmBalanceQuery'

const { getTokenRhythmBalanceMock } = vi.hoisted(() => ({
  getTokenRhythmBalanceMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getTokenRhythmBalance: getTokenRhythmBalanceMock,
    },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import TokenRhythmBalanceCell from '../TokenRhythmBalanceCell.vue'

describe('TokenRhythmBalanceCell', () => {
  beforeEach(() => {
    getTokenRhythmBalanceMock.mockReset()
    invalidateTokenRhythmBalanceCache()
  })

  it('automatically probes and displays the available CNY balance', async () => {
    getTokenRhythmBalanceMock.mockResolvedValueOnce({
      available_balance_cny: 609.49,
      balance_cny: 610,
      currency: 'CNY',
    })

    const wrapper = mount(TokenRhythmBalanceCell, {
      props: { account: { id: 42, platform: 'tokenrhythm' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    expect(getTokenRhythmBalanceMock).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('CNY 609.49')
  })

  it('uses a forced request when refreshed', async () => {
    getTokenRhythmBalanceMock
      .mockResolvedValueOnce({ available_balance_cny: 10, currency: 'CNY' })
      .mockResolvedValueOnce({ available_balance_cny: 9.25, currency: 'CNY' })

    const wrapper = mount(TokenRhythmBalanceCell, {
      props: { account: { id: 42, platform: 'tokenrhythm' } as never },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(getTokenRhythmBalanceMock).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('CNY 9.25')
  })
})
