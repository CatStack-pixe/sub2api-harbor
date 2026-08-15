import { describe, expect, it } from 'vitest'

import {
  platformBadgeClass,
  platformIconClass,
  platformLabel,
} from '../platformColors'

describe('DeepSeek platform display', () => {
  it('has a distinct label and indigo visual treatment', () => {
    expect(platformLabel('deepseek')).toBe('DeepSeek')
    expect(platformBadgeClass('deepseek')).toContain('indigo')
    expect(platformIconClass('deepseek')).toContain('indigo')
    expect(platformBadgeClass('deepseek')).not.toBe(platformBadgeClass('unknown'))
  })
})

describe('Kimi platform display', () => {
  it('has a distinct label and sky visual treatment', () => {
    expect(platformLabel('kimi')).toBe('Kimi')
    expect(platformBadgeClass('kimi')).toContain('sky')
    expect(platformIconClass('kimi')).toContain('sky')
  })
})
