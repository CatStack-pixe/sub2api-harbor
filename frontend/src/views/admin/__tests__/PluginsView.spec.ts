import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import PluginsView from '../PluginsView.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { PluginUISession } from '@/api/admin'

enableAutoUnmount(afterEach)

const {
  listPlugins,
  uploadPlugin,
  enablePlugin,
  savePluginConfig,
  getPluginConfig,
  testPlugin,
  pluginStatus,
  createUISession,
  stepUpRun,
  showError,
  showSuccess,
  showInfo,
} = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  uploadPlugin: vi.fn(),
  enablePlugin: vi.fn(),
  savePluginConfig: vi.fn(),
  getPluginConfig: vi.fn(),
  testPlugin: vi.fn(),
  pluginStatus: vi.fn(),
  createUISession: vi.fn(),
  stepUpRun: vi.fn((action: () => Promise<unknown>) => action()),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      upload: uploadPlugin,
      enable: enablePlugin,
      disable: vi.fn(),
      remove: vi.fn(),
      getConfig: getPluginConfig,
      saveConfig: savePluginConfig,
      test: testPlugin,
      status: pluginStatus,
      createUISession,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo,
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const plugin = {
  id: 7,
  plugin_key: 'local.test.transport',
  name: 'Test Transport',
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: 'local.test.transport',
    name: 'Test Transport',
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1,
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' },
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [
    {
      id: 1,
      plugin_id: 7,
      capability: 'openai.oauth.outbound_transport.v1',
      platform: 'openai',
      account_type: 'oauth',
      enabled: false,
      rollout_percent: 100,
    },
  ],
  compatibility: {
    compatible: true,
    tested: true,
    status: 'compatible' as const,
    message: '',
    current_sub2api_version: '0.1.0',
    required_sub2api_version: '>=0.1.0',
    recommended_sub2api_version: '0.1.0',
    plugin_protocol: 1,
    transport_api: 1,
    ui_bridge: 1,
  },
  runtime_healthy: false,
  runtime_message: '',
}

