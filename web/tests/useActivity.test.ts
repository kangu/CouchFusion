import { describe, expect, it, vi } from 'vitest'
import { createActivityHub, type EventSourceLike } from '../composables/useActivity'

class FakeEventSource implements EventSourceLike {
  readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  closed = false
  onerror: ((event: Event) => void) | null = null
  onopen: ((event: Event) => void) | null = null

  constructor(readonly url: string) {}

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    const listeners = this.listeners.get(type) ?? new Set()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  }

  close() {
    this.closed = true
  }

  emit(type: string, id: string, data: unknown) {
    const event = new MessageEvent(type, { data: JSON.stringify(data), lastEventId: id }) as MessageEvent<string>
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }

  fail() {
    this.onerror?.(new Event('error'))
  }
}

describe('activity hub', () => {
  it('shares one SSE connection per resource and tears it down after the last subscriber', () => {
    const sources: FakeEventSource[] = []
    const hub = createActivityHub({
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
      refresh: vi.fn(),
    })
    const first = vi.fn()
    const second = vi.fn()

    const stopFirst = hub.subscribe('job', 'job-1', first)
    const stopSecond = hub.subscribe('job', 'job-1', second)
    expect(sources).toHaveLength(1)
    sources[0]!.emit('job', '4', { id: 4, text: 'working' })
    expect(first).toHaveBeenCalledOnce()
    expect(second).toHaveBeenCalledOnce()

    stopFirst()
    expect(sources[0]!.closed).toBe(false)
    stopSecond()
    expect(sources[0]!.closed).toBe(true)
  })

  it('refreshes the snapshot and reconnects from the last event ID', async () => {
    const sources: FakeEventSource[] = []
    const scheduled: Array<() => void> = []
    const refresh = vi.fn().mockResolvedValue(undefined)
    const hub = createActivityHub({
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
      refresh,
      schedule: (callback) => {
        scheduled.push(callback)
        return 1
      },
      cancelSchedule: vi.fn(),
    })

    const stop = hub.subscribe('process', 'process-1', vi.fn())
    sources[0]!.emit('process', '8', { id: 8, text: 'ready' })
    sources[0]!.fail()
    await Promise.resolve()
    expect(refresh).toHaveBeenCalledWith('process', 'process-1')
    expect(scheduled).toHaveLength(1)

    scheduled[0]!()
    expect(sources[1]!.url).toBe('/api/v1/processes/process-1/events?after=8')
    stop()
  })
})
