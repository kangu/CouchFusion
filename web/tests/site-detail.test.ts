import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ComponentRegistryPanel from '../components/sites/ComponentRegistryPanel.vue'
import ComponentPreviewOverlay from '../components/sites/ComponentPreviewOverlay.vue'
import TaskList from '../components/sites/TaskList.vue'
import SiteDetailPage from '../pages/workspaces/[workspaceId]/sites/[siteId].vue'
import type { AgentConversationEvent } from '../types/api'

const goBootstrap = { load: async () => undefined, isRust: { value: false }, has: () => false, state: { value: undefined } }

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

const stoppedPreviewHost = { status: 'stopped' as const }
const readyPreviewHost = {
  status: 'ready' as const,
  processId: 'preview-1',
  url: 'http://127.0.0.1:44001/__couchfusion/components/preview?token=capability',
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
  document.body.style.overflow = ''
})

class AgentEventSource {
  static sources: AgentEventSource[] = []
  readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  closed = false
  onerror: ((event: Event) => void) | null = null
  onopen: ((event: Event) => void) | null = null

  constructor(readonly url: string) {
    AgentEventSource.sources.push(this)
  }

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    const listeners = this.listeners.get(type) ?? new Set()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  }

  close() { this.closed = true }

  emit(event: AgentConversationEvent) {
    const message = new MessageEvent('conversation', { data: JSON.stringify(event), lastEventId: String(event.sequence) }) as MessageEvent<string>
    for (const listener of this.listeners.get('conversation') ?? []) listener(message)
  }
}

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

  it('keeps preview explicit and enables component previews only when ready', async () => {
    const stopped = mount(ComponentRegistryPanel, {
      props: {
        registry: readyRegistry,
        rebuilding: false,
        openingId: '',
        error: '',
        previewHost: stoppedPreviewHost,
        previewBusy: false,
      },
    })

    expect(stopped.get('[data-action="start-preview-host"]').text()).toContain('Start preview host')
    expect(stopped.get('[data-preview-component="hero"]').attributes()).toHaveProperty('disabled')
    await stopped.get('[data-action="start-preview-host"]').trigger('click')
    expect(stopped.emitted('start-preview-host')).toHaveLength(1)

    const ready = mount(ComponentRegistryPanel, {
      props: {
        registry: readyRegistry,
        rebuilding: false,
        openingId: '',
        error: '',
        previewHost: readyPreviewHost,
        previewBusy: false,
      },
    })
    expect(ready.get('[data-action="stop-preview-host"]').text()).toContain('Stop preview host')
    await ready.get('[data-preview-component="hero"]').trigger('click')
    expect(ready.emitted('preview')?.[0]?.[0]).toBe('hero')
    expect(ready.emitted('preview')?.[0]?.[1]).toBeInstanceOf(HTMLButtonElement)
  })

  it('shows starting and failed preview-host states without hiding recovery', () => {
    const starting = mount(ComponentRegistryPanel, {
      props: {
        registry: readyRegistry,
        rebuilding: false,
        openingId: '',
        error: '',
        previewHost: { status: 'starting', processId: 'preview-1' },
        previewBusy: true,
      },
    })
    expect(starting.get('[data-preview-host-status]').text()).toContain('Starting')
    expect(starting.get('[data-preview-component="hero"]').attributes()).toHaveProperty('disabled')

    const failed = mount(ComponentRegistryPanel, {
      props: {
        registry: readyRegistry,
        rebuilding: false,
        openingId: '',
        error: '',
        previewHost: { status: 'failed', problem: 'Nuxt did not become ready.' },
        previewBusy: false,
      },
    })
    expect(failed.text()).toContain('Nuxt did not become ready.')
    expect(failed.get('[data-action="start-preview-host"]').element).toBeInstanceOf(HTMLButtonElement)
  })

  it('renders an accessible fullscreen preview and restores the originating focus', async () => {
    const origin = document.createElement('button')
    origin.textContent = 'Preview Hero'
    document.body.append(origin)
    origin.focus()

    const wrapper = mount(ComponentPreviewOverlay, {
      props: {
        open: true,
        component: readyRegistry.components[0]!,
        previewHost: readyPreviewHost,
        returnFocus: origin,
      },
      global: { stubs: { Teleport: true } },
    })

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('aria-label')).toContain('Preview Hero')
    expect(wrapper.get('[data-component-preview-frame]').attributes('src')).toBe(
      'http://127.0.0.1:44001/__couchfusion/components/preview/hero?token=capability',
    )
    expect(document.body.style.overflow).toBe('hidden')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
    expect(document.body.style.overflow).toBe('')
    expect(document.activeElement).toBe(origin)
  })

  it('keeps close and restart actions available after the preview host stops', async () => {
    const wrapper = mount(ComponentPreviewOverlay, {
      props: {
        open: true,
        component: readyRegistry.components[0]!,
        previewHost: { status: 'failed', problem: 'Preview host exited.' },
      },
      global: { stubs: { Teleport: true } },
    })

    expect(wrapper.get('[role="alert"]').text()).toContain('Preview host exited.')
    await wrapper.get('[data-action="restart-preview-host"]').trigger('click')
    await wrapper.get('[data-action="close-component-preview"]').trigger('click')
    expect(wrapper.emitted('restartHost')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
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
      if (path.endsWith('/components/preview-host')) return stoppedPreviewHost
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
    vi.stubGlobal('useBootstrap', () => goBootstrap)
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
      '/workspaces/w1/sites/shop/components/preview-host',
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
    expect(wrapper.get('.component-registry__error[role="alert"]').text()).toContain('Editor is unavailable.')
  })

  it('monitors a ready host while fullscreen preview is open and exposes crash recovery', async () => {
    vi.useFakeTimers()
    let previewReads = 0
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/tasks')) return []
      if (path.endsWith('/components/preview-host')) {
        previewReads++
        return previewReads === 1
          ? readyPreviewHost
          : { status: 'failed', problem: 'Preview host exited.' }
      }
      if (path.endsWith('/components')) return readyRegistry
      return {
        id: 'shop', name: 'Shop', root: '/workspace/apps/shop', packageName: '@fixture/shop',
        packageManager: 'bun', modules: ['content'], scripts: { dev: 'nuxt dev' },
        dependencies: {}, devDependencies: {}, dependenciesInstalled: true,
        lockfiles: ['bun.lock'], warnings: [],
      }
    })
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'w1', siteId: 'shop' } }))
    vi.stubGlobal('useApi', () => ({ get, post: vi.fn() }))
    vi.stubGlobal('useBootstrap', () => goBootstrap)
    vi.stubGlobal('useActivity', () => ({ subscribe: vi.fn() }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage, { global: { stubs: { Teleport: true } } })
    await flushPromises()
    await wrapper.get('[data-preview-component="hero"]').trigger('click')
    expect(wrapper.get('[data-component-preview-frame]').element).toBeInstanceOf(HTMLIFrameElement)

    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()

    expect(previewReads).toBe(2)
    expect(wrapper.get('.component-preview-overlay [role="alert"]').text()).toContain('Preview host exited.')
    wrapper.unmount()
  })

  it('integrates generation before the ledger with idempotent create and retained-thread opening', async () => {
    AgentEventSource.sources = []
    vi.stubGlobal('EventSource', AgentEventSource)
    const retained = {
      id: 'older', workspaceId: 'w1', siteId: 'shop', title: 'Older component', status: 'ready',
      registry: { state: 'succeeded' }, sequence: 4, bindingValid: true,
      createdAt: '2026-07-14T11:00:00Z', updatedAt: '2026-07-14T11:02:00Z',
    }
    const created = {
      id: 'c1', workspaceId: 'w1', siteId: 'shop', title: 'New hero', status: 'active',
      registry: { state: 'not_needed' }, sequence: 1, bindingValid: true,
      createdAt: '2026-07-14T12:00:00Z', updatedAt: '2026-07-14T12:00:00Z',
    }
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/agent-conversations')) return { readiness: { status: 'ready', available: true }, conversations: [retained] }
      if (path === '/agent-conversations/older') return retained
      if (path.endsWith('/tasks')) return []
      if (path.endsWith('/components/preview-host')) return stoppedPreviewHost
      if (path.endsWith('/components')) return readyRegistry
      return {
        id: 'shop', name: 'Shop', root: '/workspace/apps/shop', packageName: '@fixture/shop', packageManager: 'bun',
        modules: ['content'], scripts: {}, dependencies: {}, devDependencies: {}, dependenciesInstalled: true, lockfiles: [], warnings: [],
      }
    })
    const post = vi.fn(async (path: string, _body?: unknown, _options?: { headers?: Record<string, string> }) => path.endsWith('/agent-conversations') ? created : {})
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'w1', siteId: 'shop' } }))
    vi.stubGlobal('useApi', () => ({ get, post }))
    vi.stubGlobal('useBootstrap', () => goBootstrap)
    vi.stubGlobal('useActivity', () => ({ subscribe: vi.fn() }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage)
    await flushPromises()
    const generator = wrapper.get('.component-generator')
    const ledger = wrapper.get('.component-ledger')
    expect(generator.element.compareDocumentPosition(ledger.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    await generator.get('textarea').setValue('Build a compact hero')
    await generator.get('form').trigger('submit')
    await flushPromises()
    const createCall = post.mock.calls.find(([path]) => path.endsWith('/agent-conversations'))
    expect(createCall?.[2]?.headers?.['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    expect(post.mock.calls.some(([path]) => path.includes('preview-host/start'))).toBe(false)

    await wrapper.get('[data-conversation="older"] button').trigger('click')
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/agent-conversations/older')
    expect(AgentEventSource.sources.at(-1)?.url).toContain('/agent-conversations/older/events?after=4')
  })

  it('refreshes and highlights the generated registry component once without starting preview', async () => {
    AgentEventSource.sources = []
    vi.stubGlobal('EventSource', AgentEventSource)
    let componentReads = 0
    const active = {
      id: 'c1', workspaceId: 'w1', siteId: 'shop', title: 'Hero', status: 'active',
      registry: { state: 'not_needed' }, sequence: 1, bindingValid: true,
      createdAt: '2026-07-14T12:00:00Z', updatedAt: '2026-07-14T12:00:00Z',
    }
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/agent-conversations')) return { readiness: { status: 'ready', available: true }, conversations: [active] }
      if (path === '/agent-conversations/c1') return active
      if (path.endsWith('/tasks')) return []
      if (path.endsWith('/components/preview-host')) return stoppedPreviewHost
      if (path.endsWith('/components')) { componentReads++; return readyRegistry }
      return {
        id: 'shop', name: 'Shop', root: '/workspace/apps/shop', packageName: '@fixture/shop', packageManager: 'bun',
        modules: ['content'], scripts: {}, dependencies: {}, devDependencies: {}, dependenciesInstalled: true, lockfiles: [], warnings: [],
      }
    })
    const post = vi.fn()
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'w1', siteId: 'shop' } }))
    vi.stubGlobal('useApi', () => ({ get, post }))
    vi.stubGlobal('useBootstrap', () => goBootstrap)
    vi.stubGlobal('useActivity', () => ({ subscribe: vi.fn() }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage)
    await flushPromises()
    await wrapper.get('[data-conversation="c1"] button').trigger('click')
    await flushPromises()
    const completed = { ...active, status: 'ready' as const, sequence: 2, generatedComponentId: 'hero', registry: { state: 'succeeded' as const, generatedComponentId: 'hero' } }
    AgentEventSource.sources.at(-1)?.emit({
      sequence: 2, type: 'conversation.snapshot', conversationId: 'c1', snapshot: completed,
      createdAt: '2026-07-14T12:01:00Z',
    })
    await flushPromises()

    expect(componentReads).toBe(2)
    expect(wrapper.get('[data-component="hero"]').element.closest('.component-row')?.classList.contains('component-row--generated')).toBe(true)
    expect(post.mock.calls.some(([path]) => path.includes('preview-host/start'))).toBe(false)
    wrapper.unmount()
    expect(AgentEventSource.sources.at(-1)?.closed).toBe(true)
  })

  it('preserves changed files and offers registry retry after generation rebuild failure', async () => {
    AgentEventSource.sources = []
    vi.stubGlobal('EventSource', AgentEventSource)
    const failed = {
      id: 'c1', workspaceId: 'w1', siteId: 'shop', title: 'Hero', status: 'ready',
      registry: { state: 'failed', problem: 'Registry generator exited.' }, sequence: 3, bindingValid: true,
      changedFiles: [{ path: 'app/components/content/Hero.vue', kind: 'added' }],
      createdAt: '2026-07-14T12:00:00Z', updatedAt: '2026-07-14T12:02:00Z',
    }
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/agent-conversations')) return { readiness: { status: 'ready', available: true }, conversations: [failed] }
      if (path === '/agent-conversations/c1') return failed
      if (path.endsWith('/tasks')) return []
      if (path.endsWith('/components/preview-host')) return stoppedPreviewHost
      if (path.endsWith('/components')) return readyRegistry
      return {
        id: 'shop', name: 'Shop', root: '/workspace/apps/shop', modules: ['content'], scripts: {}, dependencies: {}, devDependencies: {},
        dependenciesInstalled: true, lockfiles: [], warnings: [],
      }
    })
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'w1', siteId: 'shop' } }))
    vi.stubGlobal('useApi', () => ({ get, post: vi.fn() }))
    vi.stubGlobal('useBootstrap', () => goBootstrap)
    vi.stubGlobal('useActivity', () => ({ subscribe: vi.fn() }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage)
    await flushPromises()
    await wrapper.get('[data-conversation="c1"] button').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-action="retry-agent-registry"]').text()).toContain('Retry rebuild')
    expect(wrapper.get('[data-file-group="conversation"]').text()).toContain('app/components/content/Hero.vue')
  })

  it('closes agent SSE and the active task subscription when the page unmounts', async () => {
    AgentEventSource.sources = []
    vi.stubGlobal('EventSource', AgentEventSource)
    const unsubscribe = vi.fn()
    const retained = {
      id: 'c1', workspaceId: 'w1', siteId: 'shop', title: 'Hero', status: 'ready', registry: { state: 'not_needed' },
      sequence: 1, bindingValid: true, createdAt: '2026-07-14T12:00:00Z', updatedAt: '2026-07-14T12:00:00Z',
    }
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/agent-conversations')) return { readiness: { status: 'ready', available: true }, conversations: [retained] }
      if (path === '/agent-conversations/c1') return retained
      if (path.endsWith('/tasks')) return [{ id: 'script:dev', name: 'Dev server', kind: 'script', longRunning: true }]
      if (path.endsWith('/components/preview-host')) return stoppedPreviewHost
      if (path.endsWith('/components')) return readyRegistry
      return {
        id: 'shop', name: 'Shop', root: '/workspace/apps/shop', modules: ['content'], scripts: {}, dependencies: {}, devDependencies: {},
        dependenciesInstalled: true, lockfiles: [], warnings: [],
      }
    })
    const post = vi.fn(async (path: string) => path.endsWith('/processes/script%3Adev/start') ? {
      id: 'process-1', workspaceId: 'w1', siteId: 'shop', taskId: 'script:dev', status: 'running',
      startedAt: '2026-07-14T12:00:00Z', urls: [], events: [],
    } : {})
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'w1', siteId: 'shop' } }))
    vi.stubGlobal('useApi', () => ({ get, post }))
    vi.stubGlobal('useBootstrap', () => goBootstrap)
    vi.stubGlobal('useActivity', () => ({ subscribe: vi.fn(() => unsubscribe) }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage)
    await flushPromises()
    await wrapper.get('[data-conversation="c1"] button').trigger('click')
    await wrapper.get('[data-task="script:dev"]').trigger('click')
    await flushPromises()
    wrapper.unmount()

    expect(AgentEventSource.sources.at(-1)?.closed).toBe(true)
    expect(unsubscribe).toHaveBeenCalledOnce()
  })
})

