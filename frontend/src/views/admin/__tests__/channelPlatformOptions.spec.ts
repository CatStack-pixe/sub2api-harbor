import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { listPlatformIds } from '@/constants/platformCatalog'

describe('Composite channel platform options', () => {
  it('includes the CN concrete providers for pricing and model mapping', () => {
    const source = readFileSync(resolve('src/views/admin/ChannelsView.vue'), 'utf8')
    expect(source).toContain('const platformOrder = computed<GroupPlatform[]>(() => listPlatformIds())')
    expect(source).toContain('const compositePlatforms = platformOrder')
    const platforms = listPlatformIds()

    expect(platforms).toContain('kimi')
    expect(platforms).toContain('zhipu')
    expect(platforms).toContain('deepseek')
    expect(platforms).toContain('minimax')
    expect(platforms).toContain('opencode_go')
    for (const platform of ['agnes', 'nvidia', 'tokenrhythm', 'tierflow', 'senseaudio', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'volcengine', 'sensenova', 'typesafe', 'command_code', 'cline']) {
      expect(platforms).toContain(platform)
    }
  })
})
