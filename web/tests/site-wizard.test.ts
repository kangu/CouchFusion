import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SiteWizard from '../components/provision/SiteWizard.vue'
import SiteProvisionForm from '../components/provision/SiteProvisionForm.vue'

describe('site provisioning wizard', () => {
  it('requires a blank-by-default two-letter database login prefix and submits it raw', async () => {
    const wrapper = mount(SiteWizard, { props: { open: true, modules: ['content'] } })
    const prefix = wrapper.get<HTMLInputElement>('[name="dbLoginPrefix"]')
    const form = wrapper.get('form')

    expect(prefix.element.value).toBe('')
    expect(prefix.attributes()).toMatchObject({
      required: '',
      minlength: '2',
      maxlength: '2',
      pattern: '[a-z]{2}',
    })
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

it('locks Content and Auth into onboarding site submissions', async () => {
  const wrapper = mount(SiteProvisionForm, { props: { modules: ['content', 'auth', 'commerce'], requiredModules: ['content', 'auth'] } })
  await wrapper.get('[name="name"]').setValue('catalog')
  await wrapper.get('[name="dbLoginPrefix"]').setValue('ac')
  await wrapper.get('form').trigger('submit')
  expect(wrapper.findAll('input[disabled]')).toHaveLength(2)
  await wrapper.get('form').trigger('submit')
  await wrapper.get('form').trigger('submit')
  expect(wrapper.emitted('submit')?.[0]?.[0]).toMatchObject({ modules: ['content', 'auth'] })
})
