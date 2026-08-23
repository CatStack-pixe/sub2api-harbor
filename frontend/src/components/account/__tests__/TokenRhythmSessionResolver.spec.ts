import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { resolveSessionMock, createKeyMock, listKeysMock, disableKeyMock, deleteKeyMock, showErrorMock, showSuccessMock, showWarningMock } = vi.hoisted(() => ({
  resolveSessionMock: vi.fn(),
  createKeyMock: vi.fn(),
  listKeysMock: vi.fn(),
  disableKeyMock: vi.fn(),
  deleteKeyMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showWarningMock: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      resolveTokenRhythmSession: resolveSessionMock,
      createTokenRhythmAPIKey: createKeyMock,
      listTokenRhythmAPIKeys: listKeysMock,
      disableTokenRhythmAPIKey: disableKeyMock,
      deleteTokenRhythmAPIKey: deleteKeyMock
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock,
    showWarning: showWarningMock
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
    createKeyMock.mockReset()
    listKeysMock.mockReset()
    disableKeyMock.mockReset()
    deleteKeyMock.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    showWarningMock.mockReset()
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

  it('creates a provider key and emits the generated credentials for the account form', async () => {
    createKeyMock.mockResolvedValue({
      api_key: 'sk_tr_created',
      tokenrhythm_cookie: 'tr_session=session; tr_csrf=csrf',
      name: 'sub2api-demo'
    })
    const wrapper = mount(TokenRhythmSessionResolver, {
      props: { proxyId: 18, accountName: 'demo' },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /></div>'
          }
        }
      }
    })

    await wrapper.get('[data-testid="tokenrhythm-manage-key"]').trigger('click')
    await wrapper.get('[data-testid="tokenrhythm-key-session-input"]').setValue('sess_value')
    await wrapper.get('[data-testid="tokenrhythm-key-name-input"]').setValue('sub2api-demo')
    await wrapper.get('[data-testid="tokenrhythm-key-form"]').trigger('submit')
    await flushPromises()

    expect(createKeyMock).toHaveBeenCalledWith({
      sess: 'sess_value',
      name: 'sub2api-demo',
      proxy_id: 18
    })
    expect(wrapper.emitted('apiKeyCreated')).toEqual([[
      {
        api_key: 'sk_tr_created',
        tokenrhythm_cookie: 'tr_session=session; tr_csrf=csrf',
        name: 'sub2api-demo'
      }
    ]])
    expect(wrapper.emitted('update:apiKey')).toEqual([['sk_tr_created']])
    expect(wrapper.emitted('resolved')).toEqual([['tr_session=session; tr_csrf=csrf']])
    expect(wrapper.get('[data-testid="tokenrhythm-key-session-input"]').element).toHaveProperty('value', '')
  })

  it('uses the saved account credential for inventory and key creation', async () => {
    listKeysMock.mockResolvedValue({
      keys: [{ id: 'key-1', name: 'primary', masked_key: 'sk_tr_***', status: 'active' }],
      tokenrhythm_cookie: 'tr_session=rotated; tr_csrf=csrf'
    })
    createKeyMock.mockResolvedValue({
      api_key: 'sk_tr_created',
      tokenrhythm_cookie: 'tr_session=rotated; tr_csrf=csrf',
      name: 'sub2api-demo'
    })
    const wrapper = mount(TokenRhythmSessionResolver, {
      props: { accountId: 42, proxyId: 18, accountName: 'demo' },
      global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } }
    })

    await wrapper.get('[data-testid="tokenrhythm-list-keys"]').trigger('click')
    await flushPromises()
    expect(listKeysMock).toHaveBeenCalledWith({ account_id: 42, sess: undefined, cookie: undefined, proxy_id: 18 })
    expect(wrapper.get('[data-testid="tokenrhythm-key-inventory"]').text()).toContain('sk_tr_***')

    await wrapper.get('[data-testid="tokenrhythm-manage-key"]').trigger('click')
    await wrapper.get('[data-testid="tokenrhythm-key-form"]').trigger('submit')
    await flushPromises()
    expect(createKeyMock).toHaveBeenCalledWith({
      sess: undefined,
      cookie: 'tr_session=rotated; tr_csrf=csrf',
      name: 'sub2api-demo',
      proxy_id: 18,
      account_id: undefined
    })
  })

  it('creates with a pasted Cookie and confirms disable/delete actions', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const cookie = 'tr_session=session; tr_csrf=csrf'
    listKeysMock.mockResolvedValue({
      keys: [{ id: 'key-1', name: 'primary', masked_key: 'sk_tr_***', status: 'active' }],
      tokenrhythm_cookie: cookie
    })
    createKeyMock.mockResolvedValue({ api_key: 'sk_tr_created', tokenrhythm_cookie: cookie, name: 'manual' })
    disableKeyMock.mockResolvedValue({ id: 'key-1', tokenrhythm_cookie: cookie })
    deleteKeyMock.mockResolvedValue({ id: 'key-1', tokenrhythm_cookie: cookie })
    const wrapper = mount(TokenRhythmSessionResolver, {
      props: { credentialCookie: cookie },
      global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } }
    })

    await wrapper.get('[data-testid="tokenrhythm-manage-key"]').trigger('click')
    await wrapper.get('[data-testid="tokenrhythm-key-name-input"]').setValue('manual')
    await wrapper.get('[data-testid="tokenrhythm-key-form"]').trigger('submit')
    await flushPromises()
    expect(createKeyMock).toHaveBeenCalledWith({ sess: undefined, cookie, name: 'manual', proxy_id: undefined, account_id: undefined })

    await wrapper.get('[data-testid="tokenrhythm-list-keys"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="tokenrhythm-disable-key-1"]').trigger('click')
    await flushPromises()
    expect(disableKeyMock).toHaveBeenCalledWith('key-1', { account_id: undefined, sess: undefined, cookie, proxy_id: undefined })

    await wrapper.get('[data-testid="tokenrhythm-delete-key-1"]').trigger('click')
    await flushPromises()
    expect(deleteKeyMock).toHaveBeenCalledWith('key-1', { account_id: undefined, sess: undefined, cookie, proxy_id: undefined })
    expect(confirmSpy).toHaveBeenCalledTimes(2)
    confirmSpy.mockRestore()
  })

  it('prefers a newly resolved Cookie over the saved account credential and surfaces persistence warnings', async () => {
    const cookie = 'tr_session=new-session; tr_csrf=new-csrf'
    listKeysMock.mockResolvedValue({
      keys: [],
      tokenrhythm_cookie: cookie,
      credential_persist_warning: 'persistence failed'
    })
    const wrapper = mount(TokenRhythmSessionResolver, {
      props: { accountId: 42, credentialCookie: cookie }
    })

    await wrapper.get('[data-testid="tokenrhythm-list-keys"]').trigger('click')
    await flushPromises()

    expect(listKeysMock).toHaveBeenCalledWith({
      account_id: undefined,
      sess: undefined,
      cookie,
      proxy_id: undefined
    })
    expect(wrapper.emitted('resolved')).toEqual([[cookie]])
    expect(showWarningMock).toHaveBeenCalledWith(
      'admin.accounts.tokenrhythm.credentialPersistWarning'
    )
  })
})