describe('site detail on the Rust control plane', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('shows the dev session, production and components, and skips Go-only routes', async () => {
    const get = vi.fn(async (path: string) => {
      if (path.endsWith('/components')) return { status: 'ready', canRebuild: true, components: [{ id: 'network-hero', label: 'Network hero', description: '', source: 'app/components/content/NetworkHero.vue', propCount: 4, allowChildren: false }] }
      if (path.endsWith('/dev')) return { siteId: 'radustanciu', unit: 'cf-dev@radustanciu.service', status: 'ready', url: 'https://hydra1.example.ts.net:9181' }
      if (path.endsWith('/prod')) return { siteId: 'radustanciu', unit: 'cf-site@radustanciu.service', port: 4501, service: { unit: 'cf-site@radustanciu.service', description: '', kind: 'prod', activeState: 'active', subState: 'running' }, releases: [], admission: { ok: true }, previewUrl: 'https://hydra1.example.ts.net:9189/', publicUrl: 'https://radustanciu.ro' }
      return { id: 'radustanciu', name: 'radustanciu', root: '/srv/couchfusion/workspace/apps/radustanciu', packageManager: 'bun', modules: [], scripts: {}, dependencies: {}, devDependencies: {}, dependenciesInstalled: true, lockfiles: ['bun.lock'], warnings: [] }
    })
    vi.stubGlobal('definePageMeta', vi.fn())
    vi.stubGlobal('useRoute', () => ({ params: { workspaceId: 'hydra1', siteId: 'radustanciu' } }))
    vi.stubGlobal('useApi', () => ({ get, post: vi.fn() }))
    vi.stubGlobal('useBootstrap', () => ({ load: async () => undefined, isRust: { value: true }, has: (c: string) => c === 'prod-releases', state: { value: { agent: { model: 'gpt-5.6-sol', reasoningEffort: null } } } }))
    vi.stubGlobal('useActivity', () => ({ subscribe: vi.fn() }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn(), trackProcess: vi.fn() }))

    const wrapper = mount(SiteDetailPage, { global: { stubs: { Teleport: true, NuxtLink: { template: '<a><slot /></a>' } } } })
    await flushPromises()
    const paths = get.mock.calls.map(([path]) => path)
    expect(paths.some((path) => path.endsWith('/tasks') || path.includes('preview-host'))).toBe(false)
    const view = wrapper.get('[data-testid="site-rust"]')
    expect(view.text()).toContain('Open dev page')
    expect(view.text()).toContain('gpt-5.6-sol')
    expect(view.text()).toContain('network-hero')
    expect(wrapper.text()).not.toContain('Could not')
    const links = wrapper.get('[data-testid="site-links"]').findAll('a').map((a) => [a.text(), a.attributes('href')])
    expect(links).toEqual([
      ['Dev site ↗', 'https://hydra1.example.ts.net:9181/'],
      ['Dev builder ↗', 'https://hydra1.example.ts.net:9181/builder/'],
      ['Production preview ↗', 'https://hydra1.example.ts.net:9189/'],
      ['Public site ↗', 'https://radustanciu.ro'],
    ])
    wrapper.unmount()
  })
})
