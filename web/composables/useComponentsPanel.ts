import { computed, ref } from 'vue'
import type {
  ComponentRegistryResponse,
  ComponentRegistryStatus,
  ComponentSource,
  JobSnapshot,
  TrashEntry,
} from '../types/api'

interface PanelApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
}

/** Restore is only offered for entries the server marks restorable (phase 2 contract §1). */
export function canRestore(entry: Pick<TrashEntry, 'restorable'>, busy = false) {
  return entry.restorable && !busy
}

/** Why an entry cannot be restored, for display next to the disabled button. */
export function restoreBlockedReason(entry: Pick<TrashEntry, 'restorable' | 'problem' | 'source'>) {
  if (entry.restorable) return ''
  return entry.problem || `${entry.source} cannot be restored.`
}

/** Trash entries newest first (the server should already sort; this keeps the UI stable regardless). */
export function sortTrash(entries: TrashEntry[]) {
  return [...entries].sort((a, b) => b.deletedAt.localeCompare(a.deletedAt))
}

export const registryStatusLabels: Record<ComponentRegistryStatus, string> = {
  ready: 'Ready',
  stale: 'Stale',
  missing: 'Missing',
  invalid: 'Invalid',
}

/** A rebuild is useful whenever the registry is not ready, but always allowed when the capability exists. */
export function rebuildRecommended(status: ComponentRegistryStatus | undefined) {
  return !!status && status !== 'ready'
}

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback
}

/**
 * State for the dev page's Components drawer: registry list, source viewer, trash and the
 * restore / rebuild jobs. Jobs are returned to the caller, which follows them with the job
 * tracker and reloads the builder on success.
 */
export function createComponentsPanel(api: PanelApi, workspaceId: string, siteId: string) {
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}/components`

  const registry = ref<ComponentRegistryResponse>()
  const trash = ref<TrashEntry[]>([])
  const source = ref<ComponentSource>()
  const sourceId = ref<string>()
  const loadingRegistry = ref(false)
  const loadingTrash = ref(false)
  const loadingSource = ref(false)
  /** Id of the trash entry being restored, or `'rebuild'` while a rebuild is starting/running. */
  const busy = ref<string>()
  const error = ref('')
  const sourceError = ref('')

  const components = computed(() =>
    [...(registry.value?.components ?? [])].sort((a, b) => a.label.localeCompare(b.label)))

  async function loadRegistry() {
    loadingRegistry.value = true
    try {
      registry.value = await api.get<ComponentRegistryResponse>(base)
      error.value = ''
    } catch (cause) {
      error.value = errorMessage(cause, 'Could not load the component registry.')
    } finally {
      loadingRegistry.value = false
    }
  }

  /** `keepError` keeps a message set by the action that triggered this refresh. */
  async function loadTrash(options: { keepError?: boolean } = {}) {
    loadingTrash.value = true
    try {
      trash.value = sortTrash(await api.get<TrashEntry[]>(`${base}/trash`))
      if (!options.keepError) error.value = ''
    } catch (cause) {
      error.value = errorMessage(cause, 'Could not load the trash.')
    } finally {
      loadingTrash.value = false
    }
  }

  async function openSource(componentId: string) {
    sourceId.value = componentId
    source.value = undefined
    sourceError.value = ''
    loadingSource.value = true
    try {
      const loaded = await api.get<ComponentSource>(`${base}/${encodeURIComponent(componentId)}/source`)
      // Ignore a late answer for a component the user has since navigated away from.
      if (sourceId.value === componentId) source.value = loaded
    } catch (cause) {
      if (sourceId.value === componentId) sourceError.value = errorMessage(cause, 'Could not read the component source.')
    } finally {
      if (sourceId.value === componentId) loadingSource.value = false
    }
  }

  function closeSource() {
    sourceId.value = undefined
    source.value = undefined
    sourceError.value = ''
    loadingSource.value = false
  }

  /** Starts `component.restore`; returns the job, or undefined (error set) when refused. */
  async function restore(entry: TrashEntry): Promise<JobSnapshot | undefined> {
    if (!canRestore(entry, !!busy.value)) return undefined
    busy.value = entry.trashId
    try {
      const job = await api.post<JobSnapshot>(`${base}/trash/${encodeURIComponent(entry.trashId)}/restore`, {})
      error.value = ''
      return job
    } catch (cause) {
      busy.value = undefined
      error.value = errorMessage(cause, 'Restore failed.')
      // A 409 usually means the trash state changed (file re-created): refresh to show why.
      void loadTrash({ keepError: true })
      return undefined
    }
  }

  /** Starts `component-registry.rebuild`; returns the job, or undefined (error set) when refused. */
  async function rebuild(): Promise<JobSnapshot | undefined> {
    if (busy.value) return undefined
    busy.value = 'rebuild'
    try {
      const job = await api.post<JobSnapshot>(`${base}/registry/rebuild`, {})
      error.value = ''
      return job
    } catch (cause) {
      busy.value = undefined
      error.value = errorMessage(cause, 'Registry rebuild failed.')
      return undefined
    }
  }

  /** Called by the page when the job it started (restore or rebuild) reached a terminal state. */
  async function finish(job: JobSnapshot) {
    busy.value = undefined
    await Promise.all([loadRegistry(), loadTrash()])
    // Set after the reloads, which clear `error` on success.
    if (job.status !== 'succeeded') error.value = job.error?.message ?? `${job.type} ${job.status}.`
  }

  return {
    registry, trash, source, sourceId, components,
    loadingRegistry, loadingTrash, loadingSource, busy, error, sourceError,
    loadRegistry, loadTrash, openSource, closeSource, restore, rebuild, finish,
  }
}
