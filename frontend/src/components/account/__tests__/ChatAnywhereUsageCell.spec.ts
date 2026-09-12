import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ChatAnywhereUsageCell from '../ChatAnywhereUsageCell.vue'
import type { Account, AccountUsageInfo } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const account = (overrides: Partial<Account> = {}) =>
  ({
    id: 7004,
    name: 'chatanywhere',
    platform: 'chatanywhere',
    type: 'apikey',
    status: 'active',
    error_message: null,
    ...overrides
  }) as Account

const usage = (overrides: Partial<AccountUsageInfo> = {}) =>
  ({
    source: 'local',
    updated_at: null,
    five_hour: null,
    seven_day: null,
    seven_day_sonnet: null,
    chatanywhere_weekly: {
      utilization: 25,
      resets_at: null,
      remaining_seconds: 0,
      used_tokens: 12500,
      limit_tokens: 50000
    },
    chatanywhere_daily: {
      utilization: 17,
      resets_at: null,
      remaining_seconds: 0,
      used_requests: 17,
      limit_requests: 100
    },
    ...overrides
  }) as AccountUsageInfo

describe('ChatAnywhereUsageCell', () => {
  it('renders the 7d token and 1d request windows with explicit values', () => {
    const wrapper = mount(ChatAnywhereUsageCell, {
      props: {
        account: account(),
        usageInfo: usage()
      }
    })

    expect(wrapper.text()).toContain('12,500 / 50,000')
    expect(wrapper.text()).toContain('17 / 100')
    expect(wrapper.text()).toContain('25%')
    expect(wrapper.text()).toContain('17%')
  })

  it('keeps the observed request count visible when it exceeds 100', () => {
    const wrapper = mount(ChatAnywhereUsageCell, {
      props: {
        account: account(),
        usageInfo: usage({
          chatanywhere_daily: {
            utilization: 101,
            resets_at: null,
            remaining_seconds: 0,
            used_requests: 101,
            limit_requests: 100
          }
        })
      }
    })

    expect(wrapper.text()).toContain('101 / 100')
    expect(wrapper.text()).toContain('101%')
  })

  it('shows the provider point exhaustion state separately from local usage', () => {
    const wrapper = mount(ChatAnywhereUsageCell, {
      props: {
        account: account({
          status: 'error',
          error_message: 'HTTP 403: free points are insufficient'
        }),
        usageInfo: usage({ chatanywhere_quota_status: 'weekly_exhausted' })
      }
    })

    expect(wrapper.get('[data-test="chatanywhere-weekly-exhausted"]').text()).toBe(
      'admin.accounts.usageWindow.chatAnywhereWeeklyExhausted'
    )
    expect(wrapper.get('[data-test="chatanywhere-weekly-exhausted"]').attributes('title')).toContain(
      'free points are insufficient'
    )
  })
})
