import { computed, ref } from 'vue'
import type { JobSnapshot, RepoDiff, RepoStatus, RepoSummary } from '../types/api'

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

/** Per-repo git status, diff and commit for one workspace (contract §2.2; no push in Phase 1). */
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

  return { repos, status, diff, error, loading, dirtyCount, load, loadStatus, loadDiff, commit }
}
