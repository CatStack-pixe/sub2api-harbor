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

    <div v-if="accountId || credentialCookie || sess.trim()" class="space-y-3 rounded-md border border-gray-200 p-3 dark:border-dark-600">
      <div class="flex items-center justify-between gap-3">
        <div>
          <p class="text-sm font-medium">{{ t('admin.accounts.tokenrhythm.keyInventory') }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.tokenrhythm.keyInventoryHint') }}</p>
        </div>
        <button type="button" class="btn btn-secondary shrink-0" data-testid="tokenrhythm-list-keys" :disabled="inventoryLoading" @click="loadKeys">
          <Icon name="sync" size="sm" :class="inventoryLoading ? 'animate-spin' : ''" />
          {{ inventoryLoading ? t('admin.accounts.tokenrhythm.loadingKeys') : t('admin.accounts.tokenrhythm.listKeys') }}
        </button>
      </div>
      <div v-if="inventoryLoaded" class="space-y-2" data-testid="tokenrhythm-key-inventory">
        <div v-if="inventoryError" class="rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ inventoryError }}</div>
        <div v-else-if="!keys.length" class="rounded-md bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ t('admin.accounts.tokenrhythm.noKeys') }}</div>
        <template v-else>
          <div v-for="item in keys" :key="item.id" class="flex flex-col gap-2 rounded-md border border-gray-200 p-3 dark:border-dark-600 sm:flex-row sm:items-center sm:justify-between">
            <div class="min-w-0">
              <p class="truncate text-sm font-medium">{{ item.name }}</p>
              <p class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ item.masked_key || item.key_prefix || item.id }}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.status || t('admin.accounts.tokenrhythm.unknownStatus') }}</p>
            </div>
            <div class="flex shrink-0 gap-2">
              <button v-if="item.status !== 'disabled'" type="button" class="btn btn-secondary" :disabled="actionId === item.id" :data-testid="`tokenrhythm-disable-${item.id}`" @click="disableKey(item)">
                {{ actionId === item.id ? t('admin.accounts.tokenrhythm.working') : t('admin.accounts.tokenrhythm.disableKey') }}
              </button>
              <button type="button" class="btn btn-danger" :disabled="actionId === item.id" :data-testid="`tokenrhythm-delete-${item.id}`" @click="deleteKey(item)">
                {{ t('common.delete') }}
              </button>
            </div>
          </div>
        </template>
      </div>
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
          v-if="!accountId"
          v-model="keySess"
          type="password"
          class="input font-mono"
          autocomplete="off"
          spellcheck="false"
          data-testid="tokenrhythm-key-session-input"
          :placeholder="t('admin.accounts.tokenrhythm.sessionPlaceholder')"
        />
        <p v-else class="rounded-md bg-gray-50 p-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('admin.accounts.tokenrhythm.accountCredentialSource') }}</p>
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
          :disabled="keyLoading || (!accountId && !keySess.trim() && !credentialCookie.trim()) || !keyName.trim()"
        >
          <Icon name="key" size="sm" :class="keyLoading ? 'animate-spin' : ''" />
          {{ keyLoading ? t('admin.accounts.tokenrhythm.creatingKey') : t('admin.accounts.tokenrhythm.createKey') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CreateTokenRhythmAPIKeyResult, TokenRhythmAPIKeyListItem, TokenRhythmSessionResult } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = withDefaults(defineProps<{
  proxyId?: number | null
  accountName?: string
  accountId?: number | null
  credentialCookie?: string
}>(), {
  accountName: '',
  accountId: null,
  credentialCookie: ''
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
const resolvedCookie = ref('')
const keys = ref<TokenRhythmAPIKeyListItem[]>([])
const inventoryLoading = ref(false)
const inventoryLoaded = ref(false)
const inventoryError = ref('')
const actionId = ref<string | null>(null)

watch(() => props.accountId, () => {
  resolvedCookie.value = ''
  keys.value = []
  inventoryLoaded.value = false
  inventoryError.value = ''
})
const defaultKeyName = computed(() => {
  const accountName = props.accountName.trim()
  return (accountName ? `sub2api-${accountName}` : 'sub2api-tokenrhythm').slice(0, 20)
})

const openKeyDialog = () => {
  keyName.value = defaultKeyName.value
  keySess.value = sess.value.trim()
  createdKey.value = ''
  keyDialogOpen.value = true
}

const credentialRequest = () => {
  const sessValue = sess.value.trim()
  const cookieValue = resolvedCookie.value.trim() || props.credentialCookie.trim()
  const useStoredAccountCredential = Boolean(props.accountId) && !sessValue && !cookieValue
  return {
    account_id: useStoredAccountCredential ? props.accountId ?? undefined : undefined,
    sess: useStoredAccountCredential ? undefined : sessValue || undefined,
    cookie: useStoredAccountCredential ? undefined : cookieValue || undefined,
    proxy_id: props.proxyId ?? undefined
  }
}

const applyCredentialResult = (cookie?: string, warning?: string) => {
  if (cookie) {
    resolvedCookie.value = cookie
    emit('resolved', cookie)
  }
  if (warning) appStore.showWarning(t('admin.accounts.tokenrhythm.credentialPersistWarning'))
}

const loadKeys = async () => {
  if (inventoryLoading.value) return
  inventoryLoading.value = true
  inventoryError.value = ''
  try {
    const response = await adminAPI.accounts.listTokenRhythmAPIKeys(credentialRequest())
    keys.value = response.keys
    inventoryLoaded.value = true
    if (response.tokenrhythm_cookie) {
      applyCredentialResult(response.tokenrhythm_cookie, response.credential_persist_warning)
      sess.value = ''
    } else if (response.credential_persist_warning) {
      applyCredentialResult(undefined, response.credential_persist_warning)
    }
  } catch (error: any) {
    inventoryLoaded.value = true
    inventoryError.value = error.response?.data?.message || t('admin.accounts.tokenrhythm.keyListFailed')
  } finally {
    inventoryLoading.value = false
  }
}

const disableKey = async (item: TokenRhythmAPIKeyListItem) => {
  if (!window.confirm(t('admin.accounts.tokenrhythm.disableConfirm', { name: item.name }))) return
  actionId.value = item.id
  try {
    const response = await adminAPI.accounts.disableTokenRhythmAPIKey(item.id, credentialRequest())
    applyCredentialResult(response.tokenrhythm_cookie, response.credential_persist_warning)
    await loadKeys()
  } catch (error: any) {
    appStore.showError(error.response?.data?.message || t('admin.accounts.tokenrhythm.keyActionFailed'))
  } finally {
    actionId.value = null
  }
}

const deleteKey = async (item: TokenRhythmAPIKeyListItem) => {
  if (!window.confirm(t('admin.accounts.tokenrhythm.deleteConfirm', { name: item.name }))) return
  actionId.value = item.id
  try {
    const response = await adminAPI.accounts.deleteTokenRhythmAPIKey(item.id, credentialRequest())
    applyCredentialResult(response.tokenrhythm_cookie, response.credential_persist_warning)
    await loadKeys()
  } catch (error: any) {
    appStore.showError(error.response?.data?.message || t('admin.accounts.tokenrhythm.keyActionFailed'))
  } finally {
    actionId.value = null
  }
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
  const cookieValue = resolvedCookie.value.trim() || props.credentialCookie.trim()
  const nameValue = keyName.value.trim()
  const useStoredAccountCredential = Boolean(props.accountId) && !sessValue && !cookieValue
  if ((!useStoredAccountCredential && !sessValue && !cookieValue) || !nameValue || keyLoading.value) return

  keyLoading.value = true
  createdKey.value = ''
  try {
    const generated = await adminAPI.accounts.createTokenRhythmAPIKey({
      sess: useStoredAccountCredential ? undefined : sessValue || undefined,
      cookie: useStoredAccountCredential ? undefined : cookieValue || undefined,
      name: nameValue,
      proxy_id: props.proxyId ?? undefined,
      account_id: useStoredAccountCredential ? props.accountId ?? undefined : undefined
    })
    createdKey.value = generated.api_key
    emit('apiKeyCreated', generated)
    applyCredentialResult(generated.tokenrhythm_cookie, generated.credential_persist_warning)
    keySess.value = ''
    sess.value = ''
    appStore.showSuccess(t('admin.accounts.tokenrhythm.keyCreated'))
  } catch (error: any) {
    appStore.showError(error.response?.data?.message || t('admin.accounts.tokenrhythm.keyCreateFailed'))
  } finally {
    keyLoading.value = false
  }
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
    applyCredentialResult(resolved.tokenrhythm_cookie)
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
