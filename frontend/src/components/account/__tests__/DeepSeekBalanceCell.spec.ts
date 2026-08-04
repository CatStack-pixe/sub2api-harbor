import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

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
  it('probes and displays provider balances', async () => {
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

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(getDeepSeekBalanceMock).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('CNY 10.50')
    expect(wrapper.text()).toContain('USD 1.25')
  })

  it('does not render for another account platform', () => {
    const wrapper = mount(DeepSeekBalanceCell, {
      props: { account: { id: 42, platform: 'openai' } as never },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.find('button').exists()).toBe(false)
  })
})
