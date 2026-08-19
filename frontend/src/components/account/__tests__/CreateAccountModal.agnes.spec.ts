import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('CreateAccountModal Agnes account type', () => {
  it('offers API-key setup with the documented Agnes defaults', () => {
    expect(source).toContain('data-testid="agnes-platform"')
    expect(source).toContain('data-testid="agnes-account-type-api-key"')
    expect(source).toContain("newPlatform === 'agnes'")
    expect(source).toContain("? 'https://apihub.agnes-ai.com/v1'")
    expect(source).toContain("allowedModels.value = ['agnes-2.0-flash']")
  })
})

describe('CreateAccountModal DeepSeek account type', () => {
  it('offers API-key setup with the official DeepSeek defaults and no shared probe', () => {
    expect(source).toContain('data-testid="deepseek-platform"')
    expect(source).toContain('data-testid="deepseek-account-type-api-key"')
    expect(source).toContain("newPlatform === 'deepseek'")
    expect(source).toContain("? 'https://api.deepseek.com'")
    expect(source).toContain('allowedModels.value = [...getModelsByPlatform(newPlatform)]')
    expect(source).toContain("form.platform === 'deepseek' || form.platform === 'kimi' || form.platform === 'chatanywhere'")
  })
})

describe('CreateAccountModal TokenRhythm account type', () => {
  it('uses a fixed API endpoint and requires a Cookie for balance queries', () => {
    expect(source).toContain('data-testid="tokenrhythm-platform"')
    expect(source).toContain('data-testid="tokenrhythm-account-type-api-key"')
    expect(source).toContain("newPlatform === 'tokenrhythm'")
    expect(source).toContain("'https://tokenrhythm.studio/v1'")
    expect(source).toContain('credentials.tokenrhythm_cookie = tokenRhythmCookie.value.trim()')
  })
})

describe('CreateAccountModal Kimi account type', () => {
  it('offers API-key setup with the supported official regions', () => {
    expect(source).toContain('data-testid="kimi-platform"')
    expect(source).toContain('data-testid="kimi-account-type-api-key"')
    expect(source).toContain("newPlatform === 'kimi'")
    expect(source).toContain('data-testid="kimi-base-url"')
    expect(source).toContain('https://api.moonshot.cn/v1')
    expect(source).toContain('https://api.moonshot.ai/v1')
  })
})

describe('CreateAccountModal ChatAnywhere account type', () => {
  it('offers API-key setup with official regional endpoints and no billing probe', () => {
    expect(source).toContain('data-testid="chatanywhere-platform"')
    expect(source).toContain('data-testid="chatanywhere-account-type-api-key"')
    expect(source).toContain("newPlatform === 'chatanywhere'")
    expect(source).toContain('https://api.chatanywhere.tech/v1')
    expect(source).toContain('https://api.chatanywhere.org/v1')
    expect(source).toContain("form.platform !== 'tokenrhythm' && form.platform !== 'chatanywhere'")
  })
})

describe('CreateAccountModal GLM account type', () => {
  it('offers API-key setup with the fixed official Z.AI endpoint', () => {
    expect(source).toContain('data-testid="glm-platform"')
    expect(source).toContain('data-testid="glm-account-type-api-key"')
    expect(source).toContain("newPlatform === 'glm'")
    expect(source).toContain('https://open.bigmodel.cn/api/paas/v4')
    expect(source).toContain("form.platform !== 'chatanywhere' && form.platform !== 'glm'")
  })
})
