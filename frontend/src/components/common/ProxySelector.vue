<template>
  <div class="relative" ref="containerRef">
    <button
      type="button"
      @click="toggle"
      :disabled="disabled"
      :class="[
        'select-trigger',
        isOpen && 'select-trigger-open',
        disabled && 'select-trigger-disabled'
      ]"
    >
      <span class="select-value">
        {{ selectedLabel }}
      </span>
      <span class="select-icon">
        <Icon
          name="chevronDown"
          size="md"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Transition name="select-dropdown">
      <div v-if="isOpen" class="select-dropdown">
        <!-- Search and Batch Test Header -->
        <div class="select-header">
          <div class="select-search">
            <Icon name="search" size="sm" class="text-gray-400" />
            <input
              ref="searchInputRef"
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.proxies.searchProxies')"
              class="select-search-input"
              @click.stop
            />
          </div>
          <button
            v-if="matchingProxies.length > 0"
            type="button"
            @click.stop="handleBatchTest"
            :disabled="batchTesting || matchingProxies.length > MAX_VISIBLE_PROXIES"
            class="batch-test-btn"
            :title="batchTestTitle"
            data-testid="proxy-batch-test"
          >
            <svg v-if="batchTesting" class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            <Icon v-else name="play" size="sm" />
          </button>
        </div>

        <div class="select-filter-row">
          <select
            v-model="groupFilter"
            class="select-group-filter"
            data-testid="proxy-group-filter"
            :aria-label="t('admin.proxies.proxyGroup')"
            @click.stop
          >
            <option v-for="option in groupOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
        </div>

        <!-- Options list -->
        <div class="select-options">
          <!-- No Proxy option -->
          <div
            @click="selectOption(null)"
            :class="['select-option', modelValue === null && 'select-option-selected']"
          >
            <span class="select-option-label">{{ t('admin.accounts.noProxy') }}</span>
            <Icon v-if="modelValue === null" name="check" size="sm" class="text-primary-500" />
          </div>

          <!-- Proxy options -->
          <div
            v-for="proxy in visibleProxies"
            :key="proxy.id"
            @click="selectOption(proxy.id)"
            :class="['select-option', modelValue === proxy.id && 'select-option-selected']"
            data-testid="proxy-option"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="truncate font-medium">{{ proxy.name }}</span>
                <span
                  v-if="proxy.proxy_group_name"
                  class="inline-flex max-w-[7rem] flex-shrink-0 truncate rounded bg-primary-50 px-1.5 py-0.5 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
                >
                  {{ proxy.proxy_group_name }}
                </span>
                <!-- Account count badge -->
                <span
                  v-if="proxy.account_count !== undefined"
                  class="inline-flex flex-shrink-0 items-center rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-dark-600 dark:text-gray-400"
                >
                  {{ proxy.account_count }}
                </span>
                <!-- Test result badges -->
                <template v-if="testResults[proxy.id]">
                  <span
                    v-if="testResults[proxy.id].success"
                    class="inline-flex flex-shrink-0 items-center gap-1 rounded bg-emerald-100 px-1.5 py-0.5 text-xs text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400"
                  >
                    <span v-if="testResults[proxy.id].country">{{
                      testResults[proxy.id].country
                    }}</span>
                    <span v-if="testResults[proxy.id].latency_ms"
                      >{{ testResults[proxy.id].latency_ms }}ms</span
                    >
                  </span>
                  <span
                    v-else
                    class="inline-flex flex-shrink-0 items-center rounded bg-red-100 px-1.5 py-0.5 text-xs text-red-700 dark:bg-red-900/30 dark:text-red-400"
                  >
                    {{ t('admin.proxies.testFailed') }}
                  </span>
                </template>
              </div>
              <div class="truncate text-xs text-gray-500 dark:text-gray-400">
                {{ proxy.protocol }}://{{ proxy.host }}:{{ proxy.port }}
              </div>
            </div>

            <!-- Individual test button -->
            <button
              type="button"
              @click.stop="handleTestProxy(proxy)"
              :disabled="testingProxyIds.has(proxy.id)"
              class="test-btn"
              :title="t('admin.proxies.testConnection')"
            >
              <svg
                v-if="testingProxyIds.has(proxy.id)"
                class="h-3.5 w-3.5 animate-spin"
                fill="none"
                viewBox="0 0 24 24"
              >
                <circle
                  class="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  stroke-width="4"
                ></circle>
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                ></path>
              </svg>
              <Icon v-else name="play" size="xs" />
            </button>

            <Icon
              v-if="modelValue === proxy.id"
              name="check"
              size="sm"
              class="flex-shrink-0 text-primary-500"
            />
          </div>

          <!-- Empty state -->
          <div v-if="matchingProxies.length === 0" class="select-empty">
            {{ t('common.noOptionsFound') }}
          </div>
          <div
            v-if="isResultLimited"
            class="select-limit-hint"
            data-testid="proxy-result-limit-hint"
          >
            {{
              t('admin.proxies.resultLimitHint', {
                shown: MAX_VISIBLE_PROXIES,
                total: matchingProxies.length
              })
            }}
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Icon from '@/components/icons/Icon.vue'
import type { Proxy } from '@/types'

