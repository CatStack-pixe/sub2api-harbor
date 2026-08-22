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
        <button
          type="button"
          class="btn btn-secondary shrink-0"
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
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { TokenRhythmSessionResult } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  proxyId?: number | null
}>()

const emit = defineEmits<{
  resolved: [cookie: string]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const sess = ref('')
const loading = ref(false)
const result = ref<TokenRhythmSessionResult | null>(null)

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
