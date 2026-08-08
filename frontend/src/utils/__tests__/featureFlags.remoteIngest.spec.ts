import { beforeEach, describe, expect, it, vi } from 'vitest'

const appStore = vi.hoisted(() => ({
  cachedPublicSettings: null as null | Record<string, unknown>
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore
}))

import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

describe('remote ingest feature flag', () => {
  beforeEach(() => {
    appStore.cachedPublicSettings = null
  })

  it('is opt-in and stays hidden while settings are unknown', () => {
    expect(FeatureFlags.remoteIngest).toEqual({
      key: 'remote_ingest_enabled',
      mode: 'opt-in',
      label: 'Remote Ingest'
    })
    expect(isFeatureFlagEnabled(FeatureFlags.remoteIngest)).toBe(false)
  })

  it('becomes visible only after the backend explicitly enables it', () => {
    appStore.cachedPublicSettings = { remote_ingest_enabled: true }
    expect(isFeatureFlagEnabled(FeatureFlags.remoteIngest)).toBe(true)

    appStore.cachedPublicSettings = { remote_ingest_enabled: false }
    expect(isFeatureFlagEnabled(FeatureFlags.remoteIngest)).toBe(false)
  })
})
