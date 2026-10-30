import { computed, ref } from 'vue'
import type { ActivityEvent, ServiceSnapshot } from '../types/api'

interface ServicesApi {
  get<T>(path: string): Promise<T>
}

export const SERVICES_REFRESH_MS = 10_000
export const DEFAULT_LOG_LINES = 200

/** Visual state for a unit: `ok` (active/running), `busy` (transitioning), `down` (failed/inactive). */
export function serviceTone(service: Pick<ServiceSnapshot, 'activeState'>): 'ok' | 'busy' | 'down' {
  if (service.activeState === 'active') return 'ok'
  if (['activating', 'deactivating', 'reloading', 'refreshing'].includes(service.activeState)) return 'busy'
  return 'down'
}

export function formatBytes(bytes: number | undefined) {
  if (!bytes) return ''
  if (bytes >= 1073741824) return `${(bytes / 1073741824).toFixed(1)} GiB`
  return `${Math.round(bytes / 1048576)} MiB`
}

/**
 * Read-only systemd service list (phase 2 contract §1/§4): `GET /services` refreshed every
 * 10 s while started, plus recent logs for one selected unit. No start/stop (phase 3).
 */
export function createServices(api: ServicesApi, options: {
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
  interval?: number
} = {}) {
  const schedule = options.schedule ?? ((callback, delay) => setTimeout(callback, delay))
  const cancelSchedule = options.cancelSchedule ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  const interval = options.interval ?? SERVICES_REFRESH_MS

  const services = ref<ServiceSnapshot[]>([])
  const error = ref('')
  const loading = ref(false)
  const lastLoaded = ref<Date>()
  const selected = ref<string>()
  const logs = ref<ActivityEvent[]>([])
  const logsLoading = ref(false)
  const logsError = ref('')
  let timer: unknown
  let running = false
  let inFlight: Promise<void> | undefined

  const failing = computed(() => services.value.filter((service) => serviceTone(service) === 'down').length)

  function load(): Promise<void> {
    // Never stack requests on a slow link: a refresh tick while one is pending is skipped.
    if (inFlight) return inFlight
    loading.value = true
    inFlight = api.get<ServiceSnapshot[]>('/services')
      .then((list) => {
        services.value = list
        lastLoaded.value = new Date()
        error.value = ''
      })
      .catch((cause: unknown) => {
        error.value = cause instanceof Error ? cause.message : 'Could not load services.'
      })
      .finally(() => {
        loading.value = false
        inFlight = undefined
      })
    return inFlight
  }

  function tick() {
    timer = undefined
    if (!running) return
    void load().finally(() => {
      if (running) timer = schedule(tick, interval)
    })
  }

  function start() {
    if (running) return
    running = true
    tick()
  }

  function stop() {
    running = false
    if (timer !== undefined) cancelSchedule(timer)
    timer = undefined
  }

  async function loadLogs(unit = selected.value, lines = DEFAULT_LOG_LINES) {
    if (!unit) return
    selected.value = unit
    logsLoading.value = true
    try {
      const next = await api.get<ActivityEvent[]>(`/services/${encodeURIComponent(unit)}/logs?lines=${lines}`)
      if (selected.value === unit) {
        logs.value = next
        logsError.value = ''
      }
    } catch (cause) {
      if (selected.value === unit) logsError.value = cause instanceof Error ? cause.message : 'Could not load logs.'
    } finally {
      if (selected.value === unit) logsLoading.value = false
    }
  }

  function closeLogs() {
    selected.value = undefined
    logs.value = []
    logsError.value = ''
  }

  return {
    services, error, loading, lastLoaded, failing, selected, logs, logsLoading, logsError,
    load, start, stop, loadLogs, closeLogs,
  }
}
