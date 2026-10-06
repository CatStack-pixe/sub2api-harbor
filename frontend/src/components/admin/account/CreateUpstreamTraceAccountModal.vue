<template>
  <BaseDialog :show="show" title="创建持久化测试账户" width="wide" @close="emit('close')">
    <div class="space-y-4">
      <div class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/20 dark:text-blue-200">
        这是一个独立账户，不会修改当前账户，也不会进入本站用户调度或本站计费。
      </div>
      <div class="grid gap-3 md:grid-cols-2">
        <Input v-model="form.name" label="测试账户名称" placeholder="例如 upstream-astra-test" />
        <Input v-model="form.upstream_base_url" label="上游地址" placeholder="https://example.com/v1" />
        <Input v-model="form.upstream_group_id" label="上游 group_id" type="number" />
        <Input v-model="form.request_model" label="测试模型" placeholder="gpt-6.1-sol" />
        <div>
          <label class="input-label mb-1.5 block">账户认证类型</label>
          <Select v-model="form.type" :options="typeOptions" />
        </div>
        <div>
          <label class="input-label mb-1.5 block">上游协议</label>
          <Select v-model="form.protocol" :options="protocolOptions" />
        </div>
      </div>
      <Input v-model="form.api_key" label="API Key（API Key 类型可填）" type="password" autocomplete="off" />
      <div v-if="form.type === 'oauth'" class="grid gap-3 md:grid-cols-3">
        <Input v-model="form.login_email" label="OAuth 登录邮箱" autocomplete="username" />
        <Input v-model="form.login_password" label="OAuth 登录密码" type="password" autocomplete="current-password" />
        <Input v-model="form.login_totp" label="TOTP（可选）" type="password" autocomplete="one-time-code" />
      </div>
      <label class="flex items-center gap-2 text-sm">
        <input v-model="form.enabled" type="checkbox" />
        创建后立即启用测试账户
      </label>
      <div v-if="error" class="rounded bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button class="btn btn-secondary" type="button" @click="emit('close')">取消</button>
        <button class="btn btn-primary" type="button" :disabled="loading" @click="create">
          {{ loading ? '创建中…' : '创建测试账户' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import { accountsAPI } from '@/api/admin/accounts'

defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created'): void }>()
const typeOptions = [
  { value: 'oauth', label: 'OAuth' },
  { value: 'apikey', label: 'API Key' }
]
const protocolOptions = [
  { value: 'chat_completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses' },
  { value: 'messages', label: 'Anthropic Messages' }
]
const form = reactive({
  name: '',
  upstream_base_url: '',
  upstream_group_id: '',
  request_model: '',
  protocol: 'chat_completions',
  type: 'apikey' as 'oauth' | 'apikey',
  api_key: '',
  login_email: '',
  login_password: '',
  login_totp: '',
  enabled: true
})
const loading = ref(false)
const error = ref('')

async function create() {
  error.value = ''
  loading.value = true
  try {
    await accountsAPI.createUpstreamTraceAccount({
      name: form.name.trim(),
      platform: 'openai',
      type: form.type,
      credentials: {
        ...(form.api_key ? { api_key: form.api_key } : {}),
        ...(form.login_email ? { login_email: form.login_email } : {}),
        ...(form.login_password ? { login_password: form.login_password } : {}),
        ...(form.login_totp ? { login_totp: form.login_totp } : {}),
        base_url: form.upstream_base_url.trim()
      },
      upstream_base_url: form.upstream_base_url.trim(),
      upstream_group_id: form.upstream_group_id ? Number(form.upstream_group_id) : undefined,
      request_model: form.request_model.trim(),
      protocol: form.protocol as 'chat_completions' | 'responses' | 'messages',
      enabled: form.enabled
    })
    emit('created')
    emit('close')
  } catch (cause: any) {
    error.value = cause?.response?.data?.message || cause?.message || '创建失败'
  } finally {
    loading.value = false
  }
}
</script>
