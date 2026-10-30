import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DevDeleteModal from '../components/dev/DevDeleteModal.vue'
import { ApiClientError } from '../composables/useApi'
import {
  canConfirmDelete,
  createComponentDelete,
  deleteRequestBody,
  usageRequiresAcknowledgement,
} from '../composables/useComponentDelete'
import type { ComponentUsage } from '../types/api'

function usage(overrides: Partial<ComponentUsage> = {}): ComponentUsage {
  return {
    componentId: 'network-hero',
    source: 'app/components/content/NetworkHero.vue',
    sha256: 'a'.repeat(64),
    git: 'tracked_clean',
    pages: [],
    codeReferences: [],
    pageScan: 'ok',
    ...overrides,
  }
}

describe('delete acknowledgement rules', () => {
  it('needs no acknowledgement for an unused component with a completed page scan', () => {
    expect(usageRequiresAcknowledgement(usage())).toBe(false)
    expect(canConfirmDelete(usage(), false)).toBe(true)
    expect(deleteRequestBody(usage(), false)).toEqual({ expectedSha256: 'a'.repeat(64), acknowledgeUsage: false })
  })

  it.each([
    ['page usage', { pages: [{ id: 'page-/', path: '/', title: 'Home' }] }],
    ['code references', { codeReferences: [{ path: 'app/components/content/Landing.vue', line: 12 }] }],
    ['an unavailable page scan', { pageScan: 'unavailable' as const }],
  ])('requires acknowledgement for %s', (_label, overrides) => {
    const current = usage(overrides)
    expect(usageRequiresAcknowledgement(current)).toBe(true)
    expect(canConfirmDelete(current, false)).toBe(false)
    expect(canConfirmDelete(current, true)).toBe(true)
    expect(deleteRequestBody(current, true).acknowledgeUsage).toBe(true)
  })

  it('never confirms without usage data', () => {
    expect(canConfirmDelete(undefined, true)).toBe(false)
  })

  it('sends the sha it showed and re-reads usage after a conflict', async () => {
    const api = {
      get: vi.fn().mockResolvedValueOnce(usage({ pages: [{ id: 'p', path: '/', title: 'Home' }] })).mockResolvedValueOnce(usage({ sha256: 'b'.repeat(64) })),
      delete: vi.fn().mockRejectedValue(new ApiClientError({ code: 'conflict', message: 'file changed since you looked' })),
    }
    const flow = createComponentDelete(api, 'hetzner1', 'radustanciu')
    await flow.open('network-hero', 'req-1')
    expect(api.get).toHaveBeenCalledWith('/workspaces/hetzner1/sites/radustanciu/components/network-hero/usage')
    expect(await flow.confirm()).toBeUndefined()
    expect(api.delete).not.toHaveBeenCalled()
    flow.acknowledged.value = true
    expect(await flow.confirm()).toBeUndefined()
    expect(api.delete).toHaveBeenCalledWith('/workspaces/hetzner1/sites/radustanciu/components/network-hero', { expectedSha256: 'a'.repeat(64), acknowledgeUsage: true })
    expect(flow.usage.value?.sha256).toBe('b'.repeat(64))
    expect(flow.acknowledged.value).toBe(false)
    expect(flow.error.value).toContain('file changed')
  })
})

describe('delete modal', () => {
  const base = { componentId: 'network-hero', loading: false, submitting: false, error: '', acknowledged: false }

  it('enables the delete button for an unused component', () => {
    const wrapper = mount(DevDeleteModal, { props: { ...base, usage: usage() } })
    expect(wrapper.find('[data-testid="ack"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="confirm-delete"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('app/components/content/NetworkHero.vue')
  })

  it('lists usages and keeps delete disabled until "delete anyway" is ticked', async () => {
    const current = usage({ pages: [{ id: 'page-/', path: '/', title: 'Home' }], codeReferences: [{ path: 'app/x.vue', line: 3 }] })
    const wrapper = mount(DevDeleteModal, { props: { ...base, usage: current } })
    expect(wrapper.get('[data-testid="usage-pages"]').text()).toContain('Home')
    expect(wrapper.get('[data-testid="usage-code"]').text()).toContain('app/x.vue:3')
    expect(wrapper.get('[data-testid="confirm-delete"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="ack"]').setValue(true)
    expect(wrapper.emitted('update:acknowledged')?.[0]).toEqual([true])
    await wrapper.setProps({ acknowledged: true })
    expect(wrapper.get('[data-testid="confirm-delete"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="confirm-delete"]').trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })

  it('warns when the page scan was unavailable', () => {
    const wrapper = mount(DevDeleteModal, { props: { ...base, usage: usage({ pageScan: 'unavailable' }) } })
    expect(wrapper.get('[role="alert"]').text()).toContain('could not be checked')
    expect(wrapper.get('[data-testid="confirm-delete"]').attributes('disabled')).toBeDefined()
  })
})
