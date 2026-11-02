import { describe, expect, it, vi } from 'vitest'
import { createJobTracker } from '../composables/useJobTracker'
import { createLlmRequest, modelLabel, pathFromBuilderRoute } from '../composables/useLlmRequest'
import type { JobSnapshot } from '../types/api'

const base = '/workspaces/hydra1/sites/radustanciu'

function job(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return { id: 'r1', type: 'llm.request', status: 'running', createdAt: '', events: [], ...overrides }
}

function setup(routes: { get?: (path: string) => unknown, post?: (path: string, body: unknown, options?: unknown) => unknown }) {
  const api = {
    get: vi.fn().mockImplementation(async (path: string) => routes.get?.(path)),
    post: vi.fn().mockImplementation(async (path: string, body: unknown, options?: unknown) => routes.post?.(path, body, options)),
  }
  const tracker = createJobTracker(api, { schedule: () => 0, cancelSchedule: () => {} })
  const request = createLlmRequest(api, 'hydra1', 'radustanciu', tracker)
  return { api, tracker, request }
}

const pendingResult = {
  summary: 'Swapped the services page.',
  changedFiles: [],
  operations: [{ op: 'page.copyContent', path: '/services', text: 'Replace the content of /services with /services-2026' }],
  check: { ok: true, problems: [], preview: [{ index: 0, op: 'page.copyContent', path: '/services', changes: [{ field: 'body', before: '3 blocks', after: '5 blocks' }] }] },
  applyState: 'pending',
}

describe('builder route → page path', () => {
  it('maps builder targets to site paths', () => {
    expect(pathFromBuilderRoute('/builder/')).toBe('/')
    expect(pathFromBuilderRoute('/builder')).toBe('/')
    expect(pathFromBuilderRoute('/builder/about/team?x=1')).toBe('/about/team')
    expect(pathFromBuilderRoute('/builder/caf%C3%A9')).toBe('/café')
    expect(pathFromBuilderRoute('/about')).toBeUndefined()
    expect(pathFromBuilderRoute(undefined)).toBeUndefined()
  })
})

describe('model label', () => {
  it('shows the pinned model and effort, or the Codex default', () => {
    expect(modelLabel({ model: 'gpt-5.6-sol', reasoningEffort: 'high' })).toBe('gpt-5.6-sol · high')
    expect(modelLabel({ model: 'gpt-5.6-sol', reasoningEffort: null })).toBe('gpt-5.6-sol')
    expect(modelLabel({ model: null, reasoningEffort: null })).toBe('Codex default')
    expect(modelLabel(undefined)).toBe('Codex default')
  })
})

describe('LLM request', () => {
  it('sends the prompt with the page in focus and an idempotency key', async () => {
    const { api, tracker, request } = setup({ post: () => job() })
    request.prompt.value = '  Replace /services with /services-2026 '
    request.currentPath.value = '/services'
    const pending = request.submit()
    await vi.waitFor(() => expect(request.jobId.value).toBe('r1'))
    const [path, body, options] = api.post.mock.calls[0]!
    expect(path).toBe(`${base}/requests`)
    expect(body).toEqual({ prompt: 'Replace /services with /services-2026', currentPath: '/services' })
    expect((options as { headers: Record<string, string> }).headers['Idempotency-Key']).toBeTruthy()
    tracker.update(job({ status: 'succeeded', result: pendingResult }))
    expect((await pending)?.status).toBe('succeeded')
    expect(request.result.value?.operations).toHaveLength(1)
    expect(request.canApply.value).toBe(true)
  })

  it('applies only a checked, pending proposal, once', async () => {
    const { api, tracker, request } = setup({
      post: (path) => (path.endsWith('/apply') ? job({ id: 'a1', type: 'llm.request.apply' }) : job()),
    })
    request.prompt.value = 'x'
    const sent = request.submit()
    await vi.waitFor(() => expect(request.jobId.value).toBe('r1'))
    tracker.update(job({ status: 'succeeded', result: pendingResult }))
    await sent
    const applying = request.apply()
    await vi.waitFor(() => expect(request.applyJob.value?.id).toBe('a1'))
    expect(api.post).toHaveBeenLastCalledWith(`${base}/requests/r1/apply`, {})
    expect(request.canApply.value).toBe(false)
    tracker.update(job({ id: 'a1', type: 'llm.request.apply', status: 'succeeded', result: { requestJobId: 'r1', applied: [{ index: 0, path: '/services' }] } }))
    expect((await applying)?.status).toBe('succeeded')
    expect(request.applied.value).toBe(true)
    expect(request.canApply.value).toBe(false)
    expect(await request.apply()).toBeUndefined()
  })

  it('cannot apply a proposal with problems', async () => {
    const { tracker, request } = setup({ post: () => job() })
    request.prompt.value = 'x'
    const sent = request.submit()
    await vi.waitFor(() => expect(request.jobId.value).toBe('r1'))
    tracker.update(job({ status: 'succeeded', result: { ...pendingResult, applyState: undefined, check: { ok: false, problems: [{ index: 0, message: 'Unknown component' }], preview: [] } } }))
    await sent
    expect(request.canApply.value).toBe(false)
  })

  it('reports start errors and loads pages', async () => {
    const { request } = setup({
      post: () => { throw new Error('Start the dev server first') },
      get: (path) => (path === `${base}/pages` ? { pages: [{ path: '/about', title: 'About' }] } : undefined),
    })
    request.prompt.value = 'x'
    expect(await request.submit()).toBeUndefined()
    expect(request.error.value).toContain('dev server')
    await request.loadPages()
    expect(request.pages.value?.pages[0]?.path).toBe('/about')
  })
})
