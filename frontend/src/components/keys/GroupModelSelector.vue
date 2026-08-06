<template>
  <div class="space-y-2">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('keys.modelRestrictionSelected', { count: modelValue.length }) }}
      </span>
      <div class="flex items-center gap-2">
        <button
          type="button"
          class="text-xs font-medium text-primary-600 hover:text-primary-700 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400"
          :disabled="disabled || loading || models.length === 0"
          @click="selectAll"
        >
          {{ t('keys.modelRestrictionSelectAll') }}
        </button>
        <button
          type="button"
          class="text-xs font-medium text-gray-500 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 dark:text-gray-400 dark:hover:text-gray-200"
          :disabled="disabled || loading || modelValue.length === 0"
          @click="clearAll"
        >
          {{ t('keys.modelRestrictionClear') }}
        </button>
      </div>
    </div>

    <div v-if="unavailableModels.length > 0" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-300">
      <p>{{ t('keys.modelRestrictionUnavailable') }}</p>
      <p class="mt-1 break-all">{{ unavailableModels.join(', ') }}</p>
    </div>

    <div v-if="loading" class="rounded-lg border border-gray-200 px-3 py-4 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
      {{ t('keys.modelRestrictionLoading') }}
    </div>
    <div v-else-if="error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-4 text-center text-sm text-red-600 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-400">
      {{ t('keys.modelRestrictionLoadFailed') }}
    </div>
    <div v-else-if="models.length === 0" class="rounded-lg border border-gray-200 px-3 py-4 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
      {{ t('keys.modelRestrictionNoModels') }}
    </div>
    <div v-else class="max-h-56 space-y-1 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600">
      <label
        v-for="model in models"
        :key="model"
        class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm transition-colors hover:bg-gray-50 dark:hover:bg-dark-700"
      >
        <input
          type="checkbox"
          :checked="modelValue.includes(model)"
          :disabled="disabled || loading"
          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
          @change="toggleModel(model, ($event.target as HTMLInputElement).checked)"
        />
        <span class="min-w-0 break-all text-gray-700 dark:text-gray-300">{{ model }}</span>
      </label>
    </div>

    <p class="input-hint">{{ t('keys.modelWhitelistHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  modelValue: string[]
  models: string[]
  loading?: boolean
  error?: boolean
  disabled?: boolean
}>(), {
  loading: false,
  error: false,
  disabled: false
})

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()

const unavailableModels = computed(() => props.modelValue.filter(model => !props.models.includes(model)))

const toggleModel = (model: string, checked: boolean) => {
  const selected = new Set(props.modelValue)
  if (checked) {
    selected.add(model)
  } else {
    selected.delete(model)
  }
  const legacyModels = props.modelValue.filter(item => !props.models.includes(item))
  emit('update:modelValue', [...legacyModels, ...props.models.filter(item => selected.has(item))])
}

const selectAll = () => {
  emit('update:modelValue', [...props.models])
}

const clearAll = () => {
  emit('update:modelValue', [])
}
</script>
