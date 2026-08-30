<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6" data-testid="heartbeat-view">
      <header>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
          {{ t("admin.settings.heartbeat.title") }}
        </h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.heartbeat.description") }}
        </p>
      </header>

      <HeartbeatSettingsPanel />

      <section class="card" data-testid="heartbeat-logs">
        <div class="flex flex-wrap items-start justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t("admin.settings.heartbeat.logsTitle") }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.heartbeat.logsDescription") }}
            </p>
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="logsLoading"
            :aria-label="t('admin.settings.heartbeat.logsRefresh')"
            :title="t('admin.settings.heartbeat.logsRefresh')"
            @click="loadLogs"
          >
            <Icon name="refresh" size="sm" :class="logsLoading && 'animate-spin'" />
            <span>{{ t("admin.settings.heartbeat.logsRefresh") }}</span>
          </button>
        </div>

        <div v-if="logsError" class="mx-6 mt-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {{ logsError }}
        </div>
        <div v-if="logsLoading" class="flex items-center justify-center p-10 text-sm text-gray-500 dark:text-gray-400">
          <span class="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-primary-600" />
          {{ t("admin.settings.heartbeat.logsLoading") }}
        </div>
        <div v-else-if="logs.length === 0" class="p-10 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.heartbeat.logsEmpty") }}
        </div>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-left text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800/60 dark:text-gray-400">
              <tr>
                <th class="whitespace-nowrap px-6 py-3 font-medium">{{ t("admin.settings.heartbeat.logsProvider") }}</th>
                <th class="whitespace-nowrap px-6 py-3 font-medium">{{ t("admin.settings.heartbeat.logsStatus") }}</th>
                <th class="whitespace-nowrap px-6 py-3 font-medium">{{ t("admin.settings.heartbeat.logsAttempts") }}</th>
                <th class="whitespace-nowrap px-6 py-3 font-medium">{{ t("admin.settings.heartbeat.logsTargets") }}</th>
                <th class="whitespace-nowrap px-6 py-3 font-medium">{{ t("admin.settings.heartbeat.logsUpdated") }}</th>
                <th class="min-w-[16rem] px-6 py-3 font-medium">{{ t("admin.settings.heartbeat.logsError") }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="log in logs" :key="log.id" class="align-top">
                <td class="whitespace-nowrap px-6 py-4 font-medium text-gray-900 dark:text-white">
                  {{ log.provider }}
                  <span class="block text-xs font-normal text-gray-500 dark:text-gray-400">#{{ log.id }}</span>
                </td>
                <td class="whitespace-nowrap px-6 py-4">
                  <span class="inline-flex rounded-full px-2 py-1 text-xs font-medium" :class="statusClass(log.status)">{{ log.status }}</span>
                </td>
                <td class="whitespace-nowrap px-6 py-4 text-gray-700 dark:text-gray-300">{{ log.attempts }}</td>
                <td class="whitespace-nowrap px-6 py-4 text-gray-700 dark:text-gray-300">
                  <span>G{{ log.target_group_id || "-" }}</span>
                  <span class="block text-xs text-gray-500 dark:text-gray-400">P{{ log.target_proxy_group_id || 0 }}</span>
                </td>
                <td class="whitespace-nowrap px-6 py-4 text-gray-600 dark:text-gray-300">{{ formatDate(log.updated_at) }}</td>
                <td class="max-w-xl break-words px-6 py-4 text-red-600 dark:text-red-400">{{ log.last_error || "-" }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="logsPages > 1" class="flex items-center justify-between border-t border-gray-100 px-6 py-3 text-sm dark:border-dark-700">
          <span class="text-gray-500 dark:text-gray-400">{{ t("admin.settings.heartbeat.logsPage", { page: logsPage, pages: logsPages }) }}</span>
          <div class="flex items-center gap-2">
            <button type="button" class="btn btn-secondary btn-sm px-2" :disabled="logsPage <= 1 || logsLoading" :aria-label="t('common.back')" :title="t('common.back')" @click="changeLogsPage(logsPage - 1)">
              <Icon name="chevronLeft" size="sm" />
            </button>
            <button type="button" class="btn btn-secondary btn-sm px-2" :disabled="logsPage >= logsPages || logsLoading" :aria-label="t('common.next')" :title="t('common.next')" @click="changeLogsPage(logsPage + 1)">
              <Icon name="chevronRight" size="sm" />
            </button>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { adminAPI } from "@/api";
import AppLayout from "@/components/layout/AppLayout.vue";
import Icon from "@/components/icons/Icon.vue";
import type { HeartbeatProvisioningLog } from "@/api/admin/heartbeat";
import HeartbeatSettingsPanel from "@/views/admin/settings/HeartbeatSettingsPanel.vue";
import { extractApiErrorMessage } from "@/utils/apiError";

const { t } = useI18n();

const logs = ref<HeartbeatProvisioningLog[]>([]);
const logsLoading = ref(false);
const logsError = ref("");
const logsPage = ref(1);
const logsPageSize = 25;
const logsTotal = ref(0);
const logsPages = computed(() => Math.max(1, Math.ceil(logsTotal.value / logsPageSize)));

async function loadLogs(): Promise<void> {
  logsLoading.value = true;
  logsError.value = "";
  try {
    const result = await adminAPI.heartbeat.getLogs(logsPage.value, logsPageSize);
    logs.value = result.items ?? [];
    logsTotal.value = result.total ?? 0;
  } catch (error) {
    logsError.value = extractApiErrorMessage(error);
  } finally {
    logsLoading.value = false;
  }
}

function changeLogsPage(page: number): void {
  if (page < 1 || page > logsPages.value || page === logsPage.value) return;
  logsPage.value = page;
  void loadLogs();
}

function formatDate(value?: string | null): string {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function statusClass(status: string): string {
  if (status === "complete") return "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300";
  if (status === "failed") return "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300";
  if (status === "processing") return "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300";
  return "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300";
}

onMounted(() => {
  void loadLogs();
});
</script>
