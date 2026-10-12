import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderQuotaCell from '../CNProviderQuotaCell.vue'
import type { Account } from '@/types'

const { queryQuota } = vi.hoisted(() => ({
  queryQuota: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryQuota }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const account = {
  id: 7,
  platform: 'zhipu',
  type: 'apikey',
  credentials: { account_mode: 'coding' },
  extra: {
    zhipu_5h_used_percent: 0,
    zhipu_weekly_used_percent: 27,
    zhipu_5h_reset_at: '2026-08-18T12:30:00+08:00',
    zhipu_weekly_reset_at: '2026-08-22T00:00:00+08:00',
    zhipu_usage_updated_at: new Date().toISOString()
  }
} as Account

describe('CNProviderQuotaCell', () => {
  beforeEach(() => {
    queryQuota.mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('debounces automatic probes across remounts and retries at the five-minute boundary', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-12T00:00:00Z'))
    queryQuota.mockResolvedValue({ success: true, tiers: [] })
    const staleAccount = { ...account, id: 27001, extra: {} }
    const first = mount(CNProviderQuotaCell, { props: { account: staleAccount } })
    await flushPromises()
    first.unmount()

    vi.setSystemTime(new Date('2026-10-12T00:04:59Z'))
    const second = mount(CNProviderQuotaCell, { props: { account: staleAccount } })
    await flushPromises()
    second.unmount()
    expect(queryQuota).toHaveBeenCalledTimes(1)

    vi.setSystemTime(new Date('2026-10-12T00:05:00Z'))
    const third = mount(CNProviderQuotaCell, { props: { account: staleAccount } })
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledTimes(2)
    third.unmount()
  })

  it('shares automatic debounce between simultaneous rows without delaying manual refresh or other accounts', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-12T01:00:00Z'))
    queryQuota.mockResolvedValue({ success: false, error: 'temporary upstream failure' })
    const staleAccount = { ...account, id: 27002, extra: {} }
    const first = mount(CNProviderQuotaCell, { props: { account: staleAccount } })
    const second = mount(CNProviderQuotaCell, { props: { account: staleAccount } })
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledTimes(1)

    await second.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledTimes(2)

    const other = mount(CNProviderQuotaCell, { props: { account: { ...staleAccount, id: 27003 } } })
    await flushPromises()
    expect(queryQuota.mock.calls).toEqual([[27002], [27002], [27003]])
    first.unmount()
    second.unmount()
    other.unmount()
  })

  it('keeps the compact quota stack readable inside the account table cell', async () => {
    queryQuota.mockResolvedValue({
      success: true,
      tiers: [
        { window: '5h', used_percent: 0, reset_at: '2026-08-18T12:30:00+08:00' },
        { window: 'weekly', used_percent: 27, reset_at: '2026-08-22T00:00:00+08:00' }
      ]
    })
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })

    const root = wrapper.get('[data-test="cn-provider-quota"]')
    expect(root.classes()).toContain('min-w-[220px]')

    const probeButton = root.get('button')
    expect(probeButton.classes()).toContain('whitespace-nowrap')
    expect(probeButton.classes()).toContain('leading-4')
    await probeButton.trigger('click')
    await flushPromises()

    const tiers = root.findAll('[data-test="cn-provider-quota-tier"]')
    expect(tiers).toHaveLength(2)
    for (const tier of tiers) {
      expect(tier.classes()).toContain('min-w-0')
      expect(tier.classes()).toContain('leading-4')
    }

    const labels = root.findAll('[data-test="cn-provider-quota-label"]')
    expect(labels).toHaveLength(2)
    for (const label of labels) {
      expect(label.classes()).toContain('w-14')
      expect(label.classes()).toContain('whitespace-nowrap')
    }

    expect(queryQuota).toHaveBeenCalledWith(account.id)
  })

  it('labels the refresh control with an explicit action verb, not a data caption', async () => {
    const wrapper = mount(CNProviderQuotaCell, { props: { account } })
    await flushPromises()

    // The snapshot is fresh (usage_updated_at = now): bars render without probing.
    expect(queryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('27%')

    // The control reads as an action ("query"), unlike the old noun label
    // ("5-hour window/weekly window") which looked like a passive caption.
    // The i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-quota-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryQuota).toHaveBeenCalledWith(account.id)
  })

  it.each(['cline', 'command_code'])('shows all persisted subscription windows for official %s accounts', async (platform) => {
    const wrapper = mount(CNProviderQuotaCell, {
      props: {
        account: {
          ...account,
          platform,
          credentials: { account_mode: 'payg' },
          extra: {
            [`${platform}_5h_used_percent`]: 15,
            [`${platform}_weekly_used_percent`]: 35,
            [`${platform}_monthly_used_percent`]: 65,
            [`${platform}_usage_updated_at`]: new Date().toISOString()
          }
        }
      }
    })
    await flushPromises()

    expect(wrapper.findAll('[data-test="cn-provider-quota-tier"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('15%')
    expect(wrapper.text()).toContain('35%')
    expect(wrapper.text()).toContain('65%')
    expect(queryQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['cline', 'command_code'])('hides %s relay windows and skips automatic probes', async (platform) => {
    const wrapper = mount(CNProviderQuotaCell, {
      props: {
        account: {
          ...account,
          platform,
          credentials: {
            api_protocol: 'adaptive',
            api_base_urls: { chat_completions: 'https://relay.example.com/v1' }
          },
          extra: { [`${platform}_weekly_used_percent`]: 35 }
        }
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-test="cn-provider-quota"]').exists()).toBe(false)
    expect(queryQuota).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('forwards the credits returned by a combined quota refresh', async () => {
    const balance = { provider: 'cline', success: true, balance: 8.25, currency: 'USD', fetched_at: 1, persisted: true }
    queryQuota.mockResolvedValue({ success: true, tiers: [], balance })
    const wrapper = mount(CNProviderQuotaCell, {
      props: {
        account: {
          ...account,
          platform: 'cline',
          credentials: {},
          extra: { cline_usage_updated_at: new Date().toISOString() }
        }
      }
    })

    await wrapper.get('[data-test="cn-provider-quota-probe"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('balance-updated')?.[0]).toEqual([balance])
    wrapper.unmount()
  })
})
