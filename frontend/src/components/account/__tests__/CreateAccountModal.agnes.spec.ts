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
    expect(source).toContain("form.platform === 'deepseek' ? undefined")
  })
})
