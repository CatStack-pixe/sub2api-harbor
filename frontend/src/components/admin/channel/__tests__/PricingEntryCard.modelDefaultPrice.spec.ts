import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../types'
import channelsAPI from '@/api/admin/channels'

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const getModelDefaultPricing = vi.mocked(channelsAPI.getModelDefaultPricing)
enableAutoUnmount(afterEach)

function createEntry(models: string[] = []): PricingFormEntry {
  return {
    models,
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_windows: [],
    time_pricing: {
      timezone: 'Asia/Shanghai',
      weekdays_only: false,
      periods: [],
    },
  }
}

function mountCard(entry = createEntry()) {
  const wrapper = shallowMount(PricingEntryCard, {
    props: {
      entry,
      platform: 'anthropic',
      onUpdate: (updated: PricingFormEntry) => { void wrapper.setProps({ entry: updated }) }
    }
  })
  return wrapper
}

function addModels(entry: PricingFormEntry, models: string[]) {
  const wrapper = mountCard(entry)
  wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', models)
  return wrapper
}

function deferredPricing() {
  let resolve!: (value: Awaited<ReturnType<typeof channelsAPI.getModelDefaultPricing>>) => void
  const promise = new Promise<Awaited<ReturnType<typeof channelsAPI.getModelDefaultPricing>>>(resolvePromise => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

async function changeModels(wrapper: ReturnType<typeof mountCard>, models: string[]) {
  wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', models)
  await flushPromises()
}

const emittedEntries = (wrapper: ReturnType<typeof shallowMount>) =>
  (wrapper.emitted('update') ?? []).map(e => e[0] as PricingFormEntry)

describe('PricingEntryCard default price auto-fill', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getModelDefaultPricing.mockResolvedValue({ found: true, input_price: 3e-6, output_price: 15e-6 })
  })

  it('auto-fills the default price when a model is added to an empty entry', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).toHaveBeenCalledTimes(1)
    expect(getModelDefaultPricing).toHaveBeenCalledWith('claude-opus-4-8')
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-8'],
      input_price: 3,
      output_price: 15,
    })
  })

  it('does not overwrite prices when appending a model to an entry that already has one', async () => {
    const entry = createEntry(['claude-opus-4-7'])
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
    })
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBeNull()
      expect(emitted.output_price).toBeNull()
      expect(emitted.cache_write_price).toBeNull()
      expect(emitted.cache_read_price).toBeNull()
    }
  })

  it('does not auto-fill a shared price when several models are added to an empty entry at once', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
      input_price: null,
      output_price: null,
    })
  })

  it('keeps manual prices untouched when a model is appended', async () => {
    const entry = { ...createEntry(['claude-opus-4-7']), input_price: '1.5' }
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBe('1.5')
    }
  })

  it.each([
    'input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
    'cache_read_price', 'image_input_price', 'image_output_price'
  ])('preserves %s edited while its default price lookup is pending', async field => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValueOnce(pending.promise)
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()
    const inputs = wrapper.findAll('.pricing-default-grid input')
    const fields = [
      'input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price',
      'cache_read_price', 'image_input_price', 'image_output_price'
    ]
    await inputs[fields.indexOf(field)].setValue('7.5')
    const emissionsBeforeResponse = emittedEntries(wrapper).length
    pending.resolve({ found: true, input_price: 3e-6, output_price: 15e-6, image_input_price: 1e-6 })
    await flushPromises()

    expect(emittedEntries(wrapper)).toHaveLength(emissionsBeforeResponse)
    expect(wrapper.props('entry')).toMatchObject({ models: ['claude-opus-4-8'], [field]: '7.5' })
  })

  it('does not restore defaults after a price is typed and then cleared', async () => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValueOnce(pending.promise)
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()
    await wrapper.get('.pricing-default-grid input').setValue('7.5')
    await wrapper.get('.pricing-default-grid input').setValue('')
    const emissionsBeforeResponse = emittedEntries(wrapper).length
    pending.resolve({ found: true, input_price: 3e-6 })
    await flushPromises()

    expect(emittedEntries(wrapper)).toHaveLength(emissionsBeforeResponse)
    expect(wrapper.props('entry').input_price).toBeNull()
  })

  it.each([
    { models: [] },
    { models: ['replacement-model'] },
    { models: ['claude-opus-4-8', 'another-model'] }
  ])(
    'does not restore the lookup models after they change to $models',
    async ({ models }) => {
      const pending = deferredPricing()
      getModelDefaultPricing.mockReturnValueOnce(pending.promise)
      const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
      await flushPromises()
      await changeModels(wrapper, models)
      const emissionsBeforeResponse = emittedEntries(wrapper).length
      pending.resolve({ found: true, input_price: 3e-6 })
      await flushPromises()

      expect(emittedEntries(wrapper)).toHaveLength(emissionsBeforeResponse)
      expect(wrapper.props('entry').models).toEqual(models)
      expect(wrapper.props('entry').input_price).toBeNull()
    }
  )

  it('lets a replacement lookup finish without accepting the older response', async () => {
    const old = deferredPricing()
    const current = deferredPricing()
    getModelDefaultPricing.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = addModels(createEntry(), ['old-model'])
    await changeModels(wrapper, [])
    await changeModels(wrapper, ['current-model'])
    const emissionsBeforeResponse = emittedEntries(wrapper).length
    old.resolve({ found: true, input_price: 9e-6 })
    await flushPromises()
    expect(emittedEntries(wrapper)).toHaveLength(emissionsBeforeResponse)
    current.resolve({ found: true, input_price: 2e-6 })
    await flushPromises()

    expect(wrapper.props('entry')).toMatchObject({ models: ['current-model'], input_price: 2 })
  })

  it.each(['platform', 'billing mode'])('ignores results after the %s changes away and back', async field => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValueOnce(pending.promise)
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()
    if (field === 'platform') {
      await wrapper.setProps({ platform: 'openai' })
      await wrapper.setProps({ platform: 'anthropic' })
    } else {
      await wrapper.setProps({ entry: { ...wrapper.props('entry'), billing_mode: 'image' } })
      await wrapper.setProps({ entry: { ...wrapper.props('entry'), billing_mode: 'token' } })
    }
    const emissionsBeforeResponse = emittedEntries(wrapper).length
    pending.resolve({ found: true, input_price: 3e-6 })
    await flushPromises()

    expect(emittedEntries(wrapper)).toHaveLength(emissionsBeforeResponse)
  })

  it('ignores a response after the pricing card unmounts', async () => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValueOnce(pending.promise)
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()
    const update = vi.fn()
    await wrapper.setProps({ onUpdate: update })
    wrapper.unmount()
    pending.resolve({ found: true, input_price: 3e-6 })
    await flushPromises()

    expect(update).not.toHaveBeenCalled()
  })

  it('keeps reasoning, tier and time settings while a valid lookup fills cache and image defaults', async () => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValueOnce(pending.promise)
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()
    await wrapper.setProps({
      entry: {
        ...wrapper.props('entry'),
        reasoning_effort_multipliers: { high: '1.7' },
        fast_multiplier: '1.2',
        time_pricing: { timezone: 'UTC', weekdays_only: true, periods: [] }
      }
    })
    pending.resolve({
      found: true, input_price: 3e-6, cache_write_1h_price: 5e-6,
      image_input_price: 1e-6, image_output_price: 2e-6,
      reasoning_effort_multipliers: { high: 1.5 }
    })
    await flushPromises()

    expect(wrapper.props('entry')).toMatchObject({
      models: ['claude-opus-4-8'],
      input_price: 3, cache_write_1h_price: 5, image_input_price: 1, image_output_price: 2,
      reasoning_effort_multipliers: { high: '1.7' }, fast_multiplier: '1.2',
      time_pricing: { timezone: 'UTC', weekdays_only: true }
    })
  })
})
