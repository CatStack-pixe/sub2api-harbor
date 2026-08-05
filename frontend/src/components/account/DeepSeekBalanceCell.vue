<template>
  <div v-if="visible" class="space-y-1">
    <button
      type="button"
      class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-indigo-700 transition-colors hover:bg-indigo-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-indigo-300 dark:hover:bg-indigo-900/30"
      :disabled="loading"
      :title="t('admin.accounts.usageWindow.deepseekProbeTooltip')"
      @click="probe"
    >
      <Icon name="refresh" size="xs" :class="loading ? 'animate-spin' : ''" />
      {{ t('admin.accounts.usageWindow.deepseekProbe') }}
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
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { DeepSeekBalanceResult } from '@/api/admin/accounts'
import type { Account } from '@/types'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const visible = computed(() => props.account.platform === 'deepseek')
const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<DeepSeekBalanceResult | null>(null)

const extractErrorMessage = (value: unknown): string => {
  const err = value as { message?: string; reason?: string; response?: { data?: { message?: string; error?: string } } }
  return err?.response?.data?.message || err?.response?.data?.error || err?.message || err?.reason || t('common.error')
}

const summary = computed(() => {
  if (!data.value) return ''
  if (!data.value.is_available) return t('admin.accounts.usageWindow.deepseekUnavailable')
  const balances = data.value.balance_infos
    .filter((info) => info.currency && info.total_balance !== '')
    .map((info) => `${info.currency} ${info.total_balance}`)
  return balances.length > 0 ? balances.join(' | ') : t('admin.accounts.usageWindow.deepseekNoBalance')
})

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}...` : error.value
})

const probe = async () => {
  if (loading.value) return
  loading.value = true
  error.value = null
  try {
    data.value = await adminAPI.accounts.getDeepSeekBalance(props.account.id)
  } catch (value) {
    error.value = extractErrorMessage(value)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.account.id,
  () => {
    data.value = null
    error.value = null
    loading.value = false
  }
)
</script>
