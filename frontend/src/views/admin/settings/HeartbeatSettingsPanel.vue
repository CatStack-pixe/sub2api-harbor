<template>
  <section class="space-y-6" data-testid="heartbeat-settings-panel">
    <div v-if="loading" class="card flex items-center justify-center p-10">
      <div class="h-7 w-7 animate-spin rounded-full border-b-2 border-primary-600" />
    </div>

    <template v-else>
      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
        {{ errorMessage }}
      </div>

      <div class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.settings.heartbeat.title') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.heartbeat.description') }}
              </p>
            </div>
            <Toggle v-model="form.enabled" />
          </div>
        </div>

        <div class="space-y-6 p-6">
          <div class="grid gap-4 md:grid-cols-2">
            <label class="block">
              <span class="form-label">{{ t('admin.settings.heartbeat.vaultUrl') }}</span>
              <input v-model.trim="form.vault_url" class="input mt-1 w-full" type="url" autocomplete="off" />
            </label>
            <label class="block">
              <span class="form-label">{{ t('admin.settings.heartbeat.allowedSourceIps') }}</span>
              <input v-model="sourceIpsText" class="input mt-1 w-full" type="text" :placeholder="t('admin.settings.heartbeat.allowedSourceIpsPlaceholder')" />
              <span class="form-hint">{{ t('admin.settings.heartbeat.allowedSourceIpsHint') }}</span>
            </label>
          </div>

          <label class="flex items-center gap-3 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="form.allow_insecure_vault" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span>{{ t('admin.settings.heartbeat.allowInsecureVault') }}</span>
          </label>

          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <label v-for="field in numericFields" :key="field.key" class="block">
              <span class="form-label">{{ t(`admin.settings.heartbeat.${field.label}`) }}</span>
              <input v-model.number="form[field.key]" class="input mt-1 w-full" type="number" :min="field.min" :max="field.max" :step="1" />
            </label>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <div class="flex items-center justify-between gap-4">
            <div>
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('admin.settings.heartbeat.targetsTitle') }}
              </h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.heartbeat.targetsDescription') }}
              </p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm" @click="addTarget">
              <Icon name="plus" size="sm" />
              {{ t('admin.settings.heartbeat.addTarget') }}
            </button>
          </div>
        </div>

        <div class="space-y-4 p-6">
          <label class="block max-w-xl">
            <span class="form-label">{{ t('admin.settings.heartbeat.defaultGroup') }}</span>
            <Select
              v-model="form.default_group_id"
              class="mt-1"
              :options="groupOptions"
              :placeholder="t('admin.settings.heartbeat.selectGroup')"
              :aria-label="t('admin.settings.heartbeat.defaultGroup')"
              searchable
            />
          </label>

          <div v-if="form.targets.length === 0" class="rounded-lg border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
            {{ t('admin.settings.heartbeat.noTargets') }}
          </div>

          <div v-for="(target, index) in form.targets" :key="`${target.group_id}-${index}`" class="grid gap-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end">
            <label class="block">
              <span class="form-label">{{ t('admin.settings.heartbeat.accountGroup') }}</span>
              <Select
                v-model="target.group_id"
                class="mt-1"
                :options="groupOptions"
                :placeholder="t('admin.settings.heartbeat.selectGroup')"
                :aria-label="t('admin.settings.heartbeat.accountGroup')"
                searchable
              />
            </label>
            <label class="block">
              <span class="form-label">{{ t('admin.settings.heartbeat.proxyGroup') }}</span>
              <Select
                v-model="target.proxy_group_id"
                class="mt-1"
                :options="proxyGroupOptions"
                :placeholder="t('admin.settings.heartbeat.selectProxyGroup')"
                :aria-label="t('admin.settings.heartbeat.proxyGroup')"
                searchable
              />
            </label>
            <button
              type="button"
              class="btn btn-secondary self-end px-3 text-red-600 hover:text-red-700 dark:text-red-400"
              :aria-label="t('admin.settings.heartbeat.removeTarget')"
              :title="t('admin.settings.heartbeat.removeTarget')"
              @click="removeTarget(index)"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('admin.settings.heartbeat.statusTitle') }}
            </h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.heartbeat.source') }}: {{ status.config_source || '-' }}
            </p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="refreshing" :aria-label="t('admin.settings.heartbeat.refresh')" :title="t('admin.settings.heartbeat.refresh')" @click="refreshStatus">
            <Icon name="refresh" size="sm" :class="refreshing && 'animate-spin'" />
            <span>{{ t('admin.settings.heartbeat.refresh') }}</span>
          </button>
        </div>
        <div class="grid gap-3 p-6 sm:grid-cols-2 lg:grid-cols-5">
          <div v-for="item in queueItems" :key="item.key" class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/60">
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.settings.heartbeat.queue.${item.key}`) }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ status[item.key] }}</p>
          </div>
        </div>
        <div class="grid gap-4 border-t border-gray-100 px-6 py-4 text-sm dark:border-dark-700 md:grid-cols-2">
          <div>
            <span class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.heartbeat.lastHeartbeat') }}</span>
            <span class="ml-2 text-gray-900 dark:text-white">{{ formatDate(status.last_heartbeat_at) }}</span>
          </div>
          <div>
            <span class="text-gray-500 dark:text-gray-400">{{ t('admin.settings.heartbeat.lastError') }}</span>
            <span class="ml-2 text-red-600 dark:text-red-400">{{ status.last_error || '-' }}</span>
          </div>
        </div>
      </div>

      <div v-if="saveMessage" class="rounded-lg border border-green-200 bg-green-50 p-4 text-sm text-green-700 dark:border-green-800 dark:bg-green-900/20 dark:text-green-300">
        {{ saveMessage }}
      </div>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" :disabled="saving || loading" @click="save">
          <Icon name="check" size="sm" />
          {{ saving ? t('admin.settings.saving') : t('admin.settings.heartbeat.save') }}
        </button>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import type { HeartbeatConfig, HeartbeatConfigUpdate, HeartbeatOptions, HeartbeatStatus, HeartbeatTarget } from '@/api/admin/heartbeat'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()

type NumericKey = 'worker_count' | 'proxy_probe_workers' | 'proxy_probe_sample_size' | 'proxy_probe_timeout_seconds' | 'proxy_sweep_ttl_seconds' | 'max_attempts'

const numericFields: Array<{ key: NumericKey; label: string; min: number; max: number }> = [
  { key: 'worker_count', label: 'workerCount', min: 1, max: 64 },
  { key: 'proxy_probe_workers', label: 'proxyProbeWorkers', min: 1, max: 128 },
  { key: 'proxy_probe_sample_size', label: 'proxyProbeSampleSize', min: 1, max: 10000 },
  { key: 'proxy_probe_timeout_seconds', label: 'proxyProbeTimeout', min: 1, max: 60 },
  { key: 'proxy_sweep_ttl_seconds', label: 'proxySweepTtl', min: 1, max: 86400 },
  { key: 'max_attempts', label: 'maxAttempts', min: 1, max: 20 },
]

const queueItems = [
  { key: 'queued' as const },
  { key: 'processing' as const },
  { key: 'retry' as const },
  { key: 'failed' as const },
  { key: 'complete' as const },
]

const loading = ref(true)
const saving = ref(false)
const refreshing = ref(false)
const errorMessage = ref('')
const saveMessage = ref('')
const sourceIpsText = ref('')
const options = reactive<HeartbeatOptions>({ groups: [], proxy_groups: [] })
const form = reactive<HeartbeatConfigUpdate>({
  enabled: false,
  vault_url: '',
  allow_insecure_vault: false,
  allowed_source_ips: [],
  default_group_id: 0,
  targets: [],
  worker_count: 2,
  proxy_probe_workers: 10,
  proxy_probe_sample_size: 100,
  proxy_probe_timeout_seconds: 5,
  proxy_sweep_ttl_seconds: 300,
  max_attempts: 5,
})
const status = reactive<HeartbeatStatus>({
  enabled: false,
  running: false,
  config_source: '',
  last_heartbeat_at: null,
  queued: 0,
  processing: 0,
  retry: 0,
  failed: 0,
  complete: 0,
  last_error: '',
  last_error_at: null,
})

const groupOptions = computed(() => options.groups.map(group => ({
  value: group.id,
  label: `${group.name} [${group.platform}] (#${group.id})`,
})))

