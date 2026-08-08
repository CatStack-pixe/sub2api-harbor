<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <nav
          class="flex overflow-x-auto border-b border-gray-200 dark:border-dark-700"
          role="tablist"
          :aria-label="t('admin.remoteIngest.title')"
        >
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            role="tab"
            class="-mb-px inline-flex shrink-0 items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium transition-colors"
            :class="activeTab === tab.key
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-800 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
            :aria-selected="activeTab === tab.key"
            :tabindex="activeTab === tab.key ? 0 : -1"
            :data-test="`remote-ingest-tab-${tab.key}`"
            @click="selectTab(tab.key)"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </nav>
      </template>

      <template #filters>
        <div class="flex flex-wrap items-end gap-3 rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
          <template v-if="activeTab === 'tokens'">
            <div class="w-44">
              <label class="input-label">{{ t('admin.remoteIngest.filters.status') }}</label>
              <Select v-model="tokenFilters.status" :options="tokenStatusOptions" @change="searchTokens" />
            </div>
            <div class="ml-auto w-48">
              <label class="input-label">{{ t('admin.remoteIngest.tokens.expiresIn') }}</label>
              <Select v-model="tokenLifetime" :options="tokenLifetimeOptions" :disabled="creatingToken" />
            </div>
            <button data-test="generate-registration-token" type="button" class="btn btn-primary" :disabled="creatingToken" @click="generateToken">
              <Icon name="plus" size="sm" class="mr-1.5" />
              {{ creatingToken ? t('common.processing') : t('admin.remoteIngest.tokens.generate') }}
            </button>
          </template>

          <template v-else-if="activeTab === 'clients'">
            <div class="min-w-56 flex-1 sm:max-w-sm">
              <label class="input-label">{{ t('common.search') }}</label>
              <input
                v-model.trim="clientFilters.search"
                class="input"
                type="search"
                :placeholder="t('admin.remoteIngest.clients.searchPlaceholder')"
                @keyup.enter="searchClients"
              />
            </div>
            <div class="w-44">
              <label class="input-label">{{ t('admin.remoteIngest.filters.status') }}</label>
              <Select v-model="clientFilters.status" :options="clientStatusOptions" @change="searchClients" />
            </div>
            <button type="button" class="btn btn-secondary" @click="searchClients">
              <Icon name="search" size="sm" class="mr-1.5" />
              {{ t('common.search') }}
            </button>
          </template>

          <template v-else>
            <div class="min-w-56 flex-1 sm:max-w-sm">
              <label class="input-label">{{ t('common.search') }}</label>
              <input
                v-model.trim="deliveryFilters.search"
                class="input"
                type="search"
                :placeholder="t('admin.remoteIngest.deliveries.searchPlaceholder')"
                @keyup.enter="searchDeliveries"
              />
            </div>
            <div class="w-44">
              <label class="input-label">{{ t('admin.remoteIngest.filters.status') }}</label>
              <Select v-model="deliveryFilters.status" :options="deliveryStatusOptions" @change="searchDeliveries" />
            </div>
            <button type="button" class="btn btn-secondary" @click="searchDeliveries">
              <Icon name="search" size="sm" class="mr-1.5" />
              {{ t('common.search') }}
            </button>
          </template>

          <button
            type="button"
            class="btn btn-secondary btn-icon"
            :disabled="activeLoading"
            :title="t('common.refresh')"
            :aria-label="t('common.refresh')"
            @click="refreshActiveTab"
          >
            <Icon name="refresh" size="md" :class="activeLoading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>

      <template #table>
        <DataTable
          v-if="activeTab === 'tokens'"
          :columns="tokenColumns"
          :data="tokens"
          :loading="tokenLoading"
          row-key="id"
        >
          <template #cell-fingerprint="{ value }">
            <code class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ value }}</code>
          </template>
          <template #cell-status="{ row }">
            <span :class="statusBadgeClass(tokenStatus(row))">
              {{ t(`admin.remoteIngest.tokens.status.${tokenStatus(row)}`) }}
            </span>
          </template>
          <template #cell-expires_at="{ value }">{{ formatTimestamp(value) }}</template>
          <template #cell-used_at="{ value }">{{ formatTimestamp(value) }}</template>
          <template #cell-created_at="{ value }">{{ formatTimestamp(value) }}</template>
          <template #empty>
            <EmptyState :title="t('admin.remoteIngest.tokens.empty')" />
          </template>
        </DataTable>

        <DataTable
          v-else-if="activeTab === 'clients'"
          :columns="clientColumns"
          :data="clients"
          :loading="clientLoading"
          row-key="id"
        >
          <template #cell-machine_name="{ row }">
            <div class="max-w-60">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="row.machine_name">
                {{ row.machine_name }}
              </div>
              <div class="mt-0.5 truncate font-mono text-xs text-gray-400" :title="row.id">{{ row.id }}</div>
            </div>
          </template>
          <template #cell-public_key_fingerprint="{ value }">
            <code class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ value }}</code>
          </template>
          <template #cell-access_subject="{ value }">
            <span class="block max-w-64 truncate font-mono text-xs" :title="value">{{ value }}</span>
          </template>
          <template #cell-status="{ row }">
            <span :class="statusBadgeClass(row.revoked_at ? 'revoked' : 'active')">
              {{ t(`admin.remoteIngest.clients.status.${row.revoked_at ? 'revoked' : 'active'}`) }}
            </span>
          </template>
          <template #cell-last_active_at="{ value }">{{ formatTimestamp(value) }}</template>
          <template #cell-enrolled_at="{ value }">{{ formatTimestamp(value) }}</template>
          <template #cell-actions="{ row }">
            <button
              v-if="!row.revoked_at"
              type="button"
              class="btn btn-ghost btn-icon text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
              :title="t('admin.remoteIngest.clients.revoke')"
              :aria-label="t('admin.remoteIngest.clients.revoke')"
              :data-test="`revoke-client-${row.id}`"
              @click="openRevokeDialog(row)"
            >
              <Icon name="ban" size="sm" />
            </button>
            <span v-else class="text-gray-400">-</span>
          </template>
          <template #empty>
            <EmptyState :title="t('admin.remoteIngest.clients.empty')" />
          </template>
        </DataTable>

        <DataTable
          v-else
          :columns="deliveryColumns"
          :data="deliveries"
          :loading="deliveryLoading"
          row-key="id"
        >
          <template #cell-external_id="{ row }">
            <div class="max-w-60">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="row.external_id">
                {{ row.external_id }}
              </div>
              <div class="mt-0.5 truncate font-mono text-xs text-gray-400" :title="row.id">{{ row.id }}</div>
            </div>
          </template>
          <template #cell-client="{ row }">
            <div class="max-w-48 truncate" :title="row.client_machine_name || row.client_id">
              {{ row.client_machine_name || row.client_id }}
            </div>
          </template>
          <template #cell-status="{ value }">
            <span :class="statusBadgeClass(value)">
              {{ t(`admin.remoteIngest.deliveries.status.${value}`) }}
            </span>
          </template>
          <template #cell-account_id="{ value }">
            <router-link
              v-if="value"
              :to="{ path: '/admin/accounts', query: { search: String(value) } }"
              class="font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
            >
              #{{ value }}
            </router-link>
            <span v-else>-</span>
          </template>
          <template #cell-masked_error="{ value }">
            <span
              class="block max-w-72 truncate text-sm text-red-600 dark:text-red-400"
              :title="value || ''"
            >
              {{ value || '-' }}
            </span>
          </template>
          <template #cell-updated_at="{ value }">{{ formatTimestamp(value) }}</template>
          <template #cell-actions="{ row }">
            <button
              v-if="row.status === 'probe_failed'"
              type="button"
              class="btn btn-ghost btn-icon text-primary-600 dark:text-primary-400"
              :disabled="retryingDeliveryId === row.id"
              :title="t('admin.remoteIngest.deliveries.retry')"
              :aria-label="t('admin.remoteIngest.deliveries.retry')"
              :data-test="`retry-delivery-${row.id}`"
              @click="retryProbe(row)"
            >
              <Icon name="refresh" size="sm" :class="retryingDeliveryId === row.id ? 'animate-spin' : ''" />
            </button>
            <span v-else class="text-gray-400">-</span>
          </template>
          <template #empty>
            <EmptyState :title="t('admin.remoteIngest.deliveries.empty')" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="activePagination.total > 0"
          :page="activePagination.page"
          :total="activePagination.total"
          :page-size="activePagination.page_size"
          @update:page="changePage"
          @update:pageSize="changePageSize"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="Boolean(createdToken)"
      :title="t('admin.remoteIngest.tokens.createdTitle')"
      width="normal"
      @close="closeTokenDialog"
    >
      <div v-if="createdToken" class="space-y-4">
        <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-800/70 dark:bg-amber-900/20 dark:text-amber-200">
          {{ t('admin.remoteIngest.tokens.oneTimeWarning') }}
        </div>
        <div>
          <label class="input-label">{{ t('admin.remoteIngest.tokens.token') }}</label>
          <div class="flex items-center gap-2">
            <input data-test="created-registration-token" :value="createdToken.token" type="text" class="input min-w-0 flex-1 font-mono" readonly />
            <button
              type="button"
              class="btn btn-secondary btn-icon shrink-0"
              :title="t('common.copy')"
              :aria-label="t('common.copy')"
              @click="copyCreatedToken"
            >
              <Icon name="copy" size="md" />
            </button>
          </div>
        </div>
        <dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
          <div>
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.remoteIngest.tokens.fingerprint') }}</dt>
            <dd class="mt-1 font-mono text-gray-900 dark:text-white">{{ createdToken.fingerprint }}</dd>
          </div>
          <div>
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.remoteIngest.tokens.expiresAt') }}</dt>
            <dd class="mt-1 text-gray-900 dark:text-white">{{ formatTimestamp(createdToken.expires_at) }}</dd>
          </div>
        </dl>
      </div>
      <template #footer>
        <button data-test="close-registration-token" type="button" class="btn btn-primary" @click="closeTokenDialog">
          {{ t('common.close') }}
        </button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="Boolean(clientToRevoke)"
      :title="t('admin.remoteIngest.clients.revokeTitle')"
      :message="t('admin.remoteIngest.clients.revokeConfirm', { name: clientToRevoke?.machine_name || '' })"
      :confirm-text="t('admin.remoteIngest.clients.revoke')"
      danger
      @confirm="confirmRevoke"
      @cancel="clientToRevoke = null"
    />

    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  CreatedRemoteIngestRegistrationToken,
  RemoteIngestClient,
  RemoteIngestDelivery,
  RemoteIngestDeliveryStatus,
  RemoteIngestRegistrationToken
} from '@/api/admin'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import type { Column } from '@/components/common/types'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize, setPersistedPageSize } from '@/composables/usePersistedPageSize'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp
} from '@/composables/useStepUp'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'

