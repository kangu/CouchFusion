import { computed, ref } from 'vue'
import type { GitRemoteOp, JobSnapshot, RepoDiff, RepoStatus, RepoSummary } from '../types/api'

interface GitApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
}

export interface CommitRequest {
  message: string
  paths: string[]
  expectedHead: string
}

/**
 * Repo ids are relative paths that the server may send already URL-encoded
 * (`apps%2Fradustanciu`). Normalise to exactly one level of encoding for path segments.
 */
export function repoSegment(id: string) {
  let raw = id
  try { raw = decodeURIComponent(id) } catch { /* keep as is */ }
  return encodeURIComponent(raw)
}

export interface RemoteActions {
  canFetch: boolean
  canPull: boolean
  canPush: boolean
  /** Why push is unavailable (shown next to the button). */
  pushBlocked?: string
  /** Short tracking line, e.g. "origin/main · 2 ahead · 0 behind". */
  summary: string
}

/** Which remote buttons a repo status allows (pure, for tests). */
export function remoteActions(status: RepoStatus | undefined): RemoteActions {
  if (!status || !status.hasRemote || !status.pushBranches) {
    return { canFetch: false, canPull: false, canPush: false, pushBlocked: 'No remote configured.', summary: 'No remote' }
  }
  const configured = status.pushBranches.length > 0
  const counts = status.ahead !== undefined && status.behind !== undefined ? ` · ${status.ahead} ahead · ${status.behind} behind` : ''
  const summary = status.upstream ? `${status.upstream}${counts}` : 'Not tracking a remote branch yet'
  let pushBlocked: string | undefined
  if (!configured) pushBlocked = 'This repo has no remote in server.toml.'
  else if (!status.pushBranches.includes(status.branch)) pushBlocked = `Only ${status.pushBranches.join(', ')} may be pushed.`
  else if ((status.behind ?? 0) > 0) pushBlocked = 'Pull first: the remote has newer commits.'
  else if (status.upstream && status.ahead === 0) pushBlocked = 'Nothing to push.'
  return {
    canFetch: configured,
    canPull: configured && !!status.upstream && (status.behind ?? 0) > 0,
    canPush: configured && pushBlocked === undefined,
    ...(pushBlocked ? { pushBlocked } : {}),
    summary,
  }
}

/** Per-repo git status, diff, commit, fetch, pull and push for one workspace. */
export function createGitRepos(api: GitApi, workspaceId: string) {
  const repos = ref<RepoSummary[]>([])
  const status = ref<RepoStatus>()
  const diff = ref<RepoDiff>()
  const error = ref('')
  const loading = ref(false)
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/repos`

  const dirtyCount = computed(() => repos.value.reduce((total, repo) => total + repo.dirty, 0))

  async function load() {
    loading.value = true
    try {
      repos.value = await api.get<RepoSummary[]>(base)
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Repositories unavailable.'
    } finally {
      loading.value = false
    }
  }

  async function loadStatus(repoId: string) {
    diff.value = undefined
    try {
      status.value = await api.get<RepoStatus>(`${base}/${repoSegment(repoId)}/status`)
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Status unavailable.'
    }
  }

  async function loadDiff(repoId: string, path: string) {
    try {
      diff.value = await api.get<RepoDiff>(`${base}/${repoSegment(repoId)}/diff?path=${encodeURIComponent(path)}`)
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Diff unavailable.'
    }
  }

  function commit(repoId: string, request: CommitRequest) {
    return api.post<JobSnapshot>(`${base}/${repoSegment(repoId)}/commit`, request)
  }

  /** Starts a fetch, fast-forward pull or push job (push needs the branch typed as `confirm`). */
  function remote(repoId: string, op: GitRemoteOp, body: { branch?: string; confirm?: string } = {}) {
    return api.post<JobSnapshot>(`${base}/${repoSegment(repoId)}/${op}`, body)
  }

  return { repos, status, diff, error, loading, dirtyCount, load, loadStatus, loadDiff, commit, remote }
}
