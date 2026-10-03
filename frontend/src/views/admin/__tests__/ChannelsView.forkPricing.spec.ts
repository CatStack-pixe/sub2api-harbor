import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel, ChannelModelPricing } from '@/api/admin/channels'
import ChannelsView from '../ChannelsView.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  update: vi.fn(),
  getAll: vi.fn(),
  getWebSearchEmulationConfig: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: mocks.list, update: mocks.update },
    groups: { getAll: mocks.getAll },
    settings: { getWebSearchEmulationConfig: mocks.getWebSearchEmulationConfig },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }),
}))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const platforms = ['deepseek', 'minimax', 'agnes', 'nvidia', 'tokenrhythm', 'tierflow', 'senseaudio', 'sensenova', 'opencode_go', 'typesafe'] as const

function createPricing(platform: string): ChannelModelPricing {
  return {
    platform,
    models: [`${platform}-model`],
    billing_mode: 'token',
    input_price: 1e-6,
    output_price: 2e-6,
    cache_write_price: 0,
    cache_write_1h_price: 3e-6,
    cache_read_price: null,
    fast_multiplier: 2,
    flex_multiplier: 0.5,
    reasoning_effort_multipliers: { high: 1.5, max: 3 },
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_windows: [{
      start_time: '22:00',
      end_time: '00:00',
      input_price: 4e-6,
      output_price: 0,
      cache_write_price: null,
      cache_read_price: 0.25e-6,
      sort_order: 2,
    }],
    time_pricing: {
      timezone: 'Asia/Shanghai',
      weekdays_only: true,
      periods: [{ start_time: '09:00:00', end_time: '12:00:00', multiplier: 1.5 }],
    },
  }
}

function createStatsPricing(platform: string): ChannelModelPricing {
  const pricing = createPricing(platform)
  // Service-tier multipliers and time-pricing periods apply to channel billing,
  // not the independent account statistics pricing rules.
  delete pricing.fast_multiplier
  delete pricing.flex_multiplier
  pricing.time_pricing = null
  return pricing
}

function createChannel(): Channel {
  return {
    id: 7,
    name: 'Fork providers',
    description: 'Keep custom pricing',
    status: 'active',
    restrict_models: true,
    billing_model_source: 'upstream',
    group_ids: platforms.map((_platform, index) => index + 1),
    model_mapping: Object.fromEntries(platforms.map(platform => [platform, { alias: `${platform}-model` }])),
    model_pricing: platforms.map(createPricing),
    features_config: { fork_custom_feature: { enabled: true } },
    apply_pricing_to_account_stats: true,
    account_stats_pricing_rules: platforms.map((platform, index) => ({
      name: `${platform} stats`,
      group_ids: [index + 1],
      account_ids: [],
      pricing: [createStatsPricing(platform)],
    })),
    created_at: '2026-09-30T00:00:00Z',
    updated_at: '2026-09-30T00:00:00Z',
  }
}

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
        }),
        DataTable: defineComponent({
          props: ['data'],
          emits: ['sort'],
          template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>',
        }),
        BaseDialog: defineComponent({
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>',
        }),
        PricingEntryCard: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        Toggle: true,
      },
    },
  })
}

async function openEdit() {
  const wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
  await flushPromises()
  return wrapper
}

describe('ChannelsView fork pricing preservation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    const channel = createChannel()
    mocks.list.mockResolvedValue({ items: [channel], total: 1 })
    mocks.update.mockResolvedValue(channel)
    mocks.getAll.mockResolvedValue(platforms.map((platform, index) => ({
      id: index + 1, platform, name: `${platform} group`,
    })))
    mocks.getWebSearchEmulationConfig.mockResolvedValue({ enabled: false, providers: [] })
  })

  it('round-trips custom providers, model mappings, time windows, and reasoning multipliers', async () => {
    const original = createChannel()
    const wrapper = await openEdit()
    await wrapper.get('#channel-form').trigger('submit')
    await flushPromises()

    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(mocks.update).toHaveBeenCalledWith(7, expect.objectContaining({
      group_ids: expect.arrayContaining(original.group_ids),
      model_mapping: original.model_mapping,
      model_pricing: expect.arrayContaining(original.model_pricing),
      account_stats_pricing_rules: expect.arrayContaining(original.account_stats_pricing_rules),
      features_config: expect.objectContaining(original.features_config),
    }))
    const payload = mocks.update.mock.calls[0][1]
    expect(payload.model_pricing).toHaveLength(platforms.length)
    expect(payload.account_stats_pricing_rules).toHaveLength(platforms.length)
    wrapper.unmount()
  })

  it.each(['channel', 'account statistics'])('validates reasoning multipliers for %s pricing before saving', async (scope) => {
    const channel = createChannel()
    const pricing = scope === 'channel'
      ? channel.model_pricing[0]
      : channel.account_stats_pricing_rules![0].pricing[0]
    pricing.reasoning_effort_multipliers = { high: 0 }
    mocks.list.mockResolvedValue({ items: [channel], total: 1 })
    const wrapper = await openEdit()
    await wrapper.get('#channel-form').trigger('submit')
    await flushPromises()
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith(expect.stringContaining('reasoningEffortMultiplierPositive'))
    wrapper.unmount()
  })
})
