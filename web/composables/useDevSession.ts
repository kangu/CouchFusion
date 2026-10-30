import { computed, ref } from 'vue'
import type { DevSessionView } from '../types/api'

interface DevApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
}

export type DevAction = 'start' | 'stop' | 'restart'

/**
 * Origin guard (phase 2 contract §4): the dev slot must be reached through the same scheme
 * and host the control page was loaded through (tailnet vs SSH tunnel). If the server hands
 * out a URL on another scheme/host, keep the slot's port but move it onto the page's own
 * scheme and host, and say so. Returns the URL unchanged when it already matches, when it
 * cannot be parsed, or when there is no page origin (tests, SSR).
 */
export function alignSlotUrl(url: string, pageOrigin: string | undefined, warn: (message: string) => void = () => {}) {
  if (!url || !pageOrigin) return url
  let slot: URL
  let page: URL
  try {
    slot = new URL(url)
    page = new URL(pageOrigin)
  } catch {
    return url
  }
  if (slot.protocol === page.protocol && slot.hostname === page.hostname) return url
  const aligned = new URL(url)
  aligned.protocol = page.protocol
  aligned.hostname = page.hostname
  const result = aligned.toString().replace(/\/$/, url.endsWith('/') ? '/' : '')
  warn(`[dev-session] ignoring dev slot origin ${slot.origin}: the page was loaded through ${page.origin}; using ${aligned.origin}`)
  return result
}

/** Dev session state for one site: `GET …/dev` plus start/stop/restart (contract §2.2). */
export function createDevSession(
  api: DevApi,
  workspaceId: string,
  siteId: string,
  options: { pageOrigin?: () => string | undefined; warn?: (message: string) => void } = {},
) {
  const pageOrigin = options.pageOrigin ?? (() => (typeof window === 'undefined' ? undefined : window.location.origin))
  // eslint-disable-next-line no-console
  const warn = options.warn ?? ((message: string) => console.warn(message))
  const guard = (next: DevSessionView): DevSessionView => {
    const url = alignSlotUrl(next.url, pageOrigin(), warn)
    return url === next.url ? next : { ...next, url }
  }
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
      view.value = guard(await api.get<DevSessionView>(base))
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Dev session unavailable.'
    }
  }

  async function run(action: DevAction) {
    busy.value = action
    try {
      view.value = guard(await api.post<DevSessionView>(`${base}/${action}`, {}))
      error.value = ''
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : `Could not ${action} the dev server.`
    } finally {
      busy.value = null
    }
  }

  function apply(next: DevSessionView) {
    view.value = guard(next)
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