type TabKey = 'tokens' | 'clients' | 'deliveries'

interface PaginationState {
  page: number
  page_size: number
  total: number
}

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const stepUp = useStepUp()

const activeTab = ref<TabKey>('tokens')
const createdToken = ref<CreatedRemoteIngestRegistrationToken | null>(null)
const creatingToken = ref(false)
const clientToRevoke = ref<RemoteIngestClient | null>(null)
const retryingDeliveryId = ref('')
const tokenLifetime = ref<number>(600)

const tokens = ref<RemoteIngestRegistrationToken[]>([])
const clients = ref<RemoteIngestClient[]>([])
const deliveries = ref<RemoteIngestDelivery[]>([])
const tokenLoading = ref(false)
const clientLoading = ref(false)
const deliveryLoading = ref(false)

const tokenFilters = reactive({ status: '' })
const clientFilters = reactive({ search: '', status: '' })
const deliveryFilters = reactive({ search: '', status: '' })

const createPagination = (): PaginationState => ({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0
})

const tokenPagination = reactive(createPagination())
const clientPagination = reactive(createPagination())
const deliveryPagination = reactive(createPagination())

let tokenAbortController: AbortController | null = null
let clientAbortController: AbortController | null = null
let deliveryAbortController: AbortController | null = null
let createTokenOperationKey = ''
const revokeOperationKeys = new Map<string, string>()
const retryOperationKeys = new Map<string, string>()
const loadedTabs = new Set<TabKey>()

