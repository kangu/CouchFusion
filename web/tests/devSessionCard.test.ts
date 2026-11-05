import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DevSessionCard from '../components/sites/DevSessionCard.vue'
import type { DevSessionView } from '../types/api'

const base = '/workspaces/hydra1/sites/radustanciu/dev'

function view(overrides: Partial<DevSessionView> = {}): DevSessionView {
  return { siteId: 'radustanciu', unit: 'cf-dev@radustanciu.service', status: 'stopped', url: '', ...overrides }
}

describe('dev session card', () => {
  let current: DevSessionView
  const get = vi.fn()
  const post = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    current = view()
    get.mockImplementation(async () => current)
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/start')) current = view({ status: 'starting', startedAt: '2026-11-05T09:00:00Z' })
      if (path.endsWith('/stop')) current = view({ status: 'stopped' })
      return current
    })
    vi.stubGlobal('useApi', () => ({ get, post }))
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('starts a stopped dev server and follows it to running', async () => {
    const wrapper = mount(DevSessionCard, { props: { workspaceId: 'hydra1', siteId: 'radustanciu' } })
    await flushPromises()
    expect(wrapper.get('h2').text()).toBe('Stopped')
    await wrapper.get('[data-testid="dev-card-start"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(`${base}/start`, {})
    expect(wrapper.get('h2').text()).toBe('Starting…')
    current = view({ status: 'ready', startedAt: '2026-11-05T09:00:00Z', memoryBytes: 600 * 1024 * 1024 })
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()
    expect(wrapper.get('h2').text()).toBe('Running')
    expect(wrapper.get('[data-testid="dev-card-detail"]').text()).toContain('MiB')
    expect(wrapper.emitted('update')!.at(-1)![0]).toMatchObject({ status: 'ready' })
  })

  it('asks before stopping and says production keeps running', async () => {
    current = view({ status: 'ready' })
    const wrapper = mount(DevSessionCard, { props: { workspaceId: 'hydra1', siteId: 'radustanciu' } })
    await flushPromises()
    await wrapper.get('[data-testid="dev-card-stop"]').trigger('click')
    expect(post).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Production keeps running')
    await wrapper.get('[data-testid="dev-card-stop-confirm"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(`${base}/stop`, {})
    expect(wrapper.get('h2').text()).toBe('Stopped')
  })

  it('shows why a stop was refused', async () => {
    current = view({ status: 'ready' })
    post.mockRejectedValueOnce(new Error('A llm.request job is using the dev server; wait for it to finish or cancel it first.'))
    const wrapper = mount(DevSessionCard, { props: { workspaceId: 'hydra1', siteId: 'radustanciu' } })
    await flushPromises()
    await wrapper.get('[data-testid="dev-card-stop"]').trigger('click')
    await wrapper.get('[data-testid="dev-card-stop-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('using the dev server')
    expect(wrapper.get('h2').text()).toBe('Running')
  })
})
