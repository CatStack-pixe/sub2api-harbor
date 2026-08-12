import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ProxyGroup } from '@/types'
import ProxyGroupManageDialog from '../ProxyGroupManageDialog.vue'

const { createGroup, updateGroup, deleteGroup, showSuccess, showError } = vi.hoisted(() => ({
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
  deleteGroup: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxyGroups: {
      create: createGroup,
      update: updateGroup,
      delete: deleteGroup
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<section v-if="show"><slot /><slot name="footer" /></section>'
})

const ConfirmDialogStub = defineComponent({
  props: { show: Boolean },
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show"><button data-testid="confirm-delete" @click="$emit(\'confirm\')">confirm</button></div>'
})

const groups: ProxyGroup[] = [
  {
    id: 7,
    name: 'Primary',
    total_count: 12,
    active_count: 10,
    inactive_count: 2,
    created_at: '2026-08-08T00:00:00Z',
    updated_at: '2026-08-08T00:00:00Z'
  }
]

const mountDialog = () =>
  mount(ProxyGroupManageDialog, {
    props: { show: true, groups },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        Icon: true
      }
    }
  })

describe('ProxyGroupManageDialog', () => {
  beforeEach(() => {
    for (const mock of [createGroup, updateGroup, deleteGroup, showSuccess, showError]) {
      mock.mockReset()
    }
    createGroup.mockResolvedValue({ id: 8, name: 'New Group' })
    updateGroup.mockResolvedValue({ ...groups[0], name: 'Renamed' })
    deleteGroup.mockResolvedValue({ message: 'ok' })
  })

  it('trims and creates a proxy group', async () => {
    const wrapper = mountDialog()

    await wrapper.get('[data-testid="proxy-group-create-name"]').setValue('  New Group  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(createGroup).toHaveBeenCalledWith({ name: 'New Group' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('renames an existing proxy group', async () => {
    const wrapper = mountDialog()

    await wrapper.get('button[title="common.edit"]').trigger('click')
    await wrapper.get('input[aria-label="admin.proxies.proxyGroups.name"]').setValue(' Renamed ')
    await wrapper.get('button[title="common.save"]').trigger('click')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledWith(7, { name: 'Renamed' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('deletes a group after confirmation', async () => {
    const wrapper = mountDialog()

    await wrapper.get('button[title="common.delete"]').trigger('click')
    await wrapper.get('[data-testid="confirm-delete"]').trigger('click')
    await flushPromises()

    expect(deleteGroup).toHaveBeenCalledWith(7)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })
})
