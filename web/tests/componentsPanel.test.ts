import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DevComponentsDrawer from '../components/dev/DevComponentsDrawer.vue'
import { ApiClientError } from '../composables/useApi'
import {
  canRestore,
  createComponentsPanel,
  rebuildRecommended,
  restoreBlockedReason,
  sortTrash,
} from '../composables/useComponentsPanel'
import type { ComponentRegistryResponse, JobSnapshot, TrashEntry } from '../types/api'

const BASE = '/workspaces/hetzner1/sites/radustanciu/components'

function entry(overrides: Partial<TrashEntry> = {}): TrashEntry {
  return {
    trashId: 'job-1',
    componentId: 'zz-smoke',
    source: 'app/components/content/ZzSmoke.vue',
    sha256: 'a'.repeat(64),
    git: 'untracked',
    deletedAt: '2026-10-30T06:03:11Z',
    restorable: true,
    ...overrides,
  }
}

function job(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return { id: 'job-9', type: 'component.restore', status: 'queued', createdAt: '2026-10-30T07:00:00Z', events: [], ...overrides }
}

const registry: ComponentRegistryResponse = {
  status: 'ready',
  canRebuild: true,
  components: [
    { id: 'phil', label: 'Phil', description: '', source: 'app/components/content/Phil.vue', propCount: 4, allowChildren: false },
    { id: 'contact', label: 'Contact', description: '', source: 'app/components/content/Contact.vue', propCount: 2, allowChildren: false },
  ],
}

function fakeApi(handlers: { get?: (path: string) => unknown; post?: (path: string, body?: unknown) => unknown }) {
  return {
    get: vi.fn().mockImplementation(async (path: string) => handlers.get?.(path)),
    post: vi.fn().mockImplementation(async (path: string, body?: unknown) => handlers.post?.(path, body)),
  }
}

describe('restore eligibility', () => {
  it('allows restore only for restorable entries while nothing else runs', () => {
    expect(canRestore(entry())).toBe(true)
    expect(canRestore(entry(), true)).toBe(false)
    expect(canRestore(entry({ restorable: false }))).toBe(false)
  })

  it('explains why an entry cannot be restored', () => {
    expect(restoreBlockedReason(entry())).toBe('')
    expect(restoreBlockedReason(entry({ restorable: false, problem: 'A file exists at the source path.' }))).toBe('A file exists at the source path.')
    expect(restoreBlockedReason(entry({ restorable: false }))).toContain('ZzSmoke.vue')
  })

  it('sorts trash newest first', () => {
    const sorted = sortTrash([entry({ trashId: 'old', deletedAt: '2026-10-29T10:00:00Z' }), entry({ trashId: 'new', deletedAt: '2026-10-30T10:00:00Z' })])
    expect(sorted.map((e) => e.trashId)).toEqual(['new', 'old'])
  })

  it('recommends a rebuild only when the registry is not ready', () => {
    expect(rebuildRecommended('ready')).toBe(false)
    expect(rebuildRecommended('stale')).toBe(true)
    expect(rebuildRecommended(undefined)).toBe(false)
  })
})

