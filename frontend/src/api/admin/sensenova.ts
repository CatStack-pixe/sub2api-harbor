/**
 * Admin SenseNova native Token Plan quota endpoint.
 * The quota API uses the account's separate access_token credential.
 */

import { apiClient } from '../client'

export interface SenseNovaQuotaPlan {
  id?: string
  name?: string
  type?: string
}

export interface SenseNovaQuotaWindow {
  limit: number
  used: number
  remaining: number
  reset_at?: string
}

export interface SenseNovaQuotaPool {
  id?: string
  name?: string
  pool_type?: string
  model_ids?: string[]
  window_5h?: SenseNovaQuotaWindow
  window_7d?: SenseNovaQuotaWindow
  grant_balance?: number
  nearest_grant_expiry?: string
}

export interface SenseNovaQuotaSnapshot {
  plan: SenseNovaQuotaPlan
  pools: SenseNovaQuotaPool[]
  fetched_at: string
}

export interface SenseNovaQuotaProbeResult {
  provider: string
  source?: string
  success: boolean
  credential_valid: boolean
  plan?: SenseNovaQuotaPlan
  pools?: SenseNovaQuotaPool[]
  status_code?: number
  fetched_at: number
  persisted: boolean
  error?: string
}

export async function queryQuota(id: number): Promise<SenseNovaQuotaProbeResult> {
  const { data } = await apiClient.get<SenseNovaQuotaProbeResult>(
    `/admin/cn-providers/accounts/${id}/sensenova-quota`
  )
  return data
}

export default {
  queryQuota
}
