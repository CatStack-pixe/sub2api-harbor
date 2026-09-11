import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SenseNovaQuotaCell from '../SenseNovaQuotaCell.vue'
import type { Account } from '@/types'

const { queryQuota } = vi.hoisted(() => ({
  queryQuota: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    sensenova: { queryQuota }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const account = {
  id: 8,
  platform: 'sensenova',
  type: 'apikey',
  extra: {
    sensenova_quota_snapshot: {
      plan: { name: 'Free Plan' },
      pools: [
        {
          id: 'shared',
          name: 'Shared',
          window_5h: { limit: 100, used: 25, remaining: 75 },
          window_7d: { limit: 1000, used: 100, remaining: 900 }
        }
      ],
      fetched_at: new Date().toISOString()
    }
  }
} as Account

describe('SenseNovaQuotaCell', () => {
  beforeEach(() => {
    queryQuota.mockReset()
  })

  it('renders independent 5h and 7d pool windows and supports manual refresh', async () => {
    queryQuota.mockResolvedValue({
      success: true,
      pools: [
        {
          id: 'shared',
          name: 'Shared',
          window_5h: { limit: 100, used: 40, remaining: 60 },
          window_7d: { limit: 1000, used: 250, remaining: 750 }
        },
        {
          id: 'dedicated',
          name: 'Dedicated',
          window_7d: { limit: 500, used: 450, remaining: 50 }
        }
      ]
    })

    const wrapper = mount(SenseNovaQuotaCell, { props: { account } })
    await flushPromises()
    expect(wrapper.findAll('[data-test="sensenova-quota-tier"]')).toHaveLength(2)

    await wrapper.get('[data-test="sensenova-quota-probe"]').trigger('click')
    await flushPromises()

    expect(queryQuota).toHaveBeenCalledWith(account.id)
    expect(wrapper.findAll('[data-test="sensenova-quota-tier"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('40%')
    expect(wrapper.text()).toContain('90%')
    expect(wrapper.findAll('[data-test="sensenova-quota-pool"]')).toHaveLength(2)
  })
})
