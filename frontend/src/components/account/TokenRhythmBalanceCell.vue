<template>
  <div v-if="visible" class="space-y-1">
    <button
      type="button"
      class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-teal-700 transition-colors hover:bg-teal-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-teal-300 dark:hover:bg-teal-900/30"
      :disabled="loading"
      :title="t('admin.accounts.usageWindow.tokenrhythmProbeTooltip')"
      @click="probe(true)"
    >
      <Icon name="refresh" size="xs" :class="loading ? 'animate-spin' : ''" />
      {{ t('admin.accounts.usageWindow.tokenrhythmProbe') }}
    </button>
    <div v-if="summary" class="text-[10px] text-gray-600 dark:text-gray-300">
      {{ summary }}
    </div>
    <div v-if="error" class="truncate text-[10px] text-red-600 dark:text-red-400" :title="error">
      {{ truncatedError }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TokenRhythmBalanceResult } from '@/api/admin/accounts'
import type { Account } from '@/types'
import { queryTokenRhythmBalance } from '@/utils/tokenRhythmBalanceQuery'

const props = withDefaults(
  defineProps<{
    account: Account
    autoLoad?: boolean
    refreshToken?: number
  }>(),
  {
    autoLoad: true,
    refreshToken: 0
  }
)
const { t } = useI18n()
const visible = computed(() => props.account.platform === 'tokenrhythm')
const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<TokenRhythmBalanceResult | null>(null)
let requestGeneration = 0
let disposed = false
let activeController: AbortController | null = null
let refreshPending = false

const extractErrorMessage = (value: unknown): string => {
  const err = value as { message?: string; reason?: string; response?: { data?: { message?: string; error?: string } } }
  return err?.response?.data?.message || err?.response?.data?.error || err?.message || err?.reason || t('common.error')
}

const money = (value: number | undefined) => typeof value === 'number' ? value.toFixed(2) : ''

const summary = computed(() => {
  if (!data.value) return ''
  const available = money(data.value.available_balance_cny)
  if (!available) return t('admin.accounts.usageWindow.tokenrhythmNoBalance')
  const currency = data.value.currency || 'CNY'
  return `${currency} ${available}`
})

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}...` : error.value
})

const probe = async (force = false) => {
  if (!visible.value) return
  if (loading.value) {
    if (force) refreshPending = true
    return
  }
  refreshPending = false
  const accountId = props.account.id
  const generation = ++requestGeneration
  const controller = new AbortController()
  activeController = controller
  loading.value = true
  error.value = null
  try {
    const result = await queryTokenRhythmBalance(accountId, { signal: controller.signal, force })
    if (!disposed && generation === requestGeneration && props.account.id === accountId) {
      data.value = result
    }
  } catch (value) {
    if (!controller.signal.aborted && !disposed && generation === requestGeneration && props.account.id === accountId) {
      error.value = extractErrorMessage(value)
    }
  } finally {
    if (!disposed && generation === requestGeneration && props.account.id === accountId) {
      activeController = null
      loading.value = false
      if (refreshPending && props.autoLoad) {
        refreshPending = false
        void probe(true)
      }
    }
  }
}

watch(
  [() => props.account.id, () => props.account.platform],
  () => {
    activeController?.abort()
    activeController = null
    requestGeneration += 1
    refreshPending = false
    data.value = null
    error.value = null
    loading.value = false
    if (props.autoLoad) void probe(false)
  },
  { immediate: true }
)

watch(
  () => props.autoLoad,
  (autoLoad) => {
    if (!autoLoad) {
      activeController?.abort()
      activeController = null
      requestGeneration += 1
      loading.value = false
      return
    }
    const force = refreshPending
    refreshPending = false
    void probe(force)
  }
)

watch(
  () => props.refreshToken,
  (nextToken, previousToken) => {
    if (nextToken === previousToken) return
    if (props.autoLoad) void probe(true)
    else refreshPending = true
  }
)

onBeforeUnmount(() => {
  disposed = true
  activeController?.abort()
  activeController = null
  requestGeneration += 1
})
</script>
