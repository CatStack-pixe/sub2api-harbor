import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { resolveSessionMock, showErrorMock, showSuccessMock } = vi.hoisted(() => ({
  resolveSessionMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      resolveTokenRhythmSession: resolveSessionMock
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => params?.code ? `${key}:${params.code}` : key
    })
  }
})

import TokenRhythmSessionResolver from '../TokenRhythmSessionResolver.vue'

describe('TokenRhythmSessionResolver', () => {
  beforeEach(() => {
    resolveSessionMock.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
  })

  it('resolves through the selected proxy and emits only the normalized Cookie', async () => {
    resolveSessionMock.mockResolvedValue({
      tokenrhythm_cookie: 'tr_session=session; tr_csrf=csrf',
      referral_code: 'invite-code',
      referral_link: 'https://tokenrhythm.studio/i/invite-code',
      eligible: true,
      public_enabled: true,
      registration_allowed: true
    })
    const wrapper = mount(TokenRhythmSessionResolver, { props: { proxyId: 18 } })

    await wrapper.get('[data-testid="tokenrhythm-session-input"]').setValue('sess_value')
    await wrapper.get('[data-testid="tokenrhythm-session-resolve"]').trigger('click')
    await flushPromises()

    expect(resolveSessionMock).toHaveBeenCalledWith({ sess: 'sess_value', proxy_id: 18 })
    expect(wrapper.emitted('resolved')).toEqual([['tr_session=session; tr_csrf=csrf']])
    expect(wrapper.get('[data-testid="tokenrhythm-referral-result"] input').attributes('value')).toBe(
      'https://tokenrhythm.studio/i/invite-code'
    )
    expect(wrapper.get('[data-testid="tokenrhythm-session-input"]').element).toHaveProperty('value', '')
    expect(showSuccessMock).toHaveBeenCalledTimes(1)
  })

  it('does not expose a result when the provider rejects the sess value', async () => {
    resolveSessionMock.mockRejectedValue({ response: { data: { message: 'invalid session' } } })
    const wrapper = mount(TokenRhythmSessionResolver)

    await wrapper.get('[data-testid="tokenrhythm-session-input"]').setValue('sess_invalid')
    await wrapper.get('[data-testid="tokenrhythm-session-resolve"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="tokenrhythm-referral-result"]').exists()).toBe(false)
    expect(showErrorMock).toHaveBeenCalledWith('invalid session')
  })
})