const tabs = computed(() => [
  { key: 'tokens' as const, label: t('admin.remoteIngest.tabs.tokens'), icon: 'key' as const },
  { key: 'clients' as const, label: t('admin.remoteIngest.tabs.clients'), icon: 'server' as const },
  { key: 'deliveries' as const, label: t('admin.remoteIngest.tabs.deliveries'), icon: 'inbox' as const }
])

const tokenStatusOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'available', label: t('admin.remoteIngest.tokens.status.available') },
  { value: 'used', label: t('admin.remoteIngest.tokens.status.used') },
  { value: 'expired', label: t('admin.remoteIngest.tokens.status.expired') }
])

const clientStatusOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'active', label: t('admin.remoteIngest.clients.status.active') },
  { value: 'revoked', label: t('admin.remoteIngest.clients.status.revoked') }
])

const deliveryStatusOptions = computed(() => [
  { value: '', label: t('common.all') },
  ...(['pending', 'probing', 'active', 'probe_failed'] as RemoteIngestDeliveryStatus[]).map((value) => ({
    value,
    label: t(`admin.remoteIngest.deliveries.status.${value}`)
  }))
])

const tokenLifetimeOptions = computed(() => [
  { value: 600, label: t('admin.remoteIngest.tokens.lifetime.10m') },
  { value: 1800, label: t('admin.remoteIngest.tokens.lifetime.30m') },
  { value: 3600, label: t('admin.remoteIngest.tokens.lifetime.1h') }
])

