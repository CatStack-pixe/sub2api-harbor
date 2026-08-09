import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Proxy, ProxyGroup } from '@/types'
import ProxiesView from '@/views/admin/ProxiesView.vue'

const {
  listProxies,
  getAllWithCount,
  createProxy,
  updateProxy,
  batchGroup,
  testProxy,
  listProxyGroups,
  showSuccess,
  showError,
  showInfo
} = vi.hoisted(() => ({
  listProxies: vi.fn(),
  getAllWithCount: vi.fn(),
  createProxy: vi.fn(),
  updateProxy: vi.fn(),
  batchGroup: vi.fn(),
  testProxy: vi.fn(),
  listProxyGroups: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxies: {
      list: listProxies,
      getAllWithCount,
      create: createProxy,
      update: updateProxy,
      batchGroup,
      batchCreate: vi.fn(),
      batchDelete: vi.fn(),
      exportData: vi.fn(),
      getProxyAccounts: vi.fn(),
      testProxy,
      checkProxyQuality: vi.fn(),
      delete: vi.fn()
    },
    proxyGroups: {
      list: listProxyGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError, showInfo })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const proxyGroups: ProxyGroup[] = [
  {
    id: 7,
    name: 'Primary',
    total_count: 1,
    active_count: 1,
    inactive_count: 0,
    created_at: '2026-08-08T00:00:00Z',
    updated_at: '2026-08-08T00:00:00Z'
  }
]

const proxy: Proxy = {
  id: 11,
  name: 'Proxy 11',
  protocol: 'http',
  host: 'proxy-11.example.com',
  port: 8011,
  username: null,
  status: 'active',
  expires_at: null,
  fallback_mode: 'none',
  expiry_warn_days: 7,
  proxy_group_id: 7,
  proxy_group_name: 'Primary',
  created_at: '2026-08-08T00:00:00Z',
  updated_at: '2026-08-08T00:00:00Z'
}

const LayoutStub = defineComponent({ template: '<main><slot /></main>' })
const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `
    <div>
      <div v-for="row in data" :key="row.id" data-testid="proxy-row">
        <slot name="cell-select" :row="row" />
        <slot name="cell-proxy_group_name" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `
})
const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<section v-if="show"><slot /><slot name="footer" /></section>'
})
const SelectStub = defineComponent({
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  methods: {
    handleChange(event: Event) {
      const value = (event.target as HTMLSelectElement).value
      const option = (this.options as Array<{ value: unknown }>).find(
        (item) => String(item.value ?? '') === value
      )
      const selected = option?.value ?? null
      this.$emit('update:modelValue', selected)
      this.$emit('change', selected, option)
    }
  },
  template: `
    <select :value="modelValue ?? ''" @change="handleChange">
      <option v-for="option in options" :key="String(option.value)" :value="option.value ?? ''">
        {{ option.label }}
      </option>
    </select>
  `
})

const mountView = () =>
  mount(ProxiesView, {
    global: {
      stubs: {
        AppLayout: LayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        ImportDataModal: true,
        ProxyGroupManageDialog: true,
        ProxyAdBanner: true,
        PlatformTypeBadge: true,
        Icon: true
      }
    }
  })

describe('ProxiesView proxy groups', () => {
  beforeEach(() => {
    for (const mock of [
      listProxies,
      getAllWithCount,
      createProxy,
      updateProxy,
      batchGroup,
      testProxy,
      listProxyGroups,
      showSuccess,
      showError,
      showInfo
    ]) {
      mock.mockReset()
    }
    listProxies.mockResolvedValue({ items: [proxy], total: 1, page: 1, page_size: 20, pages: 1 })
    getAllWithCount.mockResolvedValue([])
    listProxyGroups.mockResolvedValue(proxyGroups)
    createProxy.mockResolvedValue(proxy)
    updateProxy.mockResolvedValue(proxy)
    batchGroup.mockResolvedValue({ updated: 1 })
  })

  it('shows group badges and sends mutually exclusive group filters', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="proxy-group-badge"]').text()).toBe('Primary')

    const filter = wrapper.get<HTMLSelectElement>('[data-testid="proxy-group-list-filter"]')
    await filter.setValue('7')
    await flushPromises()
    expect(listProxies.mock.calls.at(-1)?.[2]).toMatchObject({
      group_id: 7,
      ungrouped: undefined
    })

    await filter.setValue('ungrouped')
    await flushPromises()
    expect(listProxies.mock.calls.at(-1)?.[2]).toMatchObject({
      group_id: undefined,
      ungrouped: true
    })
  })

  it('creates a proxy in the selected group and clears the group while editing', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="proxy-create-open"]').trigger('click')
    const createForm = wrapper.get('#create-proxy-form')
    const textInputs = createForm.findAll<HTMLInputElement>('input[type="text"]')
    await textInputs[0].setValue('New Proxy')
    await textInputs[1].setValue('new.example.com')
    await createForm.get('input[type="number"]').setValue('8080')
    await createForm.get<HTMLSelectElement>('[data-testid="proxy-create-group"]').setValue('7')
    await createForm.trigger('submit')
    await flushPromises()

    expect(createProxy).toHaveBeenCalledWith(expect.objectContaining({ proxy_group_id: 7 }))

    const editButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('common.edit'))
    expect(editButton).toBeDefined()
    await editButton!.trigger('click')
    const editForm = wrapper.get('#edit-proxy-form')
    await editForm.get<HTMLSelectElement>('[data-testid="proxy-edit-group"]').setValue('')
    await editForm.trigger('submit')
    await flushPromises()

    expect(updateProxy).toHaveBeenCalledWith(11, expect.objectContaining({ proxy_group_id: null }))
  })

  it('moves selected proxies to a group and clears the selection', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="proxy-row"] input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-testid="proxy-batch-move-open"]').trigger('click')
    await wrapper.get<HTMLSelectElement>('[data-testid="proxy-batch-group-target"]').setValue('7')
    await wrapper.get('[data-testid="proxy-batch-group-submit"]').trigger('click')
    await flushPromises()

    expect(batchGroup).toHaveBeenCalledWith([11], 7)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="proxy-batch-move-open"]').element.disabled).toBe(true)
  })

  it('rejects batch testing more than 100 filtered proxies before fetching all pages', async () => {
    listProxies.mockResolvedValueOnce({
      items: [proxy],
      total: 101,
      page: 1,
      page_size: 20,
      pages: 6
    })
    const wrapper = mountView()
    await flushPromises()

    expect(listProxies).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="proxy-page-batch-test"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.proxies.batchTestLimitExceeded')
    expect(listProxies).toHaveBeenCalledTimes(1)
  })

  it('rechecks the batch limit after fetching when the filtered result grows', async () => {
    const grownResult = Array.from({ length: 101 }, (_, index) => ({
      ...proxy,
      id: index + 100,
      name: `Proxy ${index + 100}`
    }))
    listProxies
      .mockResolvedValueOnce({ items: [proxy], total: 1, page: 1, page_size: 20, pages: 1 })
      .mockResolvedValueOnce({ items: grownResult, total: 101, page: 1, page_size: 200, pages: 1 })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="proxy-page-batch-test"]').trigger('click')
    await flushPromises()

    expect(listProxies).toHaveBeenCalledTimes(2)
    expect(showError).toHaveBeenCalledWith('admin.proxies.batchTestLimitExceeded')
    expect(testProxy).not.toHaveBeenCalled()
  })
})
