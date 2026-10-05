<template>
  <BaseDialog
    :show="show"
    title="上游 Trace / 计费复现"
    width="wide"
    @close="handleClose"
  >
    <div class="space-y-4">
      <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
        单次非流式探测，最多请求 1 个输出 token。Token 只保存在当前页面内存中，不会写入浏览器存储。
      </div>

      <div v-if="account" class="rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600">
        <div class="font-medium text-gray-900 dark:text-white">{{ account.name }}</div>
        <div class="mt-1 text-xs text-gray-500 dark:text-dark-300">
          account_id={{ account.id }} · {{ account.platform }} · {{ account.type }}
        </div>
      </div>

      <div class="grid gap-3 md:grid-cols-2">
        <Input v-model="form.upstream_base_url" label="上游 Sub2API 地址" placeholder="https://example.com" />
        <Input v-model="form.api_key" label="上游 API Key" type="password" autocomplete="off" />
        <Input v-model="form.management_token" label="管理 Token（可选，用于 Key/usage 回读）" type="password" autocomplete="off" />
        <Input v-model="form.upstream_key_id" label="上游 Key ID（可选）" type="number" />
        <Input v-model="form.request_model" label="实际请求模型" placeholder="astra / gpt-6.1-sol / luna" />
        <Input v-model="form.expected_group_name" label="期望计费组名（可选）" placeholder="Luna" />
        <div>
          <label class="input-label mb-1.5 block">协议</label>
          <Select v-model="form.protocol" :options="protocolOptions" />
        </div>
      </div>

      <Input v-model="form.prompt" label="探测提示词" />

      <div class="space-y-3 rounded-lg border border-red-200 bg-red-50/60 p-3 dark:border-red-900 dark:bg-red-950/20">
        <div class="text-sm font-medium text-red-800 dark:text-red-200">方法 2：POST /keys group_id 授权回归</div>
        <div class="text-xs text-red-700 dark:text-red-300">
          需要上游管理 Token 和目标 group_id；创建的临时 Key 会自动删除，接口受二次验证保护。
        </div>
        <div class="grid gap-3 md:grid-cols-2">
          <Input v-model="form.management_token" label="上游管理 Token" type="password" autocomplete="off" />
          <Input v-model="form.probe_group_id" label="目标 group_id" type="number" />
        </div>
        <button
          class="btn btn-secondary"
          type="button"
          :disabled="authorizationLoading || !form.management_token || !form.probe_group_id"
          @click="runAuthorizationProbe"
        >
          {{ authorizationLoading ? '回归中…' : '执行 POST /keys 授权回归' }}
        </button>
        <pre v-if="authorizationResult" class="max-h-48 overflow-auto rounded bg-gray-950 p-2 text-xs text-green-200">{{ JSON.stringify(authorizationResult, null, 2) }}</pre>
        <div v-if="authorizationError" class="text-xs text-red-700 dark:text-red-300">{{ authorizationError }}</div>
      </div>

      <div v-if="error" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
        {{ error }}
      </div>

      <div v-if="result" class="space-y-3">
        <div class="flex flex-wrap gap-2 text-xs">
          <span class="rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">trace={{ result.trace_id }}</span>
          <span class="rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">{{ result.request_path }}</span>
          <span class="rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">model={{ result.request_model }}</span>
        </div>
        <pre class="max-h-96 overflow-auto rounded-lg bg-gray-950 p-3 text-xs text-green-200">{{ JSON.stringify(result, null, 2) }}</pre>
      </div>
    </div>

    <TotpStepUpDialog :controller="authorizationStepUp" />

    <template #footer>
      <div class="flex justify-end gap-2">
        <button class="btn btn-secondary" type="button" @click="handleClose">关闭</button>
        <button class="btn btn-primary" type="button" :disabled="loading" @click="run">
          {{ loading ? '探测中…' : '开始 Trace' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from '@/composables/useStepUp'
import type { Account } from '@/types'
import { accountsAPI, type UpstreamTraceResult } from '@/api/admin/accounts'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const protocolOptions = [
  { value: 'chat_completions', label: 'OpenAI Chat Completions' },
  { value: 'responses', label: 'OpenAI Responses' },
  { value: 'messages', label: 'Anthropic Messages' }
]

const form = reactive({
  upstream_base_url: '',
  api_key: '',
  management_token: '',
  probe_group_id: '',
  upstream_key_id: '',
  request_model: '',
  expected_group_name: '',
  protocol: 'chat_completions',
  prompt: 'trace probe',
  run_authorization_probe: false
})
const loading = ref(false)
const error = ref('')
const result = ref<UpstreamTraceResult | null>(null)
const authorizationLoading = ref(false)
const authorizationError = ref('')
const authorizationResult = ref<unknown>(null)
const authorizationStepUp = useStepUp()

watch(
  () => props.show,
  (show) => {
    if (show) {
      error.value = ''
      result.value = null
      authorizationResult.value = null
      authorizationError.value = ''
      form.request_model = ''
    }
  }
)

function handleClose() {
  if (loading.value) return
  emit('close')
}

async function run() {
  if (!props.account) return
  const accountId = props.account.id
  error.value = ''
  result.value = null
  loading.value = true
  try {
    result.value = await authorizationStepUp.run(() =>
      accountsAPI.traceUpstream(accountId, {
        upstream_base_url: form.upstream_base_url.trim(),
        api_key: form.api_key,
        management_token: form.management_token || undefined,
        upstream_key_id: form.upstream_key_id ? Number(form.upstream_key_id) : undefined,
        request_model: form.request_model.trim(),
        protocol: form.protocol as 'chat_completions' | 'responses' | 'messages',
        prompt: form.prompt.trim(),
        expected_group_name: form.expected_group_name.trim() || undefined
      })
    )
  } catch (cause: any) {
    if (isStepUpCancelled(cause)) return
    if (isStepUpBlocked(cause)) {
      error.value = stepUpBlockReason(cause) || '需要启用二次验证'
      return
    }
    error.value = cause?.response?.data?.message || cause?.message || 'Trace failed'
  } finally {
    loading.value = false
  }
}

async function runAuthorizationProbe() {
  authorizationError.value = ''
  authorizationResult.value = null
  authorizationLoading.value = true
  try {
    authorizationResult.value = await authorizationStepUp.run(() =>
      accountsAPI.probeUpstreamAuthorization({
        upstream_base_url: form.upstream_base_url.trim(),
        management_token: form.management_token,
        probe_group_id: Number(form.probe_group_id)
      })
    )
  } catch (cause: any) {
    if (isStepUpCancelled(cause)) return
    if (isStepUpBlocked(cause)) {
      authorizationError.value = stepUpBlockReason(cause) || '需要启用二次验证'
      return
    }
    authorizationError.value = cause?.response?.data?.message || cause?.message || 'Authorization probe failed'
  } finally {
    authorizationLoading.value = false
  }
}
</script>