const tokenColumns = computed<Column[]>(() => [
  { key: 'fingerprint', label: t('admin.remoteIngest.tokens.fingerprint') },
  { key: 'status', label: t('common.status') },
  { key: 'expires_at', label: t('admin.remoteIngest.tokens.expiresAt') },
  { key: 'used_at', label: t('admin.remoteIngest.tokens.usedAt') },
  { key: 'created_at', label: t('admin.remoteIngest.columns.createdAt') }
])

const clientColumns = computed<Column[]>(() => [
  { key: 'machine_name', label: t('admin.remoteIngest.clients.machineName') },
  { key: 'public_key_fingerprint', label: t('admin.remoteIngest.clients.publicKeyFingerprint') },
  { key: 'access_subject', label: t('admin.remoteIngest.clients.accessSubject') },
  { key: 'status', label: t('common.status') },
  { key: 'last_active_at', label: t('admin.remoteIngest.clients.lastActiveAt') },
  { key: 'enrolled_at', label: t('admin.remoteIngest.clients.enrolledAt') },
  { key: 'actions', label: t('common.actions') }
])

const deliveryColumns = computed<Column[]>(() => [
  { key: 'external_id', label: t('admin.remoteIngest.deliveries.externalId') },
  { key: 'client', label: t('admin.remoteIngest.deliveries.client') },
  { key: 'platform', label: t('admin.remoteIngest.deliveries.platform') },
  { key: 'group_name', label: t('admin.remoteIngest.deliveries.group') },
  { key: 'status', label: t('common.status') },
  { key: 'account_id', label: t('admin.remoteIngest.deliveries.account') },
  { key: 'attempts', label: t('admin.remoteIngest.deliveries.attempts') },
  { key: 'masked_error', label: t('admin.remoteIngest.deliveries.error') },
  { key: 'updated_at', label: t('admin.remoteIngest.columns.updatedAt') },
  { key: 'actions', label: t('common.actions') }
])

const activePagination = computed(() => {
  if (activeTab.value === 'tokens') return tokenPagination
  if (activeTab.value === 'clients') return clientPagination
  return deliveryPagination
})

const activeLoading = computed(() => {
  if (activeTab.value === 'tokens') return tokenLoading.value
  if (activeTab.value === 'clients') return clientLoading.value
  return deliveryLoading.value
})

function operationKey(action: string, id?: string): string {
  const requestId = globalThis.crypto?.randomUUID?.()
    ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  return `remote-ingest-${action}${id ? `-${id}` : ''}-${requestId}`
}

function currentAdminScope(): string {
  try {
    const raw = globalThis.localStorage?.getItem('auth_user')
    const id = raw ? (JSON.parse(raw) as { id?: string | number }).id : undefined
    return String(id ?? 'unknown')
  } catch {
    return 'unknown'
  }
}

function operationStorageKey(action: string, id?: string): string {
  return `sub2api:admin:remote-ingest:${currentAdminScope()}:${action}${id ? `:${id}` : ''}`
}

