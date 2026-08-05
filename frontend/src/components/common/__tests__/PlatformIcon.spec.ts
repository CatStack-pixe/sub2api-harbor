import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import PlatformIcon from '../PlatformIcon.vue'

describe('PlatformIcon branded marks', () => {
  it.each([
    ['agnes', 'M12 0c6.627'],
    ['deepseek', 'M23.748 4.482'],
    ['nvidia', 'M10.212 8.976']
  ] as const)('renders the official %s mark', (platform, pathPrefix) => {
    const wrapper = mount(PlatformIcon, { props: { platform } })
    const svg = wrapper.find('svg')

    expect(svg.attributes('fill')).toBe('currentColor')
    expect(svg.attributes('fill-rule')).toBe('evenodd')
    expect(svg.find('path').attributes('d')).toMatch(new RegExp(`^${pathPrefix.replace('.', '\\.')}`))
  })
})
