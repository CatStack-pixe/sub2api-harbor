import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderBalanceCell from '../CNProviderBalanceCell.vue'
import type { Account } from '@/types'

const { queryBalance } = vi.hoisted(() => ({
  queryBalance: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryBalance }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const account = {
  id: 7,
  platform: 'kimi',
  type: 'apikey',
  credentials: { account_mode: 'payg' },
  extra: {
    kimi_balance: 12.5,
    kimi_balance_currency: 'CNY'
  }
} as Account

describe('CNProviderBalanceCell', () => {
  beforeEach(() => {
    queryBalance.mockReset()
  })

  it('renders the persisted balance as static text with an explicit query action', async () => {
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })
    await flushPromises()

    // Snapshot value renders without any probe.
    expect(queryBalance).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('CNY 12.50')

    // The control reads as an action; the i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-balance-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenCalledWith(account.id)
  })

  it('shows the low-balance badge from the snapshot marker', () => {
    const lowAccount = {
      ...account,
      extra: { kimi_balance: 0.4, kimi_balance_low: true }
    } as Account

    const wrapper = mount(CNProviderBalanceCell, { props: { account: lowAccount } })

    expect(wrapper.text()).toContain('admin.accounts.cnProviders.balanceLow')
  })

  it('keeps the snapshot balance visible when a query fails', async () => {
    queryBalance.mockResolvedValue({ success: false, error: 'HTTP 401' })
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })

    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('CNY 12.50')
    expect(wrapper.text()).toContain('HTTP 401')
  })

  it.each(['cline', 'command_code'])('renders persisted %s credits and supports manual refresh', async (platform) => {
    queryBalance.mockResolvedValue({ success: true, balance: 9.25, currency: 'USD' })
    const wrapper = mount(CNProviderBalanceCell, {
      props: {
        account: {
          ...account,
          platform,
          credentials: { account_mode: 'payg' },
          extra: { [`${platform}_balance`]: 12.5, [`${platform}_balance_currency`]: 'USD' }
        }
      }
    })
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('USD 12.50')
    expect(queryBalance).not.toHaveBeenCalled()

    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenCalledWith(account.id)
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('USD 9.25')
    wrapper.unmount()
  })

  it.each(['cline', 'command_code'])('hides %s relay balance snapshots and refresh controls', async (platform) => {
    const wrapper = mount(CNProviderBalanceCell, {
      props: {
        account: {
          ...account,
          platform,
          credentials: { base_url: 'https://relay.example.com/v1' },
          extra: { [`${platform}_balance`]: 12.5, [`${platform}_balance_currency`]: 'USD' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-test="cn-provider-balance-value"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="cn-provider-balance-probe"]').exists()).toBe(false)
    expect(queryBalance).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('updates credits from a sibling quota probe without a second request', async () => {
    const wrapper = mount(CNProviderBalanceCell, {
      props: {
        account: { ...account, platform: 'cline', credentials: {}, extra: {} }
      }
    })
    await wrapper.setProps({
      balanceResult: {
        provider: 'cline', success: true, balance: 7.5, currency: 'USD',
        fetched_at: 1, persisted: true
      }
    })

    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('USD 7.50')
    expect(queryBalance).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
