<template>
  <div class="mb-4 space-y-3 border-b border-gray-200 pb-4 dark:border-dark-600">
    <div>
      <label class="input-label">{{ t('admin.accounts.tokenrhythm.session') }}</label>
      <div class="flex flex-col gap-2 sm:flex-row">
        <input
          v-model="sess"
          type="password"
          class="input min-w-0 flex-1 font-mono"
          autocomplete="off"
          spellcheck="false"
          data-testid="tokenrhythm-session-input"
          :placeholder="t('admin.accounts.tokenrhythm.sessionPlaceholder')"
          @keydown.enter.prevent="resolveSession"
        />
        <div class="flex shrink-0 gap-2">
          <button
            type="button"
            class="btn btn-secondary"
            data-testid="tokenrhythm-session-resolve"
            :disabled="loading || !sess.trim()"
            @click="resolveSession"
          >
            <Icon name="sync" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{
              loading
                ? t('admin.accounts.tokenrhythm.resolvingSession')
                : t('admin.accounts.tokenrhythm.resolveSession')
            }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            data-testid="tokenrhythm-manage-key"
            @click="openKeyDialog"
          >
            <Icon name="key" size="sm" />
            {{ t('admin.accounts.tokenrhythm.manageKey') }}
          </button>
        </div>
      </div>
      <p class="input-hint">{{ t('admin.accounts.tokenrhythm.sessionHint') }}</p>
    </div>

    <div v-if="result" class="space-y-2" data-testid="tokenrhythm-referral-result">
      <label class="input-label">{{ t('admin.accounts.tokenrhythm.inviteLink') }}</label>
      <div class="flex gap-2">
        <input :value="result.referral_link" class="input min-w-0 flex-1 font-mono" readonly />
        <a
          :href="result.referral_link"
          target="_blank"
          rel="noopener noreferrer"
          class="btn btn-secondary shrink-0"
          :title="t('admin.accounts.tokenrhythm.inviteLinkOpen')"
          :aria-label="t('admin.accounts.tokenrhythm.inviteLinkOpen')"
        >
          <Icon name="externalLink" size="sm" />
        </a>
      </div>
      <p class="input-hint">
        {{ t('admin.accounts.tokenrhythm.inviteCode', { code: result.referral_code }) }}
      </p>
    </div>
  </div>

  <BaseDialog
    :show="keyDialogOpen"
    :title="t('admin.accounts.tokenrhythm.manageKey')"
    width="normal"
    :z-index="60"
    @close="closeKeyDialog"
  >
    <form class="space-y-4" data-testid="tokenrhythm-key-form" @submit.prevent="createApiKey">
      <div>
        <label class="input-label">{{ t('admin.accounts.tokenrhythm.session') }}</label>
        <input
          v-model="keySess"
          type="password"
          class="input font-mono"
          autocomplete="off"
          spellcheck="false"
          data-testid="tokenrhythm-key-session-input"
          :placeholder="t('admin.accounts.tokenrhythm.sessionPlaceholder')"
        />
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.tokenrhythm.keyName') }}</label>
        <input
          v-model="keyName"
          type="text"
          maxlength="20"
          class="input"
          data-testid="tokenrhythm-key-name-input"
          :placeholder="t('admin.accounts.tokenrhythm.keyNamePlaceholder')"
        />
      </div>
      <p class="input-hint">{{ t('admin.accounts.tokenrhythm.manageKeyHint') }}</p>

      <div v-if="createdKey" class="space-y-3 rounded-md border border-amber-300 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-900/20">
        <p class="text-sm font-medium text-amber-800 dark:text-amber-200">
          {{ t('admin.accounts.tokenrhythm.keyCreatedOnce') }}
        </p>
        <div class="flex gap-2">
          <input :value="createdKey" readonly class="input min-w-0 flex-1 font-mono text-xs" />
          <button
            type="button"
            class="btn btn-secondary shrink-0"
            :title="t('admin.accounts.tokenrhythm.copyKey')"
            :aria-label="t('admin.accounts.tokenrhythm.copyKey')"
            @click="copyCreatedKey"
          >
            <Icon name="copy" size="sm" />
          </button>
        </div>
      </div>

      <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-600">
        <button type="button" class="btn btn-secondary" :disabled="keyLoading" @click="closeKeyDialog">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          class="btn btn-primary"
          data-testid="tokenrhythm-create-key"
          :disabled="keyLoading || !keySess.trim() || !keyName.trim()"
        >
          <Icon name="key" size="sm" :class="keyLoading ? 'animate-spin' : ''" />
          {{ keyLoading ? t('admin.accounts.tokenrhythm.creatingKey') : t('admin.accounts.tokenrhythm.createKey') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CreateTokenRhythmAPIKeyResult, TokenRhythmSessionResult } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = withDefaults(defineProps<{
  proxyId?: number | null
  accountName?: string
}>(), {
  accountName: ''
})

const emit = defineEmits<{
  resolved: [cookie: string]
  apiKeyCreated: [result: CreateTokenRhythmAPIKeyResult]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const sess = ref('')
const loading = ref(false)
const result = ref<TokenRhythmSessionResult | null>(null)
const keyDialogOpen = ref(false)
const keySess = ref('')
const keyName = ref('')
const keyLoading = ref(false)
const createdKey = ref('')
const defaultKeyName = computed(() => {
  const accountName = props.accountName.trim()
  return (accountName ? `sub2api-${accountName}` : 'sub2api-tokenrhythm').slice(0, 20)
})

const openKeyDialog = () => {
  keyName.value = defaultKeyName.value
  keySess.value = ''
  createdKey.value = ''
  keyDialogOpen.value = true
}

const closeKeyDialog = () => {
  if (keyLoading.value) return
  keyDialogOpen.value = false
  keySess.value = ''
  createdKey.value = ''
}

const copyCreatedKey = async () => {
  if (!createdKey.value) return
  try {
    await navigator.clipboard.writeText(createdKey.value)
    appStore.showSuccess(t('admin.accounts.tokenrhythm.keyCopied'))
  } catch {
    appStore.showError(t('admin.accounts.tokenrhythm.keyCopyFailed'))
  }
}

const createApiKey = async () => {
  const sessValue = keySess.value.trim()
  const nameValue = keyName.value.trim()
  if (!sessValue || !nameValue || keyLoading.value) return

  keyLoading.value = true
  createdKey.value = ''
  try {
    const generated = await adminAPI.accounts.createTokenRhythmAPIKey({
      sess: sessValue,
      name: nameValue,
      proxy_id: props.proxyId ?? undefined
    })
    createdKey.value = generated.api_key
    emit('apiKeyCreated', generated)
    tokenRhythmCookieFromGenerated(generated.tokenrhythm_cookie)
    keySess.value = ''
    appStore.showSuccess(t('admin.accounts.tokenrhythm.keyCreated'))
  } catch (error: any) {
    appStore.showError(error.response?.data?.message || t('admin.accounts.tokenrhythm.keyCreateFailed'))
  } finally {
    keyLoading.value = false
  }
}

const tokenRhythmCookieFromGenerated = (cookie: string) => {
  emit('resolved', cookie)
}

const resolveSession = async () => {
  const value = sess.value.trim()
  if (!value || loading.value) return

  loading.value = true
  result.value = null
  try {
    const resolved = await adminAPI.accounts.resolveTokenRhythmSession({
      sess: value,
      proxy_id: props.proxyId ?? undefined
    })
    result.value = resolved
    emit('resolved', resolved.tokenrhythm_cookie)
    sess.value = ''
    appStore.showSuccess(t('admin.accounts.tokenrhythm.sessionResolved'))
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message || t('admin.accounts.tokenrhythm.sessionResolveFailed')
    )
  } finally {
    loading.value = false
  }
}
</script>
