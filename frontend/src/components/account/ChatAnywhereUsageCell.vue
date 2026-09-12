<template>
  <div data-test="chatanywhere-usage" class="space-y-1" :title="t('admin.accounts.usageWindow.chatAnywhereSourceHint')">
    <div v-if="loading" class="space-y-1.5">
      <div v-for="label in ['7d', '1d']" :key="label" class="flex items-center gap-1">
        <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
      </div>
    </div>
    <div v-else-if="error" class="text-xs text-red-500">
      {{ error }}
    </div>
    <template v-else-if="usageInfo">
      <UsageProgressBar
        v-if="weeklyUsage"
        label="7d"
        :utilization="weeklyUsage.utilization"
        :resets-at="weeklyUsage.resets_at"
        :usage-display="weeklyDisplay"
        :usage-display-title="t('admin.accounts.usageWindow.chatAnywhereWeeklyHint')"
        color="emerald"
      />
      <UsageProgressBar
        v-if="dailyUsage"
        label="1d"
        :utilization="dailyUsage.utilization"
        :resets-at="dailyUsage.resets_at"
        :usage-display="dailyDisplay"
        :usage-display-title="t('admin.accounts.usageWindow.chatAnywhereDailyHint')"
        color="indigo"
      />
      <div
        v-if="quotaStatus === 'weekly_exhausted'"
        data-test="chatanywhere-weekly-exhausted"
        class="truncate text-[10px] text-red-600 dark:text-red-400"
        :title="providerError || t('admin.accounts.usageWindow.chatAnywhereProviderError')"
      >
        {{ t('admin.accounts.usageWindow.chatAnywhereWeeklyExhausted') }}
      </div>
      <div
        v-else-if="quotaStatus === 'error'"
        data-test="chatanywhere-provider-error"
        class="truncate text-[10px] text-amber-600 dark:text-amber-400"
        :title="providerError || t('admin.accounts.usageWindow.chatAnywhereProviderError')"
      >
        {{ t('admin.accounts.usageWindow.chatAnywhereProviderError') }}
      </div>
    </template>
    <div v-else class="text-xs text-gray-400">-</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account, AccountUsageInfo, UsageProgress } from '@/types'
import { formatCompactNumber } from '@/utils/format'
import UsageProgressBar from './UsageProgressBar.vue'

const props = withDefaults(
  defineProps<{
    account: Account
    usageInfo?: AccountUsageInfo | null
    loading?: boolean
    error?: string | null
  }>(),
  {
    usageInfo: null,
    loading: false,
    error: null
  }
)

const { t } = useI18n()

const weeklyUsage = computed(() => props.usageInfo?.chatanywhere_weekly ?? null)
const dailyUsage = computed(() => props.usageInfo?.chatanywhere_daily ?? null)
const quotaStatus = computed(() => props.usageInfo?.chatanywhere_quota_status ?? '')

const providerError = computed(() => {
  if (props.account.status !== 'error') return null
  const message = props.account.error_message?.trim()
  return message || null
})

const formatQuotaNumber = (value: number | null | undefined) => {
  if (value == null || !Number.isFinite(value)) return '0'
  return Math.round(Math.max(0, value)).toLocaleString('en-US')
}

const getUsedTokens = (usage: UsageProgress | null) =>
  usage?.used_tokens ?? usage?.window_stats?.tokens ?? 0

const getUsedRequests = (usage: UsageProgress | null) =>
  usage?.used_requests ?? usage?.window_stats?.requests ?? 0

const weeklyDisplay = computed(() => {
  if (!weeklyUsage.value) return null
  const used = getUsedTokens(weeklyUsage.value)
  const limit = weeklyUsage.value.limit_tokens ?? 50000
  return formatQuotaNumber(used) + ' / ' + formatQuotaNumber(limit) + ' ' + t('admin.accounts.usageWindow.chatAnywherePoints')
})

const dailyDisplay = computed(() => {
  if (!dailyUsage.value) return null
  const used = getUsedRequests(dailyUsage.value)
  const limit = dailyUsage.value.limit_requests ?? 100
  return formatCompactNumber(used, { allowBillions: false }) + ' / ' + formatQuotaNumber(limit) + ' ' + t('admin.accounts.usageWindow.chatAnywhereRequests')
})
</script>
