import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import JobStatus from '../components/activity/JobStatus.vue'
import LogViewer from '../components/activity/LogViewer.vue'

describe('activity surfaces', () => {
  it('offers cancellation only while a job is active', async () => {
    const wrapper = mount(JobStatus, { props: { job: { id: 'j1', type: 'site.create', status: 'running', createdAt: '2026-07-14', events: [] } } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('cancel')?.[0]).toEqual(['j1'])
    await wrapper.setProps({ job: { id: 'j1', type: 'site.create', status: 'succeeded', createdAt: '2026-07-14', events: [] } })
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('renders bounded logs and reconnect state accessibly', () => {
    const wrapper = mount(LogViewer, {
      props: {
        connected: false,
        events: Array.from({ length: 510 }, (_, index) => ({ id: index + 1, time: '', code: 'output', text: `line ${index + 1}` })),
      },
    })
    expect(wrapper.text()).toContain('Reconnecting')
    expect(wrapper.text()).not.toContain('line 1\n')
    expect(wrapper.text()).toContain('line 510')
  })
})
