import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { applyTierflowConsoleCredentials } from '../tierflowCredentials'
import { isHeaderOverrideCapable, isMultiProtocolApiKeyPlatform } from '../credentialsBuilder'
import { getModelsByPlatform, getPresetMappingsByPlatform } from '@/composables/useModelWhitelist'
import TierflowCredentialsFields from '../TierflowCredentialsFields.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('Tierflow optional console credentials', () => {
  it('trims both independent console credential fields without changing the API key', () => {
    const credentials: Record<string, unknown> = { api_key: 'test-api-key' }
    applyTierflowConsoleCredentials(credentials, ' session=test-cookie ', ' 12345 ')
    expect(credentials).toEqual({
      api_key: 'test-api-key',
      tierflow_cookie: 'session=test-cookie',
      tierflow_user_id: '12345'
    })
  })

  it('does not require console credentials for API-key-only creation', () => {
    const credentials: Record<string, unknown> = { api_key: 'test-api-key' }
    applyTierflowConsoleCredentials(credentials, ' ', '')
    expect(credentials).toEqual({ api_key: 'test-api-key' })
  })

  it('preserves existing console credentials when edit inputs are blank', () => {
    const credentials: Record<string, unknown> = {
      tierflow_cookie: 'existing-cookie',
      tierflow_user_id: '12345'
    }
    applyTierflowConsoleCredentials(credentials, '', ' ')
    expect(credentials.tierflow_cookie).toBe('existing-cookie')
    expect(credentials.tierflow_user_id).toBe('12345')
  })

  it('masks the optional Cookie and preserves the numeric user ID as a string', async () => {
    const wrapper = mount(TierflowCredentialsFields, {
      props: { cookie: '', userId: '', editing: true }
    })
    const cookie = wrapper.get('[data-testid="tierflow-cookie"]')
    const userId = wrapper.get('[data-testid="tierflow-user-id"]')
    expect(cookie.attributes('type')).toBe('password')
    expect(userId.attributes('type')).toBe('text')
    expect(userId.attributes('inputmode')).toBe('numeric')
    expect(cookie.attributes('required')).toBeUndefined()
    expect(userId.attributes('required')).toBeUndefined()
    expect(cookie.attributes('placeholder')).toBe('admin.accounts.leaveEmptyToKeep')
    await cookie.setValue('updated-cookie')
    await userId.setValue('67890')
    expect(wrapper.emitted('update:cookie')).toEqual([['updated-cookie']])
    expect(wrapper.emitted('update:userId')).toEqual([['67890']])
  })
})

describe('Tierflow API-key protocol capabilities', () => {
  it('does not invent model mappings or native multi-protocol support for the relay', () => {
    expect(getModelsByPlatform('tierflow')).toEqual([])
    expect(getPresetMappingsByPlatform('tierflow')).toEqual([])
    expect(isMultiProtocolApiKeyPlatform('tierflow')).toBe(false)
    expect(isHeaderOverrideCapable('tierflow', 'apikey')).toBe(true)
    expect(isHeaderOverrideCapable('tierflow', 'oauth')).toBe(false)
  })
})
