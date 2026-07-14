import type { ActivityEvent } from '../types/api'

export type ActivityKind = 'job' | 'process'
export type ActivityListener = (event: ActivityEvent) => void

export interface EventSourceLike {
  onerror: ((event: Event) => void) | null
  onopen: ((event: Event) => void) | null
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void
  close(): void
}

export interface ActivityHubOptions {
  createEventSource: (url: string) => EventSourceLike
  refresh: (kind: ActivityKind, id: string) => Promise<void> | void
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
  reconnectDelay?: number
}

interface ActivityConnection {
  kind: ActivityKind
  id: string
  listeners: Set<ActivityListener>
  lastEventId: number
  source: EventSourceLike | undefined
  reconnectHandle?: unknown
}

export function createActivityHub(options: ActivityHubOptions) {
  const connections = new Map<string, ActivityConnection>()
  const schedule = options.schedule ?? ((callback, delay) => setTimeout(callback, delay))
  const cancelSchedule = options.cancelSchedule ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  const reconnectDelay = options.reconnectDelay ?? 750

  function connect(connection: ActivityConnection) {
    if (connection.listeners.size === 0) return
    const collection = connection.kind === 'job' ? 'jobs' : 'processes'
    const query = connection.lastEventId > 0 ? `?after=${connection.lastEventId}` : ''
    const source = options.createEventSource(`/api/v1/${collection}/${encodeURIComponent(connection.id)}/events${query}`)
    connection.source = source
    source.addEventListener(connection.kind, (message) => {
      let event: ActivityEvent
      try {
        event = JSON.parse(message.data) as ActivityEvent
      } catch {
        return
      }
      const eventID = Number(message.lastEventId || event.id)
      if (Number.isSafeInteger(eventID) && eventID > connection.lastEventId) connection.lastEventId = eventID
      for (const listener of connection.listeners) listener(event)
    })
    source.onerror = () => {
      if (connection.source !== source) return
      source.close()
      connection.source = undefined
      void Promise.resolve(options.refresh(connection.kind, connection.id)).finally(() => {
        if (connection.listeners.size === 0 || connections.get(connectionKey(connection.kind, connection.id)) !== connection) return
        connection.reconnectHandle = schedule(() => {
          connection.reconnectHandle = undefined
          connect(connection)
        }, reconnectDelay)
      })
    }
  }

  function subscribe(kind: ActivityKind, id: string, listener: ActivityListener) {
    const key = connectionKey(kind, id)
    let connection = connections.get(key)
    if (!connection) {
      connection = { kind, id, listeners: new Set(), lastEventId: 0, source: undefined }
      connections.set(key, connection)
    }
    connection.listeners.add(listener)
    if (!connection.source && connection.reconnectHandle === undefined) connect(connection)

    let subscribed = true
    return () => {
      if (!subscribed || !connection) return
      subscribed = false
      connection.listeners.delete(listener)
      if (connection.listeners.size > 0) return
      connection.source?.close()
      connection.source = undefined
      if (connection.reconnectHandle !== undefined) cancelSchedule(connection.reconnectHandle)
      connections.delete(key)
    }
  }

  return { subscribe }
}

const activityHub = createActivityHub({
  createEventSource: (url) => new EventSource(url),
  refresh: async (kind, id) => {
    const api = useApi()
    await api.get(`/${kind === 'job' ? 'jobs' : 'processes'}/${encodeURIComponent(id)}`)
  },
})

export function useActivity() {
  return activityHub
}

function connectionKey(kind: ActivityKind, id: string) {
  return `${kind}:${id}`
}
