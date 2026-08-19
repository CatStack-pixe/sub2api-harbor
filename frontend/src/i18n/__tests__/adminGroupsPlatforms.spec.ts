import { describe, expect, it } from 'vitest'

import enAdmin from '../locales/en/admin/overview'
import zhAdmin from '../locales/zh/admin/overview'

describe('admin group platform translations', () => {
  it('provides a label for every platform rendered by the groups view', () => {
    const platforms = [
      'anthropic',
      'openai',
      'gemini',
      'antigravity',
      'grok',
      'agnes',
      'deepseek',
      'kimi',
      'nvidia',
      'tokenrhythm',
      'chatanywhere',
      'glm',
      'composite',
    ] as const

    for (const platform of platforms) {
      expect(zhAdmin.groups.platforms[platform]).toBeTruthy()
      expect(enAdmin.groups.platforms[platform]).toBeTruthy()
    }
  })

  it('labels TokenRhythm instead of exposing its i18n key', () => {
    expect(zhAdmin.groups.platforms.tokenrhythm).toBe('TokenRhythm')
    expect(enAdmin.groups.platforms.tokenrhythm).toBe('TokenRhythm')
  })
})