const proxyGroupOptions = computed(() => options.proxy_groups.map(group => ({
  value: group.id,
  label: `${group.name} (#${group.id}) · ${group.active_proxy_count}`,
})))

function applyConfig(config: HeartbeatConfig): void {
  Object.assign(form, {
    enabled: config.enabled,
    vault_url: config.vault_url,
    allow_insecure_vault: config.allow_insecure_vault,
    allowed_source_ips: [...(config.allowed_source_ips || [])],
    default_group_id: config.default_group_id,
    targets: (config.targets || []).map(target => ({ ...target })),
    worker_count: config.worker_count,
    proxy_probe_workers: config.proxy_probe_workers,
    proxy_probe_sample_size: config.proxy_probe_sample_size,
    proxy_probe_timeout_seconds: config.proxy_probe_timeout_seconds,
    proxy_sweep_ttl_seconds: config.proxy_sweep_ttl_seconds,
    max_attempts: config.max_attempts,
  })
  sourceIpsText.value = form.allowed_source_ips.join(', ')
  if (config.status) {
    Object.assign(status, config.status)
  }
}

function applyOptions(next: HeartbeatOptions): void {
  options.groups = next.groups || []
  options.proxy_groups = next.proxy_groups || []
}

async function load(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const [config, nextOptions, nextStatus] = await Promise.all([
      adminAPI.heartbeat.getConfig(),
      adminAPI.heartbeat.getOptions(),
      adminAPI.heartbeat.getStatus(),
    ])
    applyConfig(config)
    applyOptions(nextOptions)
    Object.assign(status, nextStatus)
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error)
  } finally {
    loading.value = false
  }
}

