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

      <div class="rounded-lg border border-blue-200 bg-blue-50/70 p-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/20 dark:text-blue-200">
        按工具的顺序操作：先填地址和凭证，再选择分组并拉取模型，最后只发一次测试请求。
        上游返回的模型、分组和 usage 会分别记录，本站用户计费规则不会被改变。
      </div>

      <div class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <div class="font-medium text-gray-900 dark:text-white">第 1 步：连接上游</div>
        <div class="grid gap-3 md:grid-cols-2">
          <Input v-model="form.upstream_base_url" label="上游地址" placeholder="https://example.com 或 https://example.com/v1" />
          <Input v-model="form.management_token" label="上游管理 Token" type="password" autocomplete="off" hint="用于读取分组 Key；不填时可直接使用 API Key" />
          <Input v-model="form.api_key" label="指定 API Key（可选）" type="password" autocomplete="off" hint="不填则按目标分组自动尝试可用 Key" />
          <Input v-model="form.upstream_group_id" label="上游分组 ID（自动选 Key 时填写）" type="number" />
        </div>
      </div>

      <div class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <div class="font-medium text-gray-900 dark:text-white">第 2 步：选择真实请求</div>
        <div class="grid gap-3 md:grid-cols-2">
          <Input v-model="form.request_model" label="实际请求模型" placeholder="例如 astra、gpt-6.1-sol" />
          <Input v-model="form.expected_group_name" label="预期上游计费组（仅用于比对）" placeholder="例如 Luna" />
        </div>
        <div>
          <label class="input-label mb-1.5 block">协议</label>
          <Select v-model="form.protocol" :options="protocolOptions" />
        </div>
        <Input v-model="form.prompt" label="测试提示词" hint="建议填写短文本，工具只请求一次且最多返回 1 token。" />
      </div>

      <details class="rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600">
        <summary class="cursor-pointer font-medium text-gray-700 dark:text-dark-200">高级账单回读（可选）</summary>
        <div class="mt-3 grid gap-3 md:grid-cols-2">
          <Input v-model="form.upstream_key_id" label="上游 Key ID" type="number" hint="填写后会读取该 Key 的 usage 前后快照。" />
        </div>
      </details>

      <div class="flex flex-wrap items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50/70 p-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/20 dark:text-emerald-200">
        <span class="font-medium">持久化测试入口</span>
        <span class="text-xs">只供管理员 Trace，不加入本站用户流量，本站计费保持原规则。</span>
        <button
          class="btn btn-secondary ml-auto"
          type="button"
          :disabled="configSaving || !configLoaded"
          @click="saveConfig(true)"
        >
          {{ configSaving ? '保存中…' : '启用测试入口' }}
        </button>
        <button
          class="btn btn-secondary"
          type="button"
          :disabled="configSaving || !configLoaded"
          @click="saveConfig(false)"
        >
          停用
        </button>
        <span v-if="configMessage" class="basis-full text-xs">{{ configMessage }}</span>
        <span v-if="configError" class="basis-full text-xs text-red-700 dark:text-red-300">{{ configError }}</span>
      </div>

      <div class="space-y-3 rounded-lg border border-red-200 bg-red-50/60 p-3 dark:border-red-900 dark:bg-red-950/20">
        <div class="text-sm font-medium text-red-800 dark:text-red-200">方法 2：POST /keys group_id 授权回归</div>
        <div class="text-xs text-red-700 dark:text-red-300">
          这是独立的授权回归，会创建并删除临时 Key；只用于验证 group_id 是否被上游接受。
        </div>
        <div class="grid gap-3 md:grid-cols-2">
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
        <button class="btn btn-primary" type="button" :disabled="loading || !canRun" @click="run">
          {{ loading ? '第 3 步：请求中…' : '第 3 步：开始 Trace' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from '@/composables/useStepUp'
import type { Account } from '@/types'
import { accountsAPI, type UpstreamTraceConfig, type UpstreamTraceResult } from '@/api/admin/accounts'

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
  upstream_group_id: '',
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
const configLoaded = ref(false)
const configSaving = ref(false)
const configMessage = ref('')
const configError = ref('')
const canRun = computed(() =>
  Boolean(form.upstream_base_url.trim() && form.request_model.trim() &&
    (form.api_key.trim() || (form.management_token.trim() && form.upstream_group_id)))
)

watch(
  () => props.show,
  (show) => {
    if (show) {
      error.value = ''
      result.value = null
      authorizationResult.value = null
      authorizationError.value = ''
      form.request_model = ''
      configLoaded.value = false
      configMessage.value = ''
      configError.value = ''
      void loadConfig()
    }
  }
)

async function loadConfig() {
  if (!props.account) return
  try {
    const config = await accountsAPI.getUpstreamTraceConfig(props.account.id)
    form.upstream_base_url = config.upstream_base_url || ''
    form.upstream_group_id = config.upstream_group_id ? String(config.upstream_group_id) : ''
    form.request_model = config.request_model || ''
    form.protocol = config.protocol || 'chat_completions'
    form.expected_group_name = config.expected_group_name || ''
    form.prompt = config.prompt || 'trace probe'
    configLoaded.value = true
  } catch (cause: any) {
    configError.value = cause?.response?.data?.message || cause?.message || '持久化配置读取失败'
  }
}

async function saveConfig(enabled: boolean) {
  if (!props.account) return
  configSaving.value = true
  configMessage.value = ''
  configError.value = ''
  try {
    const payload: UpstreamTraceConfig = {
      enabled,
      upstream_base_url: form.upstream_base_url.trim(),
      upstream_group_id: form.upstream_group_id ? Number(form.upstream_group_id) : undefined,
      request_model: form.request_model.trim(),
      protocol: form.protocol as UpstreamTraceConfig['protocol'],
      expected_group_name: form.expected_group_name.trim() || undefined,
      prompt: form.prompt.trim()
    }
    await accountsAPI.saveUpstreamTraceConfig(props.account.id, payload)
    configMessage.value = enabled ? '已启用持久化测试入口' : '已停用持久化测试入口'
  } catch (cause: any) {
    configError.value = cause?.response?.data?.message || cause?.message || '持久化配置保存失败'
  } finally {
    configSaving.value = false
  }
}

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
        upstream_group_id: form.upstream_group_id ? Number(form.upstream_group_id) : undefined,
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
