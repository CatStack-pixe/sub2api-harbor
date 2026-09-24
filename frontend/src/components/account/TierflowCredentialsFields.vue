<template>
  <div class="space-y-4" data-testid="tierflow-console-credentials">
    <div>
      <label for="tierflow-cookie" class="input-label">{{ t('admin.accounts.tierflow.cookie') }}</label>
      <input
        id="tierflow-cookie"
        :value="cookie"
        type="password"
        class="input font-mono"
        autocomplete="new-password"
        data-testid="tierflow-cookie"
        data-1p-ignore
        data-lpignore="true"
        data-bwignore="true"
        :placeholder="editing ? t('admin.accounts.leaveEmptyToKeep') : t('admin.accounts.tierflow.cookiePlaceholder')"
        @input="emit('update:cookie', ($event.target as HTMLInputElement).value)"
      />
      <p class="input-hint">{{ t('admin.accounts.tierflow.cookieHint') }}</p>
    </div>
    <div>
      <label for="tierflow-user-id" class="input-label">{{ t('admin.accounts.tierflow.userId') }}</label>
      <input
        id="tierflow-user-id"
        :value="userId"
        type="text"
        inputmode="numeric"
        pattern="[0-9]+"
        class="input font-mono"
        autocomplete="off"
        data-testid="tierflow-user-id"
        data-1p-ignore
        data-lpignore="true"
        data-bwignore="true"
        :placeholder="editing ? t('admin.accounts.leaveEmptyToKeep') : t('admin.accounts.tierflow.userIdPlaceholder')"
        @input="emit('update:userId', ($event.target as HTMLInputElement).value)"
      />
      <p class="input-hint">{{ t('admin.accounts.tierflow.userIdHint') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

withDefaults(defineProps<{
  cookie: string
  userId: string
  editing?: boolean
}>(), {
  editing: false
})

const emit = defineEmits<{
  'update:cookie': [value: string]
  'update:userId': [value: string]
}>()

const { t } = useI18n()
</script>
