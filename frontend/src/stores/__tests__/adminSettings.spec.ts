import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { getSettings, getPaymentConfig } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getPaymentConfig: vi.fn()
}))

vi.mock('@/api', () => ({
  adminAPI: {
    settings: { getSettings },
    payment: { getConfig: getPaymentConfig }
  }
}))

import { useAdminSettingsStore } from '@/stores/adminSettings'

describe('useAdminSettingsStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    localStorage.clear()
    getSettings.mockResolvedValue({
      ops_monitoring_enabled: true,
      ops_realtime_monitoring_enabled: true,
      ops_query_mode_default: 'auto',
      custom_menu_items: []
    })
  })

  it('does not block settings readiness on a slow payment config request', async () => {
    let resolvePayment: ((value: { data: { enabled: boolean } }) => void) | undefined
    getPaymentConfig.mockReturnValue(new Promise((resolve) => {
      resolvePayment = resolve
    }))

    const store = useAdminSettingsStore()
    await store.fetch()

    expect(store.loaded).toBe(true)
    expect(store.loading).toBe(false)
    expect(store.opsMonitoringEnabled).toBe(true)
    expect(store.paymentEnabled).toBe(false)

    resolvePayment?.({ data: { enabled: true } })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(store.paymentEnabled).toBe(true)
  })
})