const { t } = useI18n()

interface ProxyTestResult {
  success: boolean
  message: string
  latency_ms?: number
  ip_address?: string
  city?: string
  region?: string
  country?: string
}

interface Props {
  modelValue: number | null
  proxies: Proxy[]
  disabled?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const isOpen = ref(false)
const searchQuery = ref('')
const containerRef = ref<HTMLElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)
const groupFilter = ref('all')
const MAX_VISIBLE_PROXIES = 100

// Test state
const testResults = reactive<Record<number, ProxyTestResult>>({})
const testingProxyIds = reactive(new Set<number>())
const batchTesting = ref(false)

const selectedProxy = computed(() => {
  if (props.modelValue === null) return null
  return props.proxies.find((p) => p.id === props.modelValue) || null
})

const selectedLabel = computed(() => {
  if (!selectedProxy.value) {
    return t('admin.accounts.noProxy')
  }
  const proxy = selectedProxy.value
  return `${proxy.name} (${proxy.protocol}://${proxy.host}:${proxy.port})`
})

const groupOptions = computed(() => {
  const seen = new Set<number>()
  const groups: Array<{ value: string; label: string }> = []
  for (const proxy of props.proxies) {
    if (!proxy.proxy_group_id || seen.has(proxy.proxy_group_id)) continue
    seen.add(proxy.proxy_group_id)
    groups.push({
      value: `group:${proxy.proxy_group_id}`,
      label: proxy.proxy_group_name || `#${proxy.proxy_group_id}`
    })
  }
  return [
    { value: 'all', label: t('admin.proxies.allProxyGroups') },
    { value: 'ungrouped', label: t('admin.proxies.ungrouped') },
    ...groups
  ]
})

const matchingProxies = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return props.proxies.filter((proxy) => {
    if (groupFilter.value === 'ungrouped' && proxy.proxy_group_id != null) return false
    if (groupFilter.value.startsWith('group:')) {
      const groupId = Number(groupFilter.value.slice('group:'.length))
      if (proxy.proxy_group_id !== groupId) return false
    }
    if (!query) return true
    const name = proxy.name.toLowerCase()
    const host = proxy.host.toLowerCase()
    return name.includes(query) || host.includes(query)
  })
})

const visibleProxies = computed(() => {
  const visible = matchingProxies.value.slice(0, MAX_VISIBLE_PROXIES)
  const selected = selectedProxy.value
  if (
    !selected ||
    !matchingProxies.value.some((proxy) => proxy.id === selected.id) ||
    visible.some((proxy) => proxy.id === selected.id)
  ) {
    return visible
  }

  if (visible.length >= MAX_VISIBLE_PROXIES) {
    visible[visible.length - 1] = selected
  } else {
    visible.push(selected)
  }
  return visible
})

const isResultLimited = computed(() => {
  return matchingProxies.value.length > MAX_VISIBLE_PROXIES
})

const batchTestTitle = computed(() =>
  matchingProxies.value.length > MAX_VISIBLE_PROXIES
    ? t('admin.proxies.batchTestLimitExceeded', { count: MAX_VISIBLE_PROXIES })
    : t('admin.proxies.batchTest')
)

const toggle = () => {
  if (props.disabled) return
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    nextTick(() => {
      searchInputRef.value?.focus()
    })
  }
}

const selectOption = (value: number | null) => {
  emit('update:modelValue', value)
  isOpen.value = false
  searchQuery.value = ''
  groupFilter.value = 'all'
}

