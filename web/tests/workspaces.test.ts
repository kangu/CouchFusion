import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import WorkspaceDialog from '../components/workspaces/WorkspaceDialog.vue'

describe('workspace flows', () => {
  it('keeps registration explicit and submits the entered absolute path', async () => {
    const wrapper = mount(WorkspaceDialog, { props: { mode: 'register', open: true } })
    await wrapper.get('[name="root"]').setValue('/work/couchfusion')
    await wrapper.get('[name="name"]').setValue('Storefront')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('submit')?.[0]).toEqual([{ root: '/work/couchfusion', name: 'Storefront' }])
  })

  it('reviews initialization before emitting a filesystem operation', async () => {
    const wrapper = mount(WorkspaceDialog, { props: { mode: 'initialize', open: true } })
    await wrapper.get('[name="root"]').setValue('/work/new-stack')
    await wrapper.get('[name="name"]').setValue('New stack')
    await wrapper.get('[name="layerBranch"]').setValue('main')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.text()).toContain('Review initialization')
    expect(wrapper.emitted('submit')).toBeUndefined()
    await wrapper.get('[data-test="confirm-initialize"]').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([{ root: '/work/new-stack', name: 'New stack', layerBranch: 'main', force: false }])
  })
})
