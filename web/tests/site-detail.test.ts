import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ComponentRegistryPanel from '../components/sites/ComponentRegistryPanel.vue'
import TaskList from '../components/sites/TaskList.vue'
import SiteDetailPage from '../pages/workspaces/[workspaceId]/sites/[siteId].vue'

const readyRegistry = {
  status: 'ready' as const,
  canRebuild: true,
  components: [{
    id: 'hero',
    label: 'Hero',
    description: 'Lead block',
    source: 'app/components/content/Hero.vue',
    propCount: 2,
    allowChildren: true,
  }],
}

afterEach(() => vi.unstubAllGlobals())

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

  it('marks only the active task busy while preserving compact metadata', () => {
    const wrapper = mount(TaskList, {
      props: {
        busyTask: 'script:build',
        tasks: [
          { id: 'script:build', name: 'Build', kind: 'build', longRunning: false },
          { id: 'script:dev', name: 'Dev server', kind: 'dev', longRunning: true },
        ],
      },
    })

    const rows = wrapper.findAll('.task-row')
    expect(rows[0]!.attributes('aria-busy')).toBe('true')
    expect(rows[0]!.text()).toContain('Finite job · build')
    expect(rows[0]!.get('button').attributes()).toHaveProperty('disabled')
    expect(rows[1]!.attributes('aria-busy')).toBe('false')
    expect(rows[1]!.get('button').attributes()).not.toHaveProperty('disabled')
  })

  it('renders the component ledger and emits explicit actions', async () => {
    const wrapper = mount(ComponentRegistryPanel, {
      props: { registry: readyRegistry, rebuilding: false, openingId: '', error: '' },
    })

    expect(wrapper.get('[data-registry-status]').text()).toContain('Ready')
    expect(wrapper.text()).toContain('1 component')
    expect(wrapper.text()).toContain('2 properties')
    expect(wrapper.text()).toContain('Accepts children')
    expect(wrapper.text()).toContain('app/components/content/Hero.vue')
    await wrapper.get('[data-action="rebuild-registry"]').trigger('click')
    await wrapper.get('[data-component="hero"]').trigger('click')
    expect(wrapper.emitted('rebuild')).toHaveLength(1)
    expect(wrapper.emitted('open')?.[0]).toEqual(['hero'])
  })

  it.each([
    ['stale', 'Definitions may not match current Vue sources.'],
    ['missing', 'The component registry has not been generated.'],
    ['invalid', 'The component registry could not be trusted.'],
  ] as const)('renders %s guidance and keeps rebuild available', (status, copy) => {
    const wrapper = mount(ComponentRegistryPanel, {
      props: {
        registry: { status, canRebuild: true, components: [], problem: copy },
        rebuilding: false,
        openingId: '',
        error: '',
      },
    })

    expect(wrapper.text()).toContain(copy)
    expect(wrapper.get('[data-action="rebuild-registry"]').element).toBeInstanceOf(HTMLButtonElement)
  })

  it('renders valid empty, disabled action, and panel-scoped errors', () => {
    const wrapper = mount(ComponentRegistryPanel, {
      props: {
        registry: { status: 'ready', canRebuild: true, components: [] },
        rebuilding: true,
        openingId: '',
        error: 'Could not rebuild the registry.',
      },
    })

    expect(wrapper.text()).toContain('No local content components registered.')
    expect(wrapper.get('[data-action="rebuild-registry"]').text()).toContain('Rebuilding')
    expect(wrapper.get('[data-action="rebuild-registry"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[role="alert"]').text()).toContain('Could not rebuild')
  })

  it('disables only the component currently being opened', () => {
    const hero = readyRegistry.components[0]!
    const wrapper = mount(ComponentRegistryPanel, {
      props: {
        registry: {
          ...readyRegistry,
          components: [
            hero,
            { ...hero, id: 'parallax', label: 'Parallax' },
          ],
        },
        rebuilding: false,
        openingId: 'hero',
        error: '',
      },
    })

    expect(wrapper.get('[data-component="hero"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[data-component="hero"]').text()).toContain('Opening')
    expect(wrapper.get('[data-component="parallax"]').attributes()).not.toHaveProperty('disabled')
  })

  it('loads components, refreshes them after rebuild, and scopes open errors to the registry panel', async () => {
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/tasks')) return [{ id: 'script:build', name: 'Build', kind: 'build', longRunning: false }]
      if (path.endsWith('/components')) return readyRegistry
      if (path.startsWith('/jobs/')) return {
        id: 'registry-job', type: 'component-registry.rebuild', status: 'succeeded',
        createdAt: '2026-07-14T12:00:00Z', events: [],
      }
      return {
        id: 'shop', name: 'Shop', root: '/workspace/apps/shop', packageName: '@fixture/shop',
        packageManager: 'bun', modules: ['content'], scripts: {}, dependencies: {}, devDependencies: {},
        dependenciesInstalled: true, lockfiles: ['bun.lock'], warnings: [],
      }
    })
    const post = vi.fn(async (path: string) => {
      if (path.endsWith('/registry/rebuild')) return {
        id: 'registry-job', type: 'component-registry.rebuild', status: 'running',
        createdAt: '2026-07-14T12:00:00Z', events: [],
      }
      if (path.endsWith('/hero/open')) throw new Error('Editor is unavailable.')
      return {}
    })
    let jobListener: ((event: unknown) => void) | undefined
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'w1', siteId: 'shop' } }))
    vi.stubGlobal('useApi', () => ({ get, post }))
    vi.stubGlobal('useActivity', () => ({
      subscribe: (_kind: string, _id: string, listener: (event: unknown) => void) => {
        jobListener = listener
        return () => undefined
      },
    }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage)
    await flushPromises()

    expect(get.mock.calls.map(([path]) => path)).toEqual(expect.arrayContaining([
      '/workspaces/w1/sites/shop',
      '/workspaces/w1/sites/shop/tasks',
      '/workspaces/w1/sites/shop/components',
    ]))
    await wrapper.get('[data-action="rebuild-registry"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/workspaces/w1/sites/shop/components/registry/rebuild')
    jobListener?.({})
    await flushPromises()
    expect(get.mock.calls.filter(([path]) => path.endsWith('/components'))).toHaveLength(2)

    await wrapper.get('[data-component="hero"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/workspaces/w1/sites/shop/components/hero/open')
    expect(wrapper.get('.component-registry [role="alert"]').text()).toContain('Editor is unavailable.')
  })
})