async function refreshStatus(): Promise<void> {
  refreshing.value = true
  errorMessage.value = ''
  try {
    Object.assign(status, await adminAPI.heartbeat.getStatus())
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error)
  } finally {
    refreshing.value = false
  }
}

function normalizeSourceIps(): string[] {
  return sourceIpsText.value
    .split(/[\s,]+/)
    .map(value => value.trim())
    .filter(Boolean)
    .filter((value, index, values) => values.indexOf(value) === index)
}

function addTarget(): void {
  const usedGroups = new Set(form.targets.map(target => target.group_id))
  const group = options.groups.find(option => !usedGroups.has(option.id)) || options.groups[0]
  const proxyGroup = options.proxy_groups.find(option => option.id > 0) || options.proxy_groups[0]
  form.targets.push({
    group_id: group?.id || 0,
    proxy_group_id: proxyGroup?.id || 0,
  })
  if (!form.default_group_id && group) {
    form.default_group_id = group.id
  }
}

function removeTarget(index: number): void {
  if (form.targets.length <= 1) {
    return
  }
  const removed = form.targets[index]
  form.targets.splice(index, 1)
  if (removed && removed.group_id === form.default_group_id) {
    form.default_group_id = form.targets[0]?.group_id || 0
  }
}

function buildPayload(): HeartbeatConfigUpdate {
  return {
    enabled: form.enabled,
    vault_url: form.vault_url.trim(),
    allow_insecure_vault: form.allow_insecure_vault,
    allowed_source_ips: normalizeSourceIps(),
    default_group_id: Number(form.default_group_id),
    targets: form.targets.map((target: HeartbeatTarget) => ({
      group_id: Number(target.group_id),
      proxy_group_id: Number(target.proxy_group_id),
    })),
    worker_count: Number(form.worker_count),
    proxy_probe_workers: Number(form.proxy_probe_workers),
    proxy_probe_sample_size: Number(form.proxy_probe_sample_size),
    proxy_probe_timeout_seconds: Number(form.proxy_probe_timeout_seconds),
    proxy_sweep_ttl_seconds: Number(form.proxy_sweep_ttl_seconds),
    max_attempts: Number(form.max_attempts),
  }
}

async function save(): Promise<void> {
  saving.value = true
  errorMessage.value = ''
  saveMessage.value = ''
  try {
    const updated = await adminAPI.heartbeat.updateConfig(buildPayload())
    applyConfig(updated)
    saveMessage.value = t('admin.settings.heartbeat.saved')
    await refreshStatus()
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error)
  } finally {
    saving.value = false
  }
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

onMounted(load)
</script>