function storedOperationKey(action: string, id?: string): string {
  try {
    return globalThis.sessionStorage?.getItem(operationStorageKey(action, id)) ?? ''
  } catch {
    return ''
  }
}

function persistOperationKey(action: string, id: string | undefined, key: string): void {
  try {
    const storageKey = operationStorageKey(action, id)
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // In-memory retry protection remains available when storage is blocked.
  }
}

function isAbortError(error: unknown): boolean {
  const candidate = error as { code?: string; name?: string }
  return candidate?.code === 'ERR_CANCELED' || candidate?.name === 'AbortError' || candidate?.name === 'CanceledError'
}

function showLoadError(error: unknown): void {
  if (isAbortError(error)) return
  appStore.showError((error as { message?: string })?.message || t('admin.remoteIngest.loadFailed'))
}

function reportStepUpBlocked(error: unknown): boolean {
  if (!isStepUpBlocked(error)) return false
  appStore.showError(
    stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
      ? t('stepUp.adminApiKeyForbidden')
      : t('stepUp.notEnabled')
  )
  return true
}

async function loadTokens(): Promise<void> {
  tokenAbortController?.abort()
  const controller = new AbortController()
  tokenAbortController = controller
  tokenLoading.value = true
  try {
    const response = await adminAPI.remoteIngest.listRegistrationTokens(
      tokenPagination.page,
      tokenPagination.page_size,
      { status: tokenFilters.status || undefined },
      { signal: controller.signal }
    )
    tokens.value = response.items || []
    tokenPagination.total = response.total || 0
    loadedTabs.add('tokens')
  } catch (error) {
    showLoadError(error)
  } finally {
    if (tokenAbortController === controller) tokenLoading.value = false
  }
}

async function loadClients(): Promise<void> {
  clientAbortController?.abort()
  const controller = new AbortController()
  clientAbortController = controller
  clientLoading.value = true
  try {
    const response = await adminAPI.remoteIngest.listClients(
      clientPagination.page,
      clientPagination.page_size,
      {
        search: clientFilters.search || undefined,
        status: clientFilters.status || undefined
      },
      { signal: controller.signal }
    )
    clients.value = response.items || []
    clientPagination.total = response.total || 0
    loadedTabs.add('clients')
  } catch (error) {
    showLoadError(error)
  } finally {
    if (clientAbortController === controller) clientLoading.value = false
  }
}

async function loadDeliveries(): Promise<void> {
  deliveryAbortController?.abort()
  const controller = new AbortController()
  deliveryAbortController = controller
  deliveryLoading.value = true
  try {
    const response = await adminAPI.remoteIngest.listDeliveries(
      deliveryPagination.page,
      deliveryPagination.page_size,
      {
        search: deliveryFilters.search || undefined,
        status: deliveryFilters.status || undefined
      },
      { signal: controller.signal }
    )
    deliveries.value = response.items || []
    deliveryPagination.total = response.total || 0
    loadedTabs.add('deliveries')
  } catch (error) {
    showLoadError(error)
  } finally {
    if (deliveryAbortController === controller) deliveryLoading.value = false
  }
}

function loadActiveTab(): Promise<void> {
  if (activeTab.value === 'tokens') return loadTokens()
  if (activeTab.value === 'clients') return loadClients()
  return loadDeliveries()
}

function selectTab(tab: TabKey): void {
  activeTab.value = tab
  if (!loadedTabs.has(tab)) void loadActiveTab()
}

function refreshActiveTab(): void {
  void loadActiveTab()
}

function searchTokens(): void {
  tokenPagination.page = 1
  void loadTokens()
}

function searchClients(): void {
  clientPagination.page = 1
  void loadClients()
}

function searchDeliveries(): void {
  deliveryPagination.page = 1
  void loadDeliveries()
}

function changePage(page: number): void {
  activePagination.value.page = page
  void loadActiveTab()
}

function changePageSize(pageSize: number): void {
  activePagination.value.page = 1
  activePagination.value.page_size = pageSize
  setPersistedPageSize(pageSize)
  void loadActiveTab()
}

function tokenStatus(token: RemoteIngestRegistrationToken): 'available' | 'used' | 'expired' {
  if (token.used_at) return 'used'
  if (new Date(token.expires_at).getTime() <= Date.now()) return 'expired'
  return 'available'
}

