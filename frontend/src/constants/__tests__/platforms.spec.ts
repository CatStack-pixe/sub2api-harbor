import { describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import { platformAccentColor, platformLabel } from '@/utils/platformColors'
import { getKeyGroupProvider } from '@/utils/keyGroupProviders'

const concretePlatforms = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'agnes',
  'deepseek',
  'nvidia',
  'tokenrhythm',
  'tierflow',
  'kimi',
  'zhipu',
  'chatanywhere',
  'glm',
  'modelscope',
  'dashscope',
  'minimax',
  'volcengine',
  'sensenova',
  'opencode_go'
]

describe('platform option catalogs', () => {
  it('labels and styles Tierflow as its own relay platform', () => {
    expect(platformLabel('tierflow')).toBe('Tierflow / 清枢智汇')
    expect(platformAccentColor('tierflow')).toBe('#0891b2')
    expect(getKeyGroupProvider('tierflow')).toBe('other')
  })

  it('exposes every concrete account platform', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(concretePlatforms)
  })

  it('adds composite for group-backed filters', () => {
    expect(GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms,
      'composite'
    ])
  })
})
