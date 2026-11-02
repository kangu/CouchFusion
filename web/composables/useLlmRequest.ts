import { computed, ref } from 'vue'
import type { AgentInfo, JobSnapshot, LlmApplyResult, LlmRequestResult, SitePageList } from '../types/api'
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
  let idempotencyKey: string | undefined
  let keyFor = ''

  const job = computed<JobSnapshot | undefined>(() => (jobId.value ? tracker.jobs.value[jobId.value] : undefined))
  const applyJob = computed<JobSnapshot | undefined>(() => (applyJobId.value ? tracker.jobs.value[applyJobId.value] : undefined))
  const result = computed(() => (job.value?.status === 'succeeded' ? job.value.result as unknown as LlmRequestResult : undefined))
  const applyResult = computed(() => applyJob.value?.result as unknown as LlmApplyResult | undefined)
  const running = computed(() => (!!job.value && !isTerminalJob(job.value)) || (!!applyJob.value && !isTerminalJob(applyJob.value)))
  const applied = computed(() => applyJob.value?.status === 'succeeded')
  const canApply = computed(() => result.value?.applyState === 'pending' && !!result.value.check?.ok && !applied.value && !running.value && !busy.value)
  const canSubmit = computed(() => prompt.value.trim().length > 0 && !running.value && !busy.value)

  async function loadPages() {
    try {
      pages.value = await api.get<SitePageList>(`${base}/pages`)
      pagesError.value = ''
    } catch (cause) {
      pagesError.value = cause instanceof Error ? cause.message : 'Could not load the pages.'
    }
  }

  /** Starts the request; resolves with the terminal job (or undefined if it could not start). */
  async function submit(): Promise<JobSnapshot | undefined> {
    if (!canSubmit.value) return undefined
    const text = prompt.value.trim()
    const path = currentPath.value.trim() || undefined
    // Reuse the key while the same request is retried, so the server de-duplicates.
    const signature = `${text}\n${path ?? ''}`
    if (!idempotencyKey || keyFor !== signature) {
      idempotencyKey = crypto.randomUUID()
      keyFor = signature
    }
    busy.value = true
    error.value = ''
    applyJobId.value = undefined
    try {
      const started = await api.post<JobSnapshot>(`${base}/requests`, { prompt: text, currentPath: path }, { headers: { 'Idempotency-Key': idempotencyKey } })
      jobId.value = started.id
      busy.value = false
      const done = await tracker.track(started)
      if (done.status === 'succeeded') idempotencyKey = undefined
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
      return await tracker.track(started)
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not apply the page changes.'
      return undefined
    } finally {
      busy.value = false
    }
  }

  /** Clears the run from this view; nothing is applied. */
  function discard() {
    jobId.value = undefined
    applyJobId.value = undefined
    error.value = ''
  }

  return { prompt, currentPath, pages, pagesError, error, busy, jobId, job, applyJob, result, applyResult, running, applied, canApply, canSubmit, loadPages, submit, apply, discard }
}
