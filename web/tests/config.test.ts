import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ConfigEditor from '../components/config/ConfigEditor.vue'

describe('configuration flow', () => {
  it('shows layered provenance and requires review before save', async () => {
    const wrapper = mount(ConfigEditor, {
      props: {
        effective: {
          config: { workspace: { defaultRoot: '/apps' }, prompts: { defaultLayerSelection: ['content'] }, repos: {}, modules: {} },
          sources: { 'workspace.defaultRoot': 'global' },
        },
      },
    })

    expect(wrapper.text()).toContain('global')
    await wrapper.get('[name="defaultRoot"]').setValue('/projects')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('Review configuration')
    expect(wrapper.emitted('save')).toBeUndefined()
    await wrapper.get('[data-test="confirm-config"]').trigger('click')
    expect(wrapper.emitted('save')?.[0]).toEqual([{ workspace: { defaultRoot: '/projects' } }])
  })
})
