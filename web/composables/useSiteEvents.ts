import { getCurrentScope, onScopeDispose, ref } from 'vue'
import type { EventSourceLike } from './useActivity'
import type { SiteEvent } from '../types/api'

export interface SiteEventStreamOptions {
  workspaceId: string
  siteId: string
  createEventSource: (url: string) => EventSourceLike
  onEvent: (event: SiteEvent) => void
  /** Called after a dropped connection, before reconnecting (refresh snapshots here). */
  onReconnect?: () => Promise<void> | void
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
  reconnectDelay?: number
}

/**
 * One SSE connection per site on `/api/v1/workspaces/{ws}/sites/{site}/events` (event name `site`).
 * It carries dev status, dev logs, registry changes and job updates, so the page needs no other
 * streams. Reconnects after 750 ms, resuming with `?after=<last seq>`.
 */
export function createSiteEventStream(options: SiteEventStreamOptions) {
  const schedule = options.schedule ?? ((callback, delay) => setTimeout(callback, delay))
  const cancelSchedule = options.cancelSchedule ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  const reconnectDelay = options.reconnectDelay ?? 750
  const connected = ref(false)
  const lastSeq = ref(0)
  let source: EventSourceLike | undefined
  let reconnectHandle: unknown
  let active = false

  function url() {
    const base = `/api/v1/workspaces/${encodeURIComponent(options.workspaceId)}/sites/${encodeURIComponent(options.siteId)}/events`
    return lastSeq.value > 0 ? `${base}?after=${lastSeq.value}` : base
  }

  function connect() {
    if (!active) return
    const current = options.createEventSource(url())
    source = current
    current.onopen = () => { if (source === current) connected.value = true }
    current.addEventListener('site', (message) => {
      if (source !== current) return
      let event: SiteEvent
      try {
        event = JSON.parse(message.data) as SiteEvent
      } catch {
        return
      }
      const seq = Number(message.lastEventId || event.seq)
      if (Number.isSafeInteger(seq) && seq > 0) {
        if (seq <= lastSeq.value) return
        lastSeq.value = seq
      }
      connected.value = true
      options.onEvent(event)
    })
    current.onerror = () => {
      if (source !== current) return
      current.close()
      source = undefined
      connected.value = false
      void Promise.resolve(options.onReconnect?.()).catch(() => undefined).finally(() => {
        if (!active || source) return
        reconnectHandle = schedule(() => {
          reconnectHandle = undefined
          connect()
        }, reconnectDelay)
      })
    }
  }

  function start() {
    if (active) return
    active = true
    connect()
  }

  function stop() {
    active = false
    connected.value = false
    if (reconnectHandle !== undefined) cancelSchedule(reconnectHandle)
    reconnectHandle = undefined
    source?.close()
    source = undefined
  }

  return { start, stop, connected, lastSeq }
}

export function useSiteEvents(options: Omit<SiteEventStreamOptions, 'createEventSource'>) {
  const stream = createSiteEventStream({ ...options, createEventSource: (url) => new EventSource(url) })
  if (getCurrentScope()) onScopeDispose(stream.stop)
  return stream
}