describe('components panel', () => {
  it('loads the registry sorted by label and the trash', async () => {
    const api = fakeApi({ get: (path) => (path === BASE ? registry : [entry()]) })
    const panel = createComponentsPanel(api, 'hetzner1', 'radustanciu')
    await panel.loadRegistry()
    await panel.loadTrash()
    expect(panel.components.value.map((c) => c.id)).toEqual(['contact', 'phil'])
    expect(panel.trash.value).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith(`${BASE}/trash`)
  })

  it('restores through the trash route and refreshes both lists when the job ends', async () => {
    const started = job()
    const api = fakeApi({ get: (path) => (path === BASE ? registry : []), post: () => started })
    const panel = createComponentsPanel(api, 'hetzner1', 'radustanciu')
    const result = await panel.restore(entry())
    expect(result).toBe(started)
    expect(api.post).toHaveBeenCalledWith(`${BASE}/trash/job-1/restore`, {})
    expect(panel.busy.value).toBe('job-1')
    // A second action is refused while one runs.
    expect(await panel.restore(entry({ trashId: 'job-2' }))).toBeUndefined()
    expect(await panel.rebuild()).toBeUndefined()
    await panel.finish(job({ status: 'succeeded' }))
    expect(panel.busy.value).toBeUndefined()
    expect(panel.error.value).toBe('')
    expect(api.get).toHaveBeenCalledWith(BASE)
    expect(api.get).toHaveBeenCalledWith(`${BASE}/trash`)
  })

  it('does not call the server for a non-restorable entry', async () => {
    const api = fakeApi({})
    const panel = createComponentsPanel(api, 'hetzner1', 'radustanciu')
    expect(await panel.restore(entry({ restorable: false }))).toBeUndefined()
    expect(api.post).not.toHaveBeenCalled()
  })

  it('shows a refused restore and re-reads the trash', async () => {
    const api = fakeApi({
      get: () => [entry({ restorable: false, problem: 'exists' })],
      post: () => { throw new ApiClientError({ code: 'conflict', message: 'A file exists at the source path.' }) },
    })
    const panel = createComponentsPanel(api, 'hetzner1', 'radustanciu')
    expect(await panel.restore(entry())).toBeUndefined()
    expect(panel.busy.value).toBeUndefined()
    expect(panel.error.value).toBe('A file exists at the source path.')
    await vi.waitFor(() => expect(api.get).toHaveBeenCalledWith(`${BASE}/trash`))
  })

  it('runs a rebuild and keeps a failure message visible after the refresh', async () => {
    const api = fakeApi({ get: (path) => (path === BASE ? registry : []), post: () => job({ type: 'component-registry.rebuild' }) })
    const panel = createComponentsPanel(api, 'hetzner1', 'radustanciu')
    expect(await panel.rebuild()).toBeTruthy()
    expect(api.post).toHaveBeenCalledWith(`${BASE}/registry/rebuild`, {})
    expect(panel.busy.value).toBe('rebuild')
    await panel.finish(job({ type: 'component-registry.rebuild', status: 'failed', error: { code: 'process', message: 'Generator exited with code 1.' } }))
    expect(panel.error.value).toBe('Generator exited with code 1.')
    expect(panel.busy.value).toBeUndefined()
  })

  it('ignores a late source answer for a component the user left', async () => {
    let release: (value: unknown) => void = () => {}
    const api = fakeApi({ get: (path) => (path.endsWith('/phil/source') ? new Promise((resolve) => { release = resolve }) : { componentId: 'contact', path: 'app/components/content/Contact.vue', sha256: 'b', size: 3, content: 'abc', truncated: false }) })
    const panel = createComponentsPanel(api, 'hetzner1', 'radustanciu')
    const slow = panel.openSource('phil')
    await panel.openSource('contact')
    release({ componentId: 'phil', path: 'x', sha256: 'c', size: 1, content: 'x', truncated: false })
    await slow
    expect(panel.source.value?.componentId).toBe('contact')
  })
})

describe('components drawer', () => {
  const base = {
    tab: 'trash' as const,
    components: registry.components,
    loadingRegistry: false,
    loadingTrash: false,
    error: '',
    canRebuild: true,
    canDelete: true,
    canViewSource: true,
    canUseTrash: true,
  }

  it('enables restore only for restorable entries and shows the problem otherwise', async () => {
    const wrapper = mount(DevComponentsDrawer, {
      props: { ...base, trash: [entry(), entry({ trashId: 'job-2', restorable: false, problem: 'A file exists at the source path.' })] },
    })
    const ok = wrapper.get('[data-testid="restore-job-1"]')
    const blocked = wrapper.get('[data-testid="restore-job-2"]')
    expect(ok.attributes('disabled')).toBeUndefined()
    expect(blocked.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('A file exists at the source path.')
    await ok.trigger('click')
    expect(wrapper.emitted('restore')?.[0]?.[0]).toMatchObject({ trashId: 'job-1' })
  })

  it('emits rebuild, view-source and delete from the registry tab', async () => {
    const wrapper = mount(DevComponentsDrawer, { props: { ...base, tab: 'registry', registry: { ...registry, status: 'stale' }, trash: [] } })
    expect(wrapper.get('[data-testid="registry-status"]').text()).toBe('Stale')
    await wrapper.get('[data-testid="rebuild"]').trigger('click')
    expect(wrapper.emitted('rebuild')).toHaveLength(1)
    const buttons = wrapper.findAll('.dev-components__actions button')
    await buttons[0]!.trigger('click')
    await buttons[1]!.trigger('click')
    expect(wrapper.emitted('viewSource')?.[0]).toEqual(['phil'])
    expect(wrapper.emitted('delete')?.[0]).toEqual(['phil'])
  })

  it('disables rebuild while a job runs', () => {
    const wrapper = mount(DevComponentsDrawer, { props: { ...base, tab: 'registry', registry, trash: [], busy: 'rebuild' } })
    const button = wrapper.get('[data-testid="rebuild"]')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.text()).toBe('Rebuilding…')
  })
})
