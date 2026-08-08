import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import RemoteIngestView from '../RemoteIngestView.vue'

const {
  copyToClipboard,
  createRegistrationToken,
  listClients,
  listDeliveries,
  listRegistrationTokens,
  retryDelivery,
  revokeClient,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  copyToClipboard: vi.fn(),
  createRegistrationToken: vi.fn(),
  listClients: vi.fn(),
  listDeliveries: vi.fn(),
  listRegistrationTokens: vi.fn(),
  retryDelivery: vi.fn(),
  revokeClient: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    remoteIngest: {
      createRegistrationToken,
      listClients,
      listDeliveries,
      listRegistrationTokens,
      retryDelivery,
      revokeClient
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const page = <T>(items: T[]) => ({
  items,
  total: items.length,
  page: 1,
  page_size: 20,
  pages: items.length > 0 ? 1 : 0
})

const DataTableStub = {
  props: ['columns', 'data', 'loading'],
  template: `
    <div data-test="data-table">
      <div v-for="row in data" :key="row.id" :data-test="'row-' + row.id">
        <template v-for="column in columns" :key="column.key">
          <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]">
            {{ row[column.key] }}
          </slot>
        </template>
      </div>
      <slot v-if="!loading && data.length === 0" name="empty" />
    </div>
  `
}

const BaseDialogStub = {
  props: ['show', 'title'],
  emits: ['close'],
  template: `
    <div v-if="show" data-test="base-dialog">
      <slot />
      <slot name="footer" />
    </div>
  `
}

const ConfirmDialogStub = {
  props: ['show', 'title', 'message'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="confirm-dialog">
      <button data-test="confirm-dialog-submit" @click="$emit('confirm')">confirm</button>
      <button data-test="confirm-dialog-cancel" @click="$emit('cancel')">cancel</button>
    </div>
  `
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: '<select :value="modelValue"></select>'
}

function mountView() {
  return mount(RemoteIngestView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        DataTable: DataTableStub,
        EmptyState: true,
        Icon: true,
        Pagination: true,
        RouterLink: { template: '<a><slot /></a>' },
        Select: SelectStub,
        TotpStepUpDialog: true,
        Teleport: true
      }
    }
  })
}

describe('admin RemoteIngestView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listRegistrationTokens.mockResolvedValue(page([
      {
        id: 'token-1',
        fingerprint: 'tok_abcd',
        expires_at: '2026-08-08T10:10:00Z',
        used_at: null,
        client_id: null,
        created_at: '2026-08-08T10:00:00Z'
      }
    ]))
    listClients.mockResolvedValue(page([
      {
        id: 'client-1',
        machine_name: 'worker-1',
        public_key_fingerprint: 'ed25519:abcd',
        access_subject: 'service-token-1',
        enrolled_at: '2026-08-08T10:00:00Z',
        last_active_at: '2026-08-08T10:05:00Z',
        revoked_at: null
      }
    ]))
    listDeliveries.mockResolvedValue(page([
      {
        id: 'delivery-1',
        client_id: 'client-1',
        client_machine_name: 'worker-1',
        external_id: 'remote-account-1',
        account_id: 42,
        platform: 'openai',
        group_name: 'openai-default',
        status: 'probe_failed',
        masked_error: 'upstream returned 401 (credential redacted)',
        attempts: 1,
        created_at: '2026-08-08T10:00:00Z',
        updated_at: '2026-08-08T10:02:00Z'
      }
    ]))
    createRegistrationToken.mockResolvedValue({
      id: 'token-2',
      token: 'enroll-secret-value',
      fingerprint: 'tok_efgh',
      expires_at: '2026-08-08T10:20:00Z',
      used_at: null,
      client_id: null,
      created_at: '2026-08-08T10:10:00Z'
    })
    revokeClient.mockResolvedValue({ id: 'client-1', revoked_at: '2026-08-08T10:10:00Z' })
    retryDelivery.mockResolvedValue({ id: 'delivery-1', status: 'pending' })
    copyToClipboard.mockResolvedValue(true)
  })

  it('shows a newly generated token only until the result dialog closes', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="generate-registration-token"]').trigger('click')
    await flushPromises()

    expect(createRegistrationToken).toHaveBeenCalledWith(
      600,
      expect.objectContaining({
        idempotencyKey: expect.stringMatching(/^remote-ingest-create-token-/)
      })
    )
    expect(wrapper.get('[data-test="created-registration-token"]').attributes('value'))
      .toBe('enroll-secret-value')

    await wrapper.get('[data-test="close-registration-token"]').trigger('click')
    expect(wrapper.find('[data-test="created-registration-token"]').exists()).toBe(false)
  })

  it('revokes an active client through confirmation with an idempotency key', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="remote-ingest-tab-clients"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="revoke-client-client-1"]').trigger('click')
    await wrapper.get('[data-test="confirm-dialog-submit"]').trigger('click')
    await flushPromises()

    expect(revokeClient).toHaveBeenCalledWith(
      'client-1',
      expect.objectContaining({
        idempotencyKey: expect.stringMatching(/^remote-ingest-revoke-client-client-1-/)
      })
    )
  })

  it('shows masked delivery failures and queues a probe retry', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="remote-ingest-tab-deliveries"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('credential redacted')
    await wrapper.get('[data-test="retry-delivery-delivery-1"]').trigger('click')
    await flushPromises()

    expect(retryDelivery).toHaveBeenCalledWith(
      'delivery-1',
      expect.objectContaining({
        idempotencyKey: expect.stringMatching(/^remote-ingest-retry-delivery-delivery-1-/)
      })
    )
  })
})
