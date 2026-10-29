import { computed, ref } from 'vue'
import type { DevSessionView } from '../types/api'

interface DevApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
}

export type DevAction = 'start' | 'stop' | 'restart'

/** Dev session state for one site: `GET …/dev` plus start/stop/restart (contract §2.2). */
export function createDevSession(api: DevApi, workspaceId: string, siteId: string) {
  const view = ref<DevSessionView>()
  const busy = ref<DevAction | null>(null)
  const error = ref('')
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}/dev`

  const status = computed(() => view.value?.status ?? 'stopped')
  const ready = computed(() => status.value === 'ready')
  /** Origin of the dev slot as seen by this browser (e.g. https://…ts.net:9181). */
  const slotOrigin = computed(() => originOf(view.value?.url))

  async function load() {
    try {
      view.value = await api.get<DevSessionView>(base)
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Dev session unavailable.'
    }
  }

  async function run(action: DevAction) {
    busy.value = action
    try {
      view.value = await api.post<DevSessionView>(`${base}/${action}`, {})
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : `Could not ${action} the dev server.`
    } finally {
      busy.value = null
    }
  }

  function apply(next: DevSessionView) {
    view.value = next
  }

  return {
    view, busy, error, status, ready, slotOrigin, load, apply,
    start: () => run('start'),
    stop: () => run('stop'),
    restart: () => run('restart'),
  }
}

export function originOf(url: string | undefined) {
  if (!url) return ''
  try {
    return new URL(url).origin
  } catch {
    return ''
  }
}
