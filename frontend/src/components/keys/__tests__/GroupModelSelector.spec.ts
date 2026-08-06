import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

import GroupModelSelector from '../GroupModelSelector.vue'

const models = ['gpt-5.6', 'gpt-5.5']

describe('GroupModelSelector', () => {
  it('only exposes group models as selectable options', async () => {
    const wrapper = mount(GroupModelSelector, {
      props: { modelValue: [], models },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.findAll('input[type="text"], input[type="search"]').length).toBe(0)
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(2)

    await wrapper.find('input[type="checkbox"]').setValue(true)

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6']]])
  })

  it('can select all models or clear the restriction', async () => {
    const wrapper = mount(GroupModelSelector, {
      props: { modelValue: [], models },
      global: { stubs: { Icon: true } }
    })

    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click')
    await wrapper.setProps({ modelValue: models })
    await buttons[1].trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([
      [models],
      [[]]
    ])
  })
})
