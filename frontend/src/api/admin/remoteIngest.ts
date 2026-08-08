import { apiClient } from '../client'
import type { FetchOptions, PaginatedResponse } from '@/types'

export type RemoteIngestDeliveryStatus = 'pending' | 'probing' | 'active' | 'probe_failed'

export interface RemoteIngestRegistrationToken {
  id: string
  fingerprint: string
  expires_at: string
  used_at?: string | null
  client_id?: string | null
  created_at: string
}

export interface CreatedRemoteIngestRegistrationToken extends RemoteIngestRegistrationToken {
  token: string
}

export interface RemoteIngestClient {
  id: string
  machine_name: string
  public_key_fingerprint: string
  access_subject: string
  enrolled_at: string
  last_active_at?: string | null
  revoked_at?: string | null
}

export interface RemoteIngestDelivery {
  id: string
  client_id: string
  client_machine_name?: string
  external_id: string
  account_id: number
  platform: string
  group_name: string
  test_model?: string | null
  status: RemoteIngestDeliveryStatus
  masked_error?: string | null
  attempts: number
  created_at: string
  updated_at: string
  completed_at?: string | null
}

export interface RemoteIngestListQuery {
  search?: string
  status?: string
}

interface MutationOptions {
  idempotencyKey: string
}

interface RevokeClientResult {
  client_id: string
  revoked: boolean
}

interface RetryDeliveryResult {
  delivery_id: string
  status: RemoteIngestDeliveryStatus
}

function mutationConfig(options: MutationOptions) {
  return {
    headers: {
      'Idempotency-Key': options.idempotencyKey
    }
  }
}

export async function createRegistrationToken(
  expiresInSeconds: number,
  options: MutationOptions
): Promise<CreatedRemoteIngestRegistrationToken> {
  const { data } = await apiClient.post<CreatedRemoteIngestRegistrationToken>(
    '/admin/remote-ingest/registration-tokens',
    { expires_in_seconds: expiresInSeconds },
    mutationConfig(options)
  )
  return data
}

export async function listRegistrationTokens(
  page = 1,
  pageSize = 20,
  query: RemoteIngestListQuery = {},
  options?: FetchOptions
): Promise<PaginatedResponse<RemoteIngestRegistrationToken>> {
  const { data } = await apiClient.get<PaginatedResponse<RemoteIngestRegistrationToken>>(
    '/admin/remote-ingest/registration-tokens',
    {
      params: { page, page_size: pageSize, ...query },
      signal: options?.signal
    }
  )
  return data
}

export async function listClients(
  page = 1,
  pageSize = 20,
  query: RemoteIngestListQuery = {},
  options?: FetchOptions
): Promise<PaginatedResponse<RemoteIngestClient>> {
  const { data } = await apiClient.get<PaginatedResponse<RemoteIngestClient>>(
    '/admin/remote-ingest/clients',
    {
      params: { page, page_size: pageSize, ...query },
      signal: options?.signal
    }
  )
  return data
}

export async function revokeClient(
  id: string,
  options: MutationOptions
): Promise<RevokeClientResult> {
  const { data } = await apiClient.post<RevokeClientResult>(
    `/admin/remote-ingest/clients/${encodeURIComponent(id)}/revoke`,
    undefined,
    mutationConfig(options)
  )
  return data
}

export async function listDeliveries(
  page = 1,
  pageSize = 20,
  query: RemoteIngestListQuery = {},
  options?: FetchOptions
): Promise<PaginatedResponse<RemoteIngestDelivery>> {
  const { data } = await apiClient.get<PaginatedResponse<RemoteIngestDelivery>>(
    '/admin/remote-ingest/deliveries',
    {
      params: { page, page_size: pageSize, ...query },
      signal: options?.signal
    }
  )
  return data
}

export async function retryDelivery(
  id: string,
  options: MutationOptions
): Promise<RetryDeliveryResult> {
  const { data } = await apiClient.post<RetryDeliveryResult>(
    `/admin/remote-ingest/deliveries/${encodeURIComponent(id)}/retry`,
    undefined,
    mutationConfig(options)
  )
  return data
}

const remoteIngestAPI = {
  createRegistrationToken,
  listRegistrationTokens,
  listClients,
  revokeClient,
  listDeliveries,
  retryDelivery
}

export default remoteIngestAPI
