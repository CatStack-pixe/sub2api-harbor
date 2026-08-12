<template>
  <BaseDialog
    :show="show"
    :title="t('admin.proxies.proxyGroups.manageTitle')"
    width="normal"
    @close="handleClose"
  >
    <form class="flex items-end gap-3" @submit.prevent="handleCreate">
      <div class="min-w-0 flex-1">
        <label class="input-label">{{ t('admin.proxies.proxyGroups.name') }}</label>
        <input
          v-model="newGroupName"
          class="input"
          maxlength="100"
          :placeholder="t('admin.proxies.proxyGroups.namePlaceholder')"
          data-testid="proxy-group-create-name"
        />
      </div>
      <button
        type="submit"
        class="btn btn-primary shrink-0"
        :disabled="creating"
        data-testid="proxy-group-create"
      >
        <Icon name="plus" size="sm" class="mr-2" />
        {{ t('common.create') }}
      </button>
    </form>

    <div class="mt-5 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
      <div
        v-if="groups.length === 0"
        class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400"
      >
        {{ t('admin.proxies.proxyGroups.empty') }}
      </div>
      <div
        v-for="group in groups"
        v-else
        :key="group.id"
        class="flex items-center gap-3 border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700"
        data-testid="proxy-group-row"
      >
        <div class="min-w-0 flex-1">
          <input
            v-if="editingGroupId === group.id"
            v-model="editingGroupName"
            class="input"
            maxlength="100"
            :aria-label="t('admin.proxies.proxyGroups.name')"
            @keydown.escape="cancelEdit"
            @keydown.enter.prevent="handleUpdate(group)"
          />
          <template v-else>
            <div class="truncate text-sm font-medium text-gray-900 dark:text-white">
              {{ group.name }}
            </div>
            <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
              <span>{{ t('admin.proxies.proxyGroups.totalCount', { count: group.total_count }) }}</span>
              <span class="text-emerald-600 dark:text-emerald-400">
                {{ t('admin.proxies.proxyGroups.activeCount', { count: group.active_count }) }}
              </span>
              <span>{{ t('admin.proxies.proxyGroups.inactiveCount', { count: group.inactive_count }) }}</span>
            </div>
          </template>
        </div>

        <div class="flex shrink-0 items-center gap-1">
          <template v-if="editingGroupId === group.id">
            <button
              type="button"
              class="rounded p-2 text-primary-600 hover:bg-primary-50 disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
              :disabled="updating"
              :title="t('common.save')"
              @click="handleUpdate(group)"
            >
              <Icon name="check" size="sm" />
            </button>
            <button
              type="button"
              class="rounded p-2 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-700"
              :title="t('common.cancel')"
              @click="cancelEdit"
            >
              <Icon name="x" size="sm" />
            </button>
          </template>
          <template v-else>
            <button
              type="button"
              class="rounded p-2 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              :title="t('common.edit')"
              @click="startEdit(group)"
            >
              <Icon name="edit" size="sm" />
            </button>
            <button
              type="button"
              class="rounded p-2 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:text-gray-400 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              :title="t('common.delete')"
              @click="deletingGroup = group"
            >
              <Icon name="trash" size="sm" />
            </button>
          </template>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="deletingGroup !== null"
    :title="t('admin.proxies.proxyGroups.deleteTitle')"
    :message="t('admin.proxies.proxyGroups.deleteConfirm', {
      name: deletingGroup?.name || '',
      count: deletingGroup?.total_count || 0
    })"
    :confirm-text="t('common.delete')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="handleDelete"
    @cancel="deletingGroup = null"
  />
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ProxyGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  show: boolean
  groups: ProxyGroup[]
}>()

const emit = defineEmits<{
  close: []
  changed: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const newGroupName = ref('')
const creating = ref(false)
const updating = ref(false)
const editingGroupId = ref<number | null>(null)
const editingGroupName = ref('')
const deletingGroup = ref<ProxyGroup | null>(null)

watch(
  () => props.show,
  (show) => {
    if (!show) return
    newGroupName.value = ''
    editingGroupId.value = null
    editingGroupName.value = ''
    deletingGroup.value = null
  }
)

const normalizedName = (value: string) => value.trim()

const validateName = (value: string): string | null => {
  const name = normalizedName(value)
  if (!name) {
    appStore.showError(t('admin.proxies.proxyGroups.nameRequired'))
    return null
  }
  return name
}

const handleCreate = async () => {
  const name = validateName(newGroupName.value)
  if (!name || creating.value) return

  creating.value = true
  try {
    await adminAPI.proxyGroups.create({ name })
    newGroupName.value = ''
    appStore.showSuccess(t('admin.proxies.proxyGroups.created'))
    emit('changed')
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.proxies.proxyGroups.createFailed'))
  } finally {
    creating.value = false
  }
}

const startEdit = (group: ProxyGroup) => {
  editingGroupId.value = group.id
  editingGroupName.value = group.name
}

const cancelEdit = () => {
  editingGroupId.value = null
  editingGroupName.value = ''
}

const handleUpdate = async (group: ProxyGroup) => {
  const name = validateName(editingGroupName.value)
  if (!name || updating.value) return

  updating.value = true
  try {
    await adminAPI.proxyGroups.update(group.id, { name })
    cancelEdit()
    appStore.showSuccess(t('admin.proxies.proxyGroups.updated'))
    emit('changed')
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.proxies.proxyGroups.updateFailed'))
  } finally {
    updating.value = false
  }
}

const handleDelete = async () => {
  if (!deletingGroup.value) return

  try {
    await adminAPI.proxyGroups.delete(deletingGroup.value.id)
    deletingGroup.value = null
    appStore.showSuccess(t('admin.proxies.proxyGroups.deleted'))
    emit('changed')
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.proxies.proxyGroups.deleteFailed'))
  }
}

const handleClose = () => {
  if (creating.value || updating.value) return
  emit('close')
}
</script>
