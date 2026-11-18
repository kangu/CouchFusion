import { describe, expect, it, vi } from 'vitest'
import { createJobTracker } from '../composables/useJobTracker'
import { mount } from '@vue/test-utils'
import { createLlmRequest, formatWhen, historyOutcome, historyStatus, modelLabel, pathFromBuilderRoute } from '../composables/useLlmRequest'
import DevRequestDrawer from '../components/dev/DevRequestDrawer.vue'
import type { JobSnapshot, LlmRequestSummary } from '../types/api'

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

  it('continues the selected task with its page and sends screenshot bytes only for the new turn', async () => {
    const previous = job({ status: 'succeeded', result: { ...pendingResult, prompt: 'Review the page', currentPath: '/services' } })
    const { api, request } = setup({
      get: path => path.includes('/jobs/') ? previous : { requests: [] },
      post: () => job({ id: 'next', status: 'succeeded', result: pendingResult }),
    })
    await request.open('r1')
    request.newTask({ followUp: true })
    request.prompt.value = 'Use this layout'
    await request.submit({ data: 'data:image/png;base64,AQID', key: 'image-key' })
    expect(api.post.mock.calls[0]?.[1]).toEqual({
      parentRequestId: 'r1', currentPath: '/services', prompt: 'Use this layout', screenshot: 'data:image/png;base64,AQID',
    })
    request.newTask({ reuse: true })
    await request.submit()
    expect(api.post.mock.calls[1]?.[1]).toEqual({ prompt: 'Use this layout' })
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

function row(overrides: Partial<LlmRequestSummary> = {}): LlmRequestSummary {
  return { id: 'r1', status: 'succeeded', createdAt: '2026-11-03T10:00:00Z', prompt: 'Swap services', currentPath: '/services', changedFiles: 0, operations: 1, proposal: 'pending', applyJob: null, ...overrides }
}

describe('request history', () => {
  it('describes outcomes in a few words', () => {
    expect(historyOutcome(row())).toBe('1 page change to review')
    expect(historyOutcome(row({ changedFiles: 2, operations: 3, proposal: 'applied' }))).toBe('2 files changed · 3 page changes applied')
    expect(historyOutcome(row({ operations: 0 }))).toBe('Nothing changed')
    expect(historyOutcome(row({ status: 'failed', operations: 0, error: 'Codex exited' }))).toBe('Codex exited')
    expect(historyStatus('running')).toBe('Running')
    expect(historyStatus('succeeded')).toBe('Done')
    expect(formatWhen('nope')).toBe('')
    expect(formatWhen('2026-11-03T10:00:00Z')).not.toBe('')
  })

  it('lists the history after a request finishes and starts a new task', async () => {
    let history = [row({ status: 'running', proposal: 'none' })]
    const { api, tracker, request } = setup({ post: () => job(), get: (path) => (path === `${base}/requests` ? { requests: history } : undefined) })
    request.prompt.value = 'Swap services'
    request.currentPath.value = '/services'
    const sent = request.submit()
    await vi.waitFor(() => expect(request.jobId.value).toBe('r1'))
    expect(request.submittedPrompt.value).toBe('Swap services')
    history = [row()]
    tracker.update(job({ status: 'succeeded', result: { ...pendingResult, currentPath: '/services' } }))
    await sent
    await vi.waitFor(() => expect(request.history.value?.[0]?.status).toBe('succeeded'))
    expect(api.get).toHaveBeenCalledWith(`${base}/requests`)
    expect(request.finished.value).toBe(true)
    expect(request.canApply.value).toBe(true)

    request.newTask({ reuse: true })
    expect(request.jobId.value).toBeUndefined()
    expect(request.prompt.value).toBe('Swap services')
    expect(request.currentPath.value).toBe('/services')

    await request.open('r1')
    request.newTask({ path: '/about' })
    expect(request.prompt.value).toBe('')
    expect(request.currentPath.value).toBe('/about')
  })

  it('opens a finished request with its apply job and trusts the server about the proposal', async () => {
    const { request } = setup({
      get: (path) => {
        if (path === `${base}/requests`) return { requests: [row({ proposal: 'applied', applyJob: { id: 'a1', status: 'succeeded' } }), row({ id: 'r0', proposal: 'pending' })] }
        if (path === '/jobs/r1') return job({ status: 'succeeded', result: { ...pendingResult, prompt: 'Swap services' } })
        if (path === '/jobs/a1') return job({ id: 'a1', type: 'llm.request.apply', status: 'succeeded', result: { requestJobId: 'r1', applied: [] } })
        return undefined
      },
    })
    await request.loadHistory()
    expect(await request.open('r1')).toBeUndefined()
    expect(request.jobId.value).toBe('r1')
    expect(request.applyJob.value?.id).toBe('a1')
    expect(request.submittedPrompt.value).toBe('Swap services')
    expect(request.proposal.value).toBe('applied')
    expect(request.canApply.value).toBe(false)
    expect(request.view.value).toBe('task')
  })

  it('resumes a request that is still running on the server', async () => {
    const { tracker, request } = setup({
      get: (path) => {
        if (path === `${base}/requests`) return { requests: [row({ status: 'running', proposal: 'none', operations: 0 })] }
        if (path === '/jobs/r1') return job({ status: 'running', result: { prompt: 'Swap services' } })
        return undefined
      },
    })
    const resumed = request.resume()
    await vi.waitFor(() => expect(request.jobId.value).toBe('r1'))
    expect(request.running.value).toBe(true)
    request.newTask()
    expect(request.jobId.value).toBe('r1')
    tracker.update(job({ status: 'succeeded', result: { ...pendingResult, changedFiles: ['app/a.vue'] } }))
    expect((await resumed)?.status).toBe('succeeded')
    expect(request.finished.value).toBe(true)
  })
})

describe('request drawer', () => {
  const props = { prompt: '', currentPath: '', pagesError: '', model: 'gpt', view: 'task' as const, submittedPrompt: '', proposal: 'none' as const, historyError: '', canSubmit: false, canApply: false, canStartNew: true, running: false, busy: false, error: '' }

  it('previews a dropped screenshot, submits it, and lets the author remove it', async () => {
    const wrapper = mount(DevRequestDrawer, { props: { ...props, prompt: 'Review layout', canSubmit: true } })
    const file = new File(['pixels'], 'layout.png', { type: 'image/png' })
    const form = wrapper.get('form')
    await form.trigger('drop', { dataTransfer: { files: [file] } })
    await vi.waitFor(() => expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,cGl4ZWxz'))
    await form.trigger('submit')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toMatchObject({ data: 'data:image/png;base64,cGl4ZWxz' })
    await wrapper.findAll('button').find(b => b.text() === 'Remove')!.trigger('click')
    expect(wrapper.find('img').exists()).toBe(false)
    await form.trigger('submit')
    expect(wrapper.emitted('submit')?.[1]).toEqual([undefined])
  })

  it('shows the sent prompt and a new-task button once a task is finished', async () => {
    const wrapper = mount(DevRequestDrawer, { props: { ...props, submittedPrompt: 'Swap services', job: job({ status: 'succeeded', events: [] }), result: { ...pendingResult, applyState: 'pending' } as never, proposal: 'pending' } })
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.get('[data-testid="request-asked"]').text()).toContain('Swap services')
    await wrapper.get('[data-testid="request-new-task"]').trigger('click')
    await wrapper.get('[data-testid="request-reuse"]').trigger('click')
    expect(wrapper.emitted('newTask')).toEqual([[false], [true]])
  })

  it('lists history rows and opens one', async () => {
    const wrapper = mount(DevRequestDrawer, { props: { ...props, view: 'history', history: [row(), row({ id: 'r2', prompt: null, status: 'failed', error: 'Boom', operations: 0 })] } })
    const items = wrapper.findAll('[data-testid="request-history"] button')
    expect(items).toHaveLength(2)
    expect(items[0]!.text()).toContain('Swap services')
    expect(items[0]!.text()).toContain('1 page change to review')
    expect(items[1]!.text()).toContain('Prompt not recorded')
    await items[1]!.trigger('click')
    expect(wrapper.emitted('open')).toEqual([['r2']])
    expect(wrapper.get('[data-testid="request-tab-history"]').text()).toBe('History (2)')
  })
})
