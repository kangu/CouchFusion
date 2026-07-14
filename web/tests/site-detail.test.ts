import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TaskList from '../components/sites/TaskList.vue'

describe('site detail', () => {
  it('separates finite task runs from managed process starts', async () => {
    const wrapper = mount(TaskList, {
      props: {
        tasks: [
          { id: 'script:build', name: 'Build', kind: 'script', longRunning: false },
          { id: 'script:dev', name: 'Dev server', kind: 'script', longRunning: true },
        ],
      },
    })

    await wrapper.get('[data-task="script:build"]').trigger('click')
    await wrapper.get('[data-task="script:dev"]').trigger('click')
    expect(wrapper.emitted('run')?.[0]).toEqual(['script:build'])
    expect(wrapper.emitted('start')?.[0]).toEqual(['script:dev'])
  })
})
