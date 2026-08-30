import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface HeartbeatTarget {
  group_id: number
  proxy_group_id: number
}

export interface HeartbeatConfig {
  enabled: boolean
  vault_url: string
  allow_insecure_vault: boolean
  allowed_source_ips: string[]
  default_group_id: number
  targets: HeartbeatTarget[]
  worker_count: number
  proxy_probe_workers: number
  proxy_probe_sample_size: number
  proxy_probe_timeout_seconds: number
  proxy_sweep_ttl_seconds: number
  max_attempts: number
  config_source: 'deployment' | 'database' | string
  status?: HeartbeatStatus
}

export interface HeartbeatStatus {
  enabled: boolean
  running: boolean
  config_source: string
  last_heartbeat_at?: string | null
  queued: number
  processing: number
  retry: number
  failed: number
  complete: number
  last_error?: string
  last_error_at?: string | null
}

export interface HeartbeatGroupOption {
  id: number
  name: string
  platform: string
  status: string
}

export interface HeartbeatProxyGroupOption {
  id: number
  name: string
  active_proxy_count: number
}

export interface HeartbeatOptions {
  groups: HeartbeatGroupOption[]
  proxy_groups: HeartbeatProxyGroupOption[]
}

export interface HeartbeatProvisioningLog {
  id: number
  provider: string
  fingerprint: string
  source_balance?: number | null
  source_checked_at?: string | null
  status: string
  attempts: number
  available_at: string
  locked_until?: string | null
  target_group_id: number
  target_proxy_group_id?: number | null
  account_id?: number | null
  proxy_id?: number | null
  last_error?: string
  created_at: string
  updated_at: string
  completed_at?: string | null
}

export type HeartbeatConfigUpdate = Omit<HeartbeatConfig, 'config_source' | 'status'>

export async function getConfig(): Promise<HeartbeatConfig> {
  const { data } = await apiClient.get<HeartbeatConfig>('/admin/heartbeat/config')
  return data
}

export async function updateConfig(payload: HeartbeatConfigUpdate): Promise<HeartbeatConfig> {
  const { data } = await apiClient.put<HeartbeatConfig>('/admin/heartbeat/config', payload)
  return data
}

export async function getOptions(): Promise<HeartbeatOptions> {
  const { data } = await apiClient.get<HeartbeatOptions>('/admin/heartbeat/options')
  return data
}

export async function getStatus(): Promise<HeartbeatStatus> {
  const { data } = await apiClient.get<HeartbeatStatus>('/admin/heartbeat/status')
  return data
}

export async function getLogs(page = 1, pageSize = 25): Promise<PaginatedResponse<HeartbeatProvisioningLog>> {
  const { data } = await apiClient.get<PaginatedResponse<HeartbeatProvisioningLog>>('/admin/heartbeat/logs', {
    params: { page, page_size: pageSize },
  })
  return data
}

const heartbeatAPI = {
  getConfig,
  updateConfig,
  getOptions,
  getStatus,
  getLogs,
}

export default heartbeatAPI
