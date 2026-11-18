import { computed, ref } from 'vue'
import type { AgentInfo, JobSnapshot, LlmApplyResult, LlmProposalState, LlmRequestHistory, LlmRequestResult, LlmRequestSummary, SitePageList } from '../types/api'
import { createJobTracker, isTerminalJob } from './useJobTracker'

interface RequestApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown, options?: { headers?: Record<string, string> }): Promise<T>
}

type Tracker = ReturnType<typeof createJobTracker>

/** `/builder/about/team?x=1` → `/about/team`; `/builder/` → `/`; anything else → undefined. */
export function pathFromBuilderRoute(route: string | undefined): string | undefined {
  if (!route) return undefined
  const pathname = route.split(/[?#]/)[0] ?? ''
  const match = /^\/builder(?:\/(.*))?$/.exec(pathname)
  if (!match) return undefined
  const rest = (match[1] ?? '').split('/').filter(Boolean).map((segment) => {
    try { return decodeURIComponent(segment) } catch { return segment }
  })
  return `/${rest.join('/')}`
}

/** `gpt-5.6-sol · high`, or `Codex default` when the server does not pin a model. */
export function modelLabel(agent: AgentInfo | undefined): string {
  if (!agent?.model) return agent?.reasoningEffort ? `Codex default · ${agent.reasoningEffort}` : 'Codex default'
  return agent.reasoningEffort ? `${agent.model} · ${agent.reasoningEffort}` : agent.model
}

/** `Done`, `Running`, … for a history row. */
export function historyStatus(status: LlmRequestSummary['status']): string {
  if (status === 'succeeded') return 'Done'
  if (status === 'failed') return 'Failed'
  if (status === 'cancelled') return 'Cancelled'
  return 'Running'
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** What came of a request, in a few words: files, page changes and where those stand. */
export function historyOutcome(item: LlmRequestSummary): string {
  const parts: string[] = []
  if (item.changedFiles) parts.push(`${plural(item.changedFiles, 'file', 'files')} changed`)
  if (item.operations) {
    const changes = plural(item.operations, 'page change', 'page changes')
    const state = { pending: 'to review', applied: 'applied', partial: 'partly applied', none: 'not applicable' }[item.proposal]
    parts.push(`${changes} ${state}`)
  }
  if (!parts.length && item.status === 'succeeded') parts.push('Nothing changed')
  if (item.error && item.status !== 'succeeded') parts.push(item.error)
  return parts.join(' · ')
}

/** `Nov 3, 11:02` in the viewer's locale; empty for a missing or bad date. */
export function formatWhen(iso: string | null | undefined): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(date)
}

/**
 * LLM Request on the dev page (contract 2026-11-02): a generic prompt that may change app files
 * and proposes page operations. Page operations are applied only by `apply()`, after review.
 */
export function createLlmRequest(api: RequestApi, workspaceId: string, siteId: string, tracker: Tracker) {
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}`
  const prompt = ref('')
  const currentPath = ref('')
  const pages = ref<SitePageList>()
  const pagesError = ref('')
  const error = ref('')
  const busy = ref(false)
  const jobId = ref<string>()
  const applyJobId = ref<string>()
  /** The prompt of the task on screen (the form keeps `prompt` for the next one). */
  const submittedPrompt = ref('')
  const history = ref<LlmRequestSummary[]>()
  const historyError = ref('')
  const view = ref<'task' | 'history'>('task')
  let idempotencyKey: string | undefined
  const parentRequestId = ref<string>()
  let keyFor = ''

  const job = computed<JobSnapshot | undefined>(() => (jobId.value ? tracker.jobs.value[jobId.value] : undefined))
  const applyJob = computed<JobSnapshot | undefined>(() => (applyJobId.value ? tracker.jobs.value[applyJobId.value] : undefined))
  const result = computed(() => (job.value?.status === 'succeeded' ? job.value.result as unknown as LlmRequestResult : undefined))
  const applyResult = computed(() => applyJob.value?.result as unknown as LlmApplyResult | undefined)
  const running = computed(() => (!!job.value && !isTerminalJob(job.value)) || (!!applyJob.value && !isTerminalJob(applyJob.value)))
  const applied = computed(() => applyJob.value?.status === 'succeeded')
  /** The history row of the task on screen, once the server lists it as finished. */
  const summary = computed(() => history.value?.find((item) => item.id === jobId.value && isTerminalJob(item)))
  const proposal = computed<LlmProposalState>(() => {
    if (applied.value) return 'applied'
    return summary.value?.proposal ?? (result.value?.applyState === 'pending' ? 'pending' : 'none')
  })
  const canApply = computed(() => proposal.value === 'pending' && !!result.value?.check?.ok && !applied.value && !running.value && !busy.value)
  const finished = computed(() => !!job.value && !running.value)
  const canStartNew = computed(() => !running.value && !busy.value)
  const canSubmit = computed(() => prompt.value.trim().length > 0 && !running.value && !busy.value)

  async function loadPages() {
    try {
      pages.value = await api.get<SitePageList>(`${base}/pages`)
      pagesError.value = ''
    } catch (cause) {
      pagesError.value = cause instanceof Error ? cause.message : 'Could not load the pages.'
    }
  }

  async function loadHistory(): Promise<LlmRequestSummary[] | undefined> {
    try {
      history.value = (await api.get<LlmRequestHistory>(`${base}/requests`)).requests
      historyError.value = ''
    } catch (cause) {
      historyError.value = cause instanceof Error ? cause.message : 'Could not load the request history.'
    }
    return history.value
  }

  /**
   * Shows a request from the history. Resolves with the terminal job when it was still running
   * (the caller then reacts as after `submit()`), otherwise with undefined.
   */
  async function open(id: string): Promise<JobSnapshot | undefined> {
    if (running.value && id !== jobId.value) return undefined
    const item = history.value?.find((entry) => entry.id === id)
    error.value = ''
    try {
      const snapshot = await api.get<JobSnapshot>(`/jobs/${encodeURIComponent(id)}`)
      tracker.update(snapshot)
      if (item?.applyJob) tracker.update(await api.get<JobSnapshot>(`/jobs/${encodeURIComponent(item.applyJob.id)}`))
      parentRequestId.value = undefined
      jobId.value = id
      applyJobId.value = item?.applyJob?.id
      submittedPrompt.value = item?.prompt ?? (typeof snapshot.result?.prompt === 'string' ? snapshot.result.prompt : '')
      view.value = 'task'
      if (isTerminalJob(snapshot)) return undefined
      const done = await tracker.wait(id)
      void loadHistory()
      return done
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not open the request.'
      return undefined
    }
  }

  /** On opening the drawer: shows a request that is still running on the server, if any. */
  async function resume(): Promise<JobSnapshot | undefined> {
    const list = await loadHistory()
    if (jobId.value) return undefined
    const live = list?.find((item) => !isTerminalJob(item))
    return live ? open(live.id) : undefined
  }

  /**
   * Clears the finished task for a new one. `reuse` keeps its prompt and page in the form;
   * otherwise the form starts empty on `path` (the page open in the builder).
   */
  function newTask(options: { reuse?: boolean; followUp?: boolean; path?: string } = {}) {
    if (!canStartNew.value) return
    const previousPath = result.value?.currentPath ?? summary.value?.currentPath ?? ''
    parentRequestId.value = options.followUp ? jobId.value : undefined
    prompt.value = options.reuse ? submittedPrompt.value : ''
    currentPath.value = options.reuse || options.followUp ? previousPath : (options.path ?? '')
    jobId.value = undefined
    applyJobId.value = undefined
    submittedPrompt.value = ''
    error.value = ''
    view.value = 'task'
  }

  /** Starts the request; resolves with the terminal job (or undefined if it could not start). */
  async function submit(screenshot?: { data: string; key: string }): Promise<JobSnapshot | undefined> {
    if (!canSubmit.value) return undefined
    const text = prompt.value.trim()
    const path = currentPath.value.trim() || undefined
    // Reuse the key while the same request is retried, so the server de-duplicates.
    const signature = JSON.stringify([text, path, parentRequestId.value, screenshot?.key])
    if (!idempotencyKey || keyFor !== signature) {
      idempotencyKey = crypto.randomUUID()
      keyFor = signature
    }
    busy.value = true
    error.value = ''
    applyJobId.value = undefined
    try {
      const started = await api.post<JobSnapshot>(`${base}/requests`, { prompt: text, currentPath: path, parentRequestId: parentRequestId.value, screenshot: screenshot?.data }, { headers: { 'Idempotency-Key': idempotencyKey } })
      screenshot = undefined
      parentRequestId.value = undefined
      jobId.value = started.id
      submittedPrompt.value = text
      busy.value = false
      void loadHistory()
      const done = await tracker.track(started)
      if (done.status === 'succeeded') idempotencyKey = undefined
      void loadHistory()
      return done
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not start the request.'
      return undefined
    } finally {
      busy.value = false
    }
  }

  async function apply(): Promise<JobSnapshot | undefined> {
    if (!canApply.value || !jobId.value) return undefined
    busy.value = true
    error.value = ''
    try {
      const started = await api.post<JobSnapshot>(`${base}/requests/${encodeURIComponent(jobId.value)}/apply`, {})
      applyJobId.value = started.id
      busy.value = false
      const done = await tracker.track(started)
      void loadHistory()
      return done
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not apply the page changes.'
      return undefined
    } finally {
      busy.value = false
    }
  }

  return {
    prompt, currentPath, pages, pagesError, error, busy, jobId, job, applyJob, result, applyResult, running, applied, canApply, canSubmit,
    parentRequestId, submittedPrompt, history, historyError, view, proposal, finished, canStartNew,
    loadPages, loadHistory, submit, apply, open, resume, newTask,
  }
}
