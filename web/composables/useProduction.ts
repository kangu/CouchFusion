import { computed, ref } from 'vue'
import type { JobSnapshot, ProdView, DevDrift } from '../types/api'
import { createJobTracker, isTerminalJob } from './useJobTracker'

interface ProductionApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
}

/** Short git sha for display (`abcdef123456`). */
export function shortSha(sha: string | undefined) {
  return sha ? sha.slice(0, 12) : ''
}

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** `App`, `Layers`, or the repo path. */
export function driftLabel(repo: string) {
  if (repo.startsWith('apps/')) return 'App'
  return repo === 'layers' ? 'Layers' : repo
}

/** What dev has that the live release does not, in a few words per repo. */
export function driftParts(drift: DevDrift): string[] {
  const parts: string[] = []
  if (drift.ahead) parts.push(`${count(drift.ahead, 'new commit', 'new commits')}`)
  if (drift.behind) parts.push(`live has ${count(drift.behind, 'commit', 'commits')} dev doesn't`)
  if (drift.ahead === undefined && drift.liveHead !== drift.devHead) parts.push(`on ${shortSha(drift.devHead) || '?'}, live is ${shortSha(drift.liveHead) || 'unknown'}`)
  if (drift.changedFileCount) parts.push(`${count(drift.changedFileCount, 'uncommitted file', 'uncommitted files')}${drift.liveDirty ? ' edited since the build' : ''}`)
  if (drift.liveDirty && !drift.uncommitted && drift.liveHead === drift.devHead) parts.push('live was built with uncommitted changes dev no longer has')
  return parts
}

/**
 * Production releases of one site (Phase 3): `GET …/prod`, deploy and rollback jobs.
 * Both jobs need the site id typed as `confirm`. The job is followed by polling its snapshot
 * (events included), then the view is reloaded. A job already holding the site lease when the
 * page opens (`activeJobId`) is picked up and followed too.
 */
export function createProduction(api: ProductionApi, workspaceId: string, siteId: string, options: {
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
  pollInterval?: number
} = {}) {
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}/prod`
  const tracker = createJobTracker(api, options)
  const view = ref<ProdView>()
  const error = ref('')
  const actionError = ref('')
  const loading = ref(false)
  const jobId = ref<string>()

  const job = computed<JobSnapshot | undefined>(() => (jobId.value ? tracker.jobs.value[jobId.value] : undefined))
  const running = computed(() => !!job.value && !isTerminalJob(job.value))
  /** Another site job (component generate, git, …) holds the lease. */
  const blockedByOther = computed(() => !!view.value?.activeJobId && view.value.activeJobId !== jobId.value)
  const busy = computed(() => running.value || blockedByOther.value)

  async function load() {
    loading.value = true
    try {
      view.value = await api.get<ProdView>(base)
      error.value = ''
      const active = view.value.activeJobId
      if (active && active !== jobId.value) {
        const snapshot = await api.get<JobSnapshot>(`/jobs/${encodeURIComponent(active)}`).catch(() => undefined)
        if (snapshot && snapshot.type.startsWith('release.')) follow(snapshot)
      }
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not load production.'
    } finally {
      loading.value = false
    }
  }

  function follow(snapshot: JobSnapshot) {
    jobId.value = snapshot.id
    void tracker.track(snapshot).then(() => load())
  }

  async function start(path: string, body: Record<string, string>) {
    actionError.value = ''
    try {
      follow(await api.post<JobSnapshot>(`${base}/${path}`, body))
      return true
    } catch (cause) {
      actionError.value = cause instanceof Error ? cause.message : 'The request failed.'
      return false
    }
  }

  const deploy = (confirm: string) => start('deploy', { confirm })
  const rollback = (releaseId: string, confirm: string) => start('rollback', { releaseId, confirm })

  function stop() {
    tracker.stop()
  }

  return { view, error, actionError, loading, job, running, busy, blockedByOther, load, deploy, rollback, stop }
}
