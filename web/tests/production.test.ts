import { describe, expect, it, vi } from 'vitest'
import { createProduction, shortSha } from '../composables/useProduction'
import type { JobSnapshot, ProdView } from '../types/api'

const base = '/workspaces/hydra1/sites/radustanciu/prod'

function view(overrides: Partial<ProdView> = {}): ProdView {
  return {
    siteId: 'radustanciu',
    unit: 'cf-site@radustanciu.service',
    port: 4501,
    service: { unit: 'cf-site@radustanciu.service', description: '', kind: 'prod', activeState: 'active', subState: 'running' },
    current: 'r2',
    releases: [
      { id: 'r2', appDirty: false, layersDirty: false, current: true },
      { id: 'r1', appDirty: true, layersDirty: false, current: false },
    ],
    admission: { ok: true },
    ...overrides,
  }
}

function job(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return { id: 'j1', type: 'release.deploy', status: 'running', createdAt: '', events: [], ...overrides }
}

function setup(routes: { get?: (path: string) => unknown, post?: (path: string, body: unknown) => unknown }) {
  const scheduled: Array<() => void> = []
  const api = {
    get: vi.fn().mockImplementation(async (path: string) => routes.get?.(path)),
    post: vi.fn().mockImplementation(async (path: string, body: unknown) => routes.post?.(path, body)),
  }
  const prod = createProduction(api, 'hydra1', 'radustanciu', {
    schedule: (callback) => { scheduled.push(callback); return callback },
    cancelSchedule: () => {},
  })
  return { api, prod, scheduled }
}

describe('production', () => {
  it('shortens shas', () => {
    expect(shortSha('e9d443011edf0123456789')).toBe('e9d443011edf')
    expect(shortSha(undefined)).toBe('')
  })

  it('loads the view', async () => {
    const { prod, api } = setup({ get: () => view() })
    await prod.load()
    expect(api.get).toHaveBeenCalledWith(base)
    expect(prod.view.value?.releases).toHaveLength(2)
    expect(prod.busy.value).toBe(false)
  })

  it('deploys with the typed confirmation, follows the job, then reloads', async () => {
    let status: JobSnapshot['status'] = 'running'
    const { prod, api, scheduled } = setup({
      get: (path) => path.startsWith('/jobs/') ? job({ status, events: [{ id: 1, time: '', code: 'release.build', text: 'Building…' }] }) : view(),
      post: () => job({ status: 'queued' }),
    })
    expect(await prod.deploy('radustanciu')).toBe(true)
    expect(api.post).toHaveBeenCalledWith(`${base}/deploy`, { confirm: 'radustanciu' })
    expect(prod.running.value).toBe(true)
    expect(prod.busy.value).toBe(true)

    scheduled.shift()!()
    await vi.waitFor(() => expect(prod.job.value?.events).toHaveLength(1))
    status = 'succeeded'
    scheduled.shift()!()
    await vi.waitFor(() => expect(prod.job.value?.status).toBe('succeeded'))
    await vi.waitFor(() => expect(api.get).toHaveBeenCalledWith(base))
    expect(prod.running.value).toBe(false)
  })

  it('rolls back to a release', async () => {
    const { prod, api } = setup({ post: () => job({ id: 'j2', type: 'release.rollback', status: 'queued' }) })
    await prod.rollback('r1', 'radustanciu')
    expect(api.post).toHaveBeenCalledWith(`${base}/rollback`, { releaseId: 'r1', confirm: 'radustanciu' })
    expect(prod.job.value?.type).toBe('release.rollback')
  })

  it('reports a refused request without a job', async () => {
    const { prod } = setup({ post: () => { throw new Error('Not enough resources to build') } })
    expect(await prod.deploy('radustanciu')).toBe(false)
    expect(prod.actionError.value).toContain('Not enough resources')
    expect(prod.job.value).toBeUndefined()
  })

  it('picks up a running release job, and treats other site jobs as blocking', async () => {
    const { prod } = setup({ get: (path) => path === '/jobs/j9' ? job({ id: 'j9' }) : view({ activeJobId: 'j9' }) })
    await prod.load()
    expect(prod.job.value?.id).toBe('j9')
    expect(prod.blockedByOther.value).toBe(false)

    const other = setup({ get: (path) => path === '/jobs/g1' ? job({ id: 'g1', type: 'component.generate' }) : view({ activeJobId: 'g1' }) })
    await other.prod.load()
    expect(other.prod.job.value).toBeUndefined()
    expect(other.prod.blockedByOther.value).toBe(true)
    expect(other.prod.busy.value).toBe(true)
  })
})

describe('dev vs live', () => {
  const base = { repo: 'apps/radustanciu', liveHead: 'aaa', devHead: 'aaa', ahead: 0, behind: 0, liveDirty: false, uncommitted: 0, changedFiles: [], changedFileCount: 0, changed: false }

  it('says in a few words what dev has that live does not', async () => {
    const { driftParts, driftLabel } = await import('../composables/useProduction')
    expect(driftLabel('apps/radustanciu')).toBe('App')
    expect(driftLabel('layers')).toBe('Layers')
    expect(driftParts(base)).toEqual([])
    expect(driftParts({ ...base, devHead: 'bbb', ahead: 2, changedFileCount: 1, changedFiles: ['a.vue'], uncommitted: 1, changed: true })).toEqual(['2 new commits', '1 uncommitted file'])
    expect(driftParts({ ...base, liveDirty: true, uncommitted: 3, changedFileCount: 2, changed: true })).toEqual(['2 uncommitted files edited since the build'])
    expect(driftParts({ ...base, liveDirty: true, changed: true })).toEqual(['live was built with uncommitted changes dev no longer has'])
    const { ahead: _a, behind: _b, ...unknownCounts } = base
    expect(driftParts({ ...unknownCounts, devHead: 'ccc', changed: true })).toEqual(['on ccc, live is aaa'])
    expect(driftParts({ ...base, devHead: 'ccc', ahead: 0, behind: 1, changed: true })).toEqual(["live has 1 commit dev doesn't"])
  })
})