const handleTestProxy = async (proxy: Proxy) => {
  if (testingProxyIds.has(proxy.id)) return

  testingProxyIds.add(proxy.id)
  try {
    const result = await adminAPI.proxies.testProxy(proxy.id)
    testResults[proxy.id] = result
  } catch (error: any) {
    testResults[proxy.id] = {
      success: false,
      message: error.response?.data?.detail || 'Test failed'
    }
  } finally {
    testingProxyIds.delete(proxy.id)
  }
}

const handleBatchTest = async () => {
  const targets = matchingProxies.value
  if (
    batchTesting.value ||
    targets.length === 0 ||
    targets.length > MAX_VISIBLE_PROXIES
  ) {
    return
  }

  batchTesting.value = true

  let index = 0
  const worker = async () => {
    while (index < targets.length) {
      const proxy = targets[index]
      index++
      testingProxyIds.add(proxy.id)
      try {
        const result = await adminAPI.proxies.testProxy(proxy.id)
        testResults[proxy.id] = result
      } catch (error: any) {
        testResults[proxy.id] = {
          success: false,
          message: error.response?.data?.detail || t('admin.proxies.testFailed')
        }
      } finally {
        testingProxyIds.delete(proxy.id)
      }
    }
  }

  try {
    const workers = Array.from({ length: Math.min(5, targets.length) }, () => worker())
    await Promise.all(workers)
  } finally {
    batchTesting.value = false
  }
}

const handleClickOutside = (event: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    isOpen.value = false
    searchQuery.value = ''
    groupFilter.value = 'all'
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && isOpen.value) {
    isOpen.value = false
    searchQuery.value = ''
    groupFilter.value = 'all'
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
.select-trigger {
  @apply flex w-full items-center justify-between gap-2;
  @apply rounded-xl px-4 py-2.5 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.select-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.select-trigger-disabled {
  @apply cursor-not-allowed bg-gray-100 opacity-60 dark:bg-dark-900;
}

.select-value {
  @apply flex-1 truncate text-left;
}

.select-icon {
  @apply flex-shrink-0 text-gray-400 dark:text-dark-400;
}

.select-dropdown {
  @apply absolute z-[100] mt-2 w-full;
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
}

.select-header {
  @apply flex items-center gap-2 px-3 py-2;
  @apply border-b border-gray-100 dark:border-dark-700;
}

.select-filter-row {
  @apply border-b border-gray-100 px-3 py-2 dark:border-dark-700;
}

.select-group-filter {
  @apply w-full rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 text-xs;
  @apply text-gray-700 focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20;
  @apply dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200;
}

.select-search {
  @apply flex flex-1 items-center gap-2;
}

.select-search-input {
  @apply flex-1 bg-transparent text-sm;
  @apply text-gray-900 dark:text-gray-100;
  @apply placeholder:text-gray-400 dark:placeholder:text-dark-400;
  @apply focus:outline-none;
}

.batch-test-btn {
  @apply flex-shrink-0 rounded-lg p-1.5;
  @apply text-gray-500 hover:text-emerald-600 dark:hover:text-emerald-400;
  @apply hover:bg-emerald-50 dark:hover:bg-emerald-900/20;
  @apply transition-colors disabled:cursor-not-allowed disabled:opacity-50;
}

.select-options {
  @apply max-h-60 overflow-y-auto py-1;
}

.select-option {
  @apply flex items-center justify-between gap-2;
  @apply px-4 py-2.5 text-sm;
  @apply text-gray-700 dark:text-gray-300;
  @apply cursor-pointer transition-colors duration-150;
  @apply hover:bg-gray-50 dark:hover:bg-dark-700;
}

.select-option-selected {
  @apply bg-primary-50 dark:bg-primary-900/20;
  @apply text-primary-700 dark:text-primary-300;
}

.select-option-label {
  @apply truncate;
}

.select-empty {
  @apply px-4 py-8 text-center text-sm;
  @apply text-gray-500 dark:text-dark-400;
}

.select-limit-hint {
  @apply border-t border-gray-100 px-4 py-2 text-center text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400;
}

.test-btn {
  @apply flex-shrink-0 rounded p-1;
  @apply text-gray-400 hover:text-emerald-600 dark:hover:text-emerald-400;
  @apply hover:bg-emerald-50 dark:hover:bg-emerald-900/20;
  @apply transition-colors disabled:cursor-not-allowed disabled:opacity-50;
}

/* Dropdown animation */
.select-dropdown-enter-active,
.select-dropdown-leave-active {
  transition: all 0.2s ease;
}

.select-dropdown-enter-from,
.select-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