function mountView() {
  return mount(PluginsView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        Icon: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

function deferred<T = Record<string, unknown>>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function openPlugin(wrapper: ReturnType<typeof mountView>, index = 0) {
  await wrapper.findAll('button').filter(button => button.text() === 'admin.plugins.configure')[index].trigger('click')
  await flushPromises()
  await wrapper.get('iframe').trigger('load')
  const frame = (wrapper.get('iframe').element as HTMLIFrameElement).contentWindow!
  return { frame, postMessage: vi.spyOn(frame, 'postMessage') }
}

function bridgeMessage(frame: Window, type: string, requestID = 'request-1', config?: Record<string, unknown>) {
  const data = { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type, request_id: requestID, config }
  window.dispatchEvent(new MessageEvent('message', { source: frame, origin: 'null', data }))
  return data
}

const bridgeOperations = [
  { type: 'config.load', api: getPluginConfig, result: { enabled: true } },
  { type: 'config.save', api: savePluginConfig, result: { enabled: true } },
  { type: 'config.test', api: testPlugin, result: { success: false, message: 'test failure', latency_ms: 1 } },
  { type: 'plugin.status', api: pluginStatus, result: { healthy: true } },
]

describe('管理员插件页二次验证', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    listPlugins.mockResolvedValue([plugin])
    uploadPlugin.mockResolvedValue(plugin)
    enablePlugin.mockResolvedValue(plugin)
    savePluginConfig.mockResolvedValue({ enabled: true })
    getPluginConfig.mockResolvedValue({})
    testPlugin.mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 })
    pluginStatus.mockResolvedValue({ healthy: true })
    createUISession.mockResolvedValue({
      url: '/api/v1/plugin-ui/token/index.html#bridge_token=bridge',
      bridge_token: 'bridge',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z',
    })
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('启用插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.enable'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(enablePlugin).toHaveBeenCalledWith(7, 100, false)
  })

  it.each([7, 8])('keeps the latest session when reopening configuration for plugin %s', async (id) => {
    let resolveOld!: (session: PluginUISession) => void
    createUISession.mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveOld = resolve }))
    listPlugins.mockResolvedValue([plugin, { ...plugin, id: 8, name: 'Other Transport' }])
    const wrapper = mountView()
    await flushPromises()
    const buttons = wrapper.findAll('button').filter(item => item.text() === 'admin.plugins.configure')
    await buttons[0].trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await buttons[id === 7 ? 0 : 1].trigger('click')
    await flushPromises()
    const currentURL = wrapper.get('iframe').attributes('src')

    resolveOld({ url: '/obsolete-session', bridge_token: 'old', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toBe(currentURL)
  })

  it('ignores an old session error while the reopened configuration is loading', async () => {
    let rejectOld!: (error: Error) => void
    let resolveCurrent!: (session: PluginUISession) => void
    createUISession
      .mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
      .mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveCurrent = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const button = wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!
    await button.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await button.trigger('click')
    rejectOld(new Error('obsolete failure'))
    await flushPromises()

    expect(wrapper.text()).not.toContain('obsolete failure')
    expect(wrapper.text()).toContain('admin.plugins.loadingUI')
    resolveCurrent({ url: '/current-session', bridge_token: 'current', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    await wrapper.get('iframe').trigger('load')
    expect(wrapper.get('iframe').attributes('src')).toBe('/current-session')
    expect(wrapper.text()).not.toContain('admin.plugins.loadingUI')
  })

  it('does not restore a session after the configuration dialog closes', async () => {
    let resolveSession!: (session: PluginUISession) => void
    createUISession.mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveSession = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    resolveSession({ url: '/closed-session', bridge_token: 'old', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
  })

  it('上传插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: [new File(['plugin'], 'transport.s2plugin', { type: 'application/zip' })],
    })

    await input.trigger('change')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(uploadPlugin).toHaveBeenCalledTimes(1)
  })

  it.each(bridgeOperations)('ignores old $type responses when a new plugin reuses the request ID', async ({ type, api, result }) => {
    const old = deferred()
    const current = deferred()
    api.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    listPlugins.mockResolvedValue([plugin, { ...plugin, id: 8, name: 'Other Transport' }])
    const wrapper = mountView()
    await flushPromises()
    const original = await openPlugin(wrapper)
    bridgeMessage(original.frame, type, 'reused-id', { enabled: true })
    await flushPromises()
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    const replacement = await openPlugin(wrapper, 1)
    bridgeMessage(replacement.frame, type, 'reused-id', { enabled: false })
    await flushPromises()

    old.resolve(result)
    await flushPromises()
    expect(original.postMessage).not.toHaveBeenCalled()
    expect(replacement.postMessage).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(api.mock.calls.map(([id]) => id)).toEqual([7, 8])

    current.resolve(result)
    await flushPromises()
    expect(replacement.postMessage).toHaveBeenCalledOnce()
    expect(replacement.postMessage).toHaveBeenCalledWith(expect.objectContaining({
      type: `${type}.result`, request_id: 'reused-id', bridge_token: 'bridge',
      ok: type !== 'config.test'
    }), '*')
    if (type === 'config.save') expect(showSuccess).toHaveBeenCalledOnce()
    if (type === 'config.test') expect(showError).toHaveBeenCalledWith('test failure')
  })

  it.each(bridgeOperations)('ignores old $type errors without consuming the reopened session request', async ({ type, api, result }) => {
    const old = deferred()
    const current = deferred()
    api.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = mountView()
    await flushPromises()
    const original = await openPlugin(wrapper)
    bridgeMessage(original.frame, type, 'reused-id', { enabled: true })
    await flushPromises()
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    const replacement = await openPlugin(wrapper)
    bridgeMessage(replacement.frame, type, 'reused-id', { enabled: true })
    await flushPromises()
    old.reject(new Error('obsolete failure'))
    await flushPromises()

    expect(replacement.postMessage).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    current.resolve(result)
    await flushPromises()
    expect(replacement.postMessage).toHaveBeenCalledOnce()
  })

  it('does not leak a config response after iframe navigation reuses its request ID', async () => {
    const old = deferred()
    const current = deferred()
    getPluginConfig.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = mountView()
    await flushPromises()
    const { frame, postMessage } = await openPlugin(wrapper)
    bridgeMessage(frame, 'config.load', 'reused-id')
    await flushPromises()
    await wrapper.get('iframe').trigger('load')
    bridgeMessage(frame, 'config.load', 'reused-id')
    await flushPromises()

    old.resolve({ secret: 'obsolete-config' })
    await flushPromises()
    expect(postMessage).not.toHaveBeenCalled()
    current.resolve({ setting: 'current-document' })
    await flushPromises()
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      config: { setting: 'current-document' }
    }), '*')
  })

  it('does not let an expired result consume a fresh request with the same ID', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const old = deferred()
    const current = deferred()
    getPluginConfig.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = mountView()
    await flushPromises()
    const { frame, postMessage } = await openPlugin(wrapper)
    bridgeMessage(frame, 'config.load', 'reused-id')
    await flushPromises()
    vi.advanceTimersByTime(30_000)
    bridgeMessage(frame, 'config.load', 'reused-id')
    await flushPromises()
    old.resolve({ secret: 'expired-config' })
    await flushPromises()
    expect(postMessage).not.toHaveBeenCalled()

    current.resolve({ setting: 'current-request' })
    await flushPromises()
    expect(postMessage).toHaveBeenCalledOnce()
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      config: { setting: 'current-request' }
    }), '*')
  })

  it.each(['config.save', 'config.test'])('does not execute delayed %s step-up actions after switching plugins', async type => {
    const grant = deferred<void>()
    stepUpRun.mockImplementation(async action => {
      await grant.promise
      return action()
    })
    listPlugins.mockResolvedValue([plugin, { ...plugin, id: 8, name: 'Other Transport' }])
    const wrapper = mountView()
    await flushPromises()
    const original = await openPlugin(wrapper)
    bridgeMessage(original.frame, type, 'step-up', { enabled: true })
    await flushPromises()
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    const replacement = await openPlugin(wrapper, 1)
    grant.resolve()
    await flushPromises()

    expect(savePluginConfig).not.toHaveBeenCalled()
    expect(testPlugin).not.toHaveBeenCalled()
    expect(original.postMessage).not.toHaveBeenCalled()
    expect(replacement.postMessage).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it('does not retarget a save retry to the reopened plugin after step-up verification', async () => {
    const grant = deferred<void>()
    savePluginConfig.mockRejectedValueOnce(new Error('STEP_UP_REQUIRED'))
    stepUpRun.mockImplementation(async action => {
      try { return await action() } catch {
        await grant.promise
        return action()
      }
    })
    listPlugins.mockResolvedValue([plugin, { ...plugin, id: 8, name: 'Other Transport' }])
    const wrapper = mountView()
    await flushPromises()
    const original = await openPlugin(wrapper)
    bridgeMessage(original.frame, 'config.save', 'step-up', { enabled: true })
    await flushPromises()
    expect(savePluginConfig).toHaveBeenCalledWith(7, { enabled: true })
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    const replacement = await openPlugin(wrapper, 1)
    grant.resolve()
    await flushPromises()

    expect(savePluginConfig).toHaveBeenCalledOnce()
    expect(replacement.postMessage).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it.each(['navigation', 'timeout', 'unmount'])('cancels a pending save when its owner is invalidated by %s', async reason => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const grant = deferred<void>()
    stepUpRun.mockImplementation(async action => {
      await grant.promise
      return action()
    })
    const wrapper = mountView()
    await flushPromises()
    const { frame, postMessage } = await openPlugin(wrapper)
    bridgeMessage(frame, 'config.save', 'step-up', { enabled: true })
    await flushPromises()
    if (reason === 'navigation') await wrapper.get('iframe').trigger('load')
    else if (reason === 'timeout') vi.advanceTimersByTime(30_000)
    else wrapper.unmount()
    grant.resolve()
    await flushPromises()

    expect(savePluginConfig).not.toHaveBeenCalled()
    expect(postMessage).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('preserves the original config and plugin ID on a current-session step-up retry', async () => {
    const grant = deferred<void>()
    savePluginConfig.mockRejectedValueOnce(new Error('STEP_UP_REQUIRED'))
    stepUpRun.mockImplementation(async action => {
      try { return await action() } catch {
        await grant.promise
        return action()
      }
    })
    const wrapper = mountView()
    await flushPromises()
    const { frame, postMessage } = await openPlugin(wrapper)
    const message = bridgeMessage(frame, 'config.save', 'step-up', { enabled: true })
    await flushPromises()
    message.config!.enabled = false
    grant.resolve()
    await flushPromises()

    expect(savePluginConfig.mock.calls).toEqual([[7, { enabled: true }], [7, { enabled: true }]])
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      type: 'config.save.result', request_id: 'step-up', ok: true
    }), '*')
    expect(showSuccess).toHaveBeenCalledOnce()
  })

  it('keeps current bridge errors, notifications and resize behavior', async () => {
    getPluginConfig.mockRejectedValueOnce(new Error('current failure'))
    const wrapper = mountView()
    await flushPromises()
    const { frame, postMessage } = await openPlugin(wrapper)
    bridgeMessage(frame, 'config.load')
    await flushPromises()
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      type: 'config.load.result', ok: false, error: 'current failure'
    }), '*')

    window.dispatchEvent(new MessageEvent('message', {
      source: frame, origin: 'null',
      data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type: 'ui.resize', height: 720 }
    }))
    window.dispatchEvent(new MessageEvent('message', {
      source: frame, origin: 'null',
      data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type: 'ui.notify', message: 'current notice', level: 'info' }
    }))
    await flushPromises()
    expect(wrapper.get('iframe').element.parentElement?.style.height).toBe('720px')
    expect(showInfo).toHaveBeenCalledWith('current notice')
  })
})