watch(tokenLifetime, () => {
  // A retried idempotent request must keep the original payload. Choosing a
  // different lifetime is a new operation, so it needs a new key.
  createTokenOperationKey = ''
})

function statusBadgeClass(status: string): string {
  const base = 'inline-flex rounded-full px-2.5 py-1 text-xs font-medium'
  switch (status) {
    case 'active':
    case 'available':
      return `${base} bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300`
    case 'probing':
      return `${base} bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300`
    case 'pending':
      return `${base} bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300`
    case 'probe_failed':
    case 'revoked':
    case 'expired':
      return `${base} bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300`
    default:
      return `${base} bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300`
  }
}

function formatTimestamp(value: string | null | undefined): string {
  return value ? formatDateTime(value) : '-'
}

async function generateToken(): Promise<void> {
  const storageAction = `create-token-${tokenLifetime.value}`
  if (!createTokenOperationKey) {
    createTokenOperationKey = storedOperationKey(storageAction) || operationKey('create-token')
  }
  persistOperationKey(storageAction, undefined, createTokenOperationKey)
  creatingToken.value = true
  try {
    createdToken.value = await stepUp.run(() => adminAPI.remoteIngest.createRegistrationToken(
      Number(tokenLifetime.value),
      { idempotencyKey: createTokenOperationKey }
    ))
    persistOperationKey(storageAction, undefined, '')
    createTokenOperationKey = ''
    appStore.showSuccess(t('admin.remoteIngest.tokens.generated'))
    await loadTokens()
  } catch (error) {
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    appStore.showError((error as { message?: string })?.message || t('admin.remoteIngest.tokens.generateFailed'))
  } finally {
    creatingToken.value = false
  }
}

function closeTokenDialog(): void {
  createdToken.value = null
}

async function copyCreatedToken(): Promise<void> {
  if (!createdToken.value) return
  await copyToClipboard(createdToken.value.token, t('admin.remoteIngest.tokens.copied'))
}

function openRevokeDialog(client: RemoteIngestClient): void {
  clientToRevoke.value = client
}

async function confirmRevoke(): Promise<void> {
  const client = clientToRevoke.value
  if (!client) return
  const key = revokeOperationKeys.get(client.id)
    || storedOperationKey('revoke-client', client.id)
    || operationKey('revoke-client', client.id)
  revokeOperationKeys.set(client.id, key)
  persistOperationKey('revoke-client', client.id, key)
  try {
    await stepUp.run(() => adminAPI.remoteIngest.revokeClient(client.id, { idempotencyKey: key }))
    revokeOperationKeys.delete(client.id)
    persistOperationKey('revoke-client', client.id, '')
    clientToRevoke.value = null
    appStore.showSuccess(t('admin.remoteIngest.clients.revoked'))
    await loadClients()
  } catch (error) {
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    appStore.showError((error as { message?: string })?.message || t('admin.remoteIngest.clients.revokeFailed'))
  }
}

async function retryProbe(delivery: RemoteIngestDelivery): Promise<void> {
  const key = retryOperationKeys.get(delivery.id)
    || storedOperationKey('retry-delivery', delivery.id)
    || operationKey('retry-delivery', delivery.id)
  retryOperationKeys.set(delivery.id, key)
  persistOperationKey('retry-delivery', delivery.id, key)
  retryingDeliveryId.value = delivery.id
  try {
    await stepUp.run(() => adminAPI.remoteIngest.retryDelivery(delivery.id, { idempotencyKey: key }))
    retryOperationKeys.delete(delivery.id)
    persistOperationKey('retry-delivery', delivery.id, '')
    appStore.showSuccess(t('admin.remoteIngest.deliveries.retryQueued'))
    await loadDeliveries()
  } catch (error) {
    if (isStepUpCancelled(error)) return
    if (reportStepUpBlocked(error)) return
    appStore.showError((error as { message?: string })?.message || t('admin.remoteIngest.deliveries.retryFailed'))
  } finally {
    retryingDeliveryId.value = ''
  }
}

onMounted(() => {
  void loadTokens()
})

onBeforeUnmount(() => {
  createdToken.value = null
  tokenAbortController?.abort()
  clientAbortController?.abort()
  deliveryAbortController?.abort()
})
</script>
