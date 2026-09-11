<template>
  <div
    v-if="visible"
    data-test="sensenova-quota"
    class="min-w-[240px] space-y-1"
  >
    <div v-if="data?.success && visiblePools.length" class="space-y-1">
      <div
        v-for="(pool, poolIndex) in visiblePools"
        :key="pool.id || `${pool.name || 'pool'}-${poolIndex}`"
        class="space-y-1"
      >
        <div
          v-if="visiblePools.length > 1"
          data-test="sensenova-quota-pool"
          class="truncate text-[9px] font-medium text-gray-500 dark:text-gray-400"
          :title="pool.name || pool.id"
        >
          {{ pool.name || pool.id || t('admin.accounts.sensenova.quotaPool') }}
        </div>
        <div
          v-for="item in poolWindows(pool)"
          :key="`${pool.id || poolIndex}-${item.window}`"
          data-test="sensenova-quota-tier"
          class="flex min-w-0 items-center gap-1.5 text-[10px] leading-4"
        >
          <span
            data-test="sensenova-quota-label"
            class="w-10 shrink-0 whitespace-nowrap text-gray-500 dark:text-gray-400"
          >
            {{ windowLabel(item.window) }}
          </span>
          <div class="h-1.5 w-16 shrink-0 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
            <div
              class="h-full rounded-full transition-all"
              :class="utilizationColor(item.quota)"
              :style="{ width: `${Math.min(100, Math.max(0, quotaPercent(item.quota)))}%` }"
            />
          </div>
          <span :class="['shrink-0 font-medium', utilizationTextColor(item.quota)]">
            {{ Math.round(quotaPercent(item.quota)) }}%
          </span>
          <span
            v-if="item.quota.reset_at"
            class="min-w-0 truncate text-gray-400 dark:text-gray-500"
            :title="item.quota.reset_at"
          >
            · {{ formatReset(item.quota.reset_at) }}
          </span>
        </div>
      </div>
    </div>

    <div v-else-if="!loading" class="text-xs text-gray-400">-</div>

    <div class="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        data-test="sensenova-quota-probe"
        class="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
        :disabled="loading"
        :title="t('admin.accounts.sensenova.quotaProbeTooltip')"
        @click="handleProbe"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': loading }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          />
        </svg>
        {{ t('admin.accounts.sensenova.quotaProbe') }}
      </button>
    </div>

    <div
      v-if="error"
      class="truncate text-[10px] leading-4 text-red-600 dark:text-red-400"
      :title="error"
    >
      {{ truncatedError }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  SenseNovaQuotaPool,
  SenseNovaQuotaProbeResult,
  SenseNovaQuotaSnapshot,
  SenseNovaQuotaWindow
} from '@/api/admin/sensenova'
import type { Account } from '@/types'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()

const visible = computed(() => props.account.platform === 'sensenova')
const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<SenseNovaQuotaProbeResult | null>(null)

const SNAPSHOT_STALE_MS = 15 * 60 * 1000
const AUTO_PROBE_DEBOUNCE_MS = 5 * 60 * 1000
const lastAutoProbeAt = new Map<number, number>()

const readSnapshot = (): SenseNovaQuotaSnapshot | null => {
  const raw = (props.account.extra as Record<string, unknown> | undefined)?.sensenova_quota_snapshot
  if (!raw || typeof raw !== 'object') return null
  const snapshot = raw as Partial<SenseNovaQuotaSnapshot>
  if (!Array.isArray(snapshot.pools) || typeof snapshot.fetched_at !== 'string') return null
  return {
    plan: (snapshot.plan || {}) as SenseNovaQuotaSnapshot['plan'],
    pools: snapshot.pools as SenseNovaQuotaPool[],
    fetched_at: snapshot.fetched_at
  }
}

const snapshotData = computed(() => readSnapshot())
const snapshotIsStale = computed(() => {
  const fetchedAt = snapshotData.value?.fetched_at
  if (!fetchedAt) return true
  const ts = new Date(fetchedAt).getTime()
  return Number.isNaN(ts) || Date.now() - ts > SNAPSHOT_STALE_MS
})

const visiblePools = computed(() => data.value?.pools || [])

const poolWindows = (pool: SenseNovaQuotaPool): Array<{ window: '5h' | '7d'; quota: SenseNovaQuotaWindow }> => {
  const windows: Array<{ window: '5h' | '7d'; quota: SenseNovaQuotaWindow }> = []
  if (pool.window_5h) windows.push({ window: '5h', quota: pool.window_5h })
  if (pool.window_7d) windows.push({ window: '7d', quota: pool.window_7d })
  return windows
}

const quotaPercent = (quota: SenseNovaQuotaWindow) =>
  quota.limit > 0 ? (quota.used / quota.limit) * 100 : 0

const utilizationColor = (quota: SenseNovaQuotaWindow) => {
  const percent = quotaPercent(quota)
  if (percent >= 90) return 'bg-red-500'
  if (percent >= 75) return 'bg-amber-500'
  return 'bg-emerald-500'
}

const utilizationTextColor = (quota: SenseNovaQuotaWindow) => {
  const percent = quotaPercent(quota)
  if (percent >= 90) return 'text-red-600 dark:text-red-400'
  if (percent >= 75) return 'text-amber-600 dark:text-amber-400'
  return 'text-emerald-600 dark:text-emerald-400'
}

const windowLabel = (window: '5h' | '7d') =>
  window === '5h'
    ? t('admin.accounts.sensenova.quota5h')
    : t('admin.accounts.sensenova.quota7d')

const formatReset = (iso: string) => {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const diffMs = date.getTime() - Date.now()
  if (diffMs <= 0) return t('admin.accounts.sensenova.quotaResetSoon')
  if (diffMs < 3_600_000) return `${Math.max(1, Math.round(diffMs / 60_000))}m`
  const hours = Math.round(diffMs / 3_600_000)
  if (hours < 48) return `${hours}h`
  return `${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const extractErrorMessage = (e: unknown): string => {
  const err = e as {
    message?: string
    reason?: string
    response?: { data?: { message?: string; error?: string } }
  }
  return err?.message || err?.reason || err?.response?.data?.message || err?.response?.data?.error || t('common.error')
}

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}...` : error.value
})

const handleProbe = async () => {
  if (loading.value) return
  loading.value = true
  error.value = null
  try {
    const result = await adminAPI.sensenova.queryQuota(props.account.id)
    if (result.success) data.value = result
    else error.value = result.error || t('common.error')
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (!visible.value) return
  const snapshot = snapshotData.value
  if (snapshot) {
    data.value = {
      provider: 'sensenova',
      source: 'sensenova_quota',
      success: true,
      credential_valid: true,
      plan: snapshot.plan,
      pools: snapshot.pools,
      fetched_at: Math.floor(new Date(snapshot.fetched_at).getTime() / 1000),
      persisted: true
    }
  }
  if (!snapshotIsStale.value) return
  const last = lastAutoProbeAt.get(props.account.id) || 0
  if (Date.now() - last < AUTO_PROBE_DEBOUNCE_MS) return
  lastAutoProbeAt.set(props.account.id, Date.now())
  void handleProbe()
})

watch(
  () => props.account.id,
  () => {
    data.value = null
    error.value = null
    loading.value = false
  }
)
</script>
