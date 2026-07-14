import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SiteWizard from '../components/provision/SiteWizard.vue'

describe('site provisioning wizard', () => {
  it('requires a blank-by-default two-letter database login prefix and submits it raw', async () => {
    const wrapper = mount(SiteWizard, { props: { open: true, modules: ['content'] } })
    const prefix = wrapper.get<HTMLInputElement>('[name="dbLoginPrefix"]')
    const form = wrapper.get('form')

    expect(prefix.element.value).toBe('')
    expect(wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled).toBe(true)

    await wrapper.get('[name="name"]').setValue('catalog')
    await prefix.setValue('A1')
    await form.trigger('submit')
    expect(wrapper.text()).toContain('Site provisioning · 1/3')

    await prefix.setValue('ac')
    await form.trigger('submit')
    expect(wrapper.text()).toContain('Optional modules')

    await form.trigger('submit')
    expect(wrapper.text()).toContain('Review site')
    expect(wrapper.text()).toContain('ac-')

    await form.trigger('submit')
    expect(wrapper.emitted('submit')?.[0]).toEqual([{
      name: 'catalog',
      modules: [],
      dbLoginPrefix: 'ac',
      branch: 'main',
      force: false,
    }])
  })
})
