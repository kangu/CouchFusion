import { describe, expect, it, vi } from 'vitest'
import type { EventSourceLike } from '../composables/useActivity'
import { createSiteEventStream } from '../composables/useSiteEvents'

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
  close() { this.closed = true }
  emit(type: string, id: string, data: unknown) {
    const event = new MessageEvent(type, { data: JSON.stringify(data), lastEventId: id }) as MessageEvent<string>
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }
  fail() { this.onerror?.(new Event('error')) }
}

function setup() {
  const sources: FakeEventSource[] = []
  const scheduled: Array<{ callback: () => void; delay: number }> = []
  const onEvent = vi.fn()
  const onReconnect = vi.fn()
  const stream = createSiteEventStream({
    workspaceId: 'hetzner1',
    siteId: 'radustanciu',
    createEventSource: (url) => { const source = new FakeEventSource(url); sources.push(source); return source },
    onEvent,
    onReconnect,
    schedule: (callback, delay) => { scheduled.push({ callback, delay }); return scheduled.length },
    cancelSchedule: vi.fn(),
  })
  return { stream, sources, scheduled, onEvent, onReconnect }
}

describe('site event stream', () => {
  it('opens one stream on the site events URL and dispatches `site` events', () => {
    const { stream, sources, onEvent } = setup()
    stream.start()
    stream.start()
    expect(sources).toHaveLength(1)
    expect(sources[0]!.url).toBe('/api/v1/workspaces/hetzner1/sites/radustanciu/events')
    sources[0]!.emit('site', '3', { seq: 3, type: 'dev.status', time: '', dev: { status: 'ready' } })
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ type: 'dev.status' }))
    expect(stream.lastSeq.value).toBe(3)
  })

  it('drops duplicate or older sequence numbers', () => {
    const { stream, sources, onEvent } = setup()
    stream.start()
    sources[0]!.emit('site', '5', { seq: 5, type: 'log', time: '' })
    sources[0]!.emit('site', '5', { seq: 5, type: 'log', time: '' })
    sources[0]!.emit('site', '4', { seq: 4, type: 'log', time: '' })
    expect(onEvent).toHaveBeenCalledTimes(1)
  })

  it('refreshes, then reconnects after 750 ms resuming from the last sequence', async () => {
    const { stream, sources, scheduled, onReconnect } = setup()
    stream.start()
    sources[0]!.emit('site', '12', { seq: 12, type: 'log', time: '' })
    sources[0]!.fail()
    expect(sources[0]!.closed).toBe(true)
    expect(stream.connected.value).toBe(false)
    await vi.waitFor(() => expect(scheduled).toHaveLength(1))
    expect(onReconnect).toHaveBeenCalledOnce()
    expect(scheduled[0]!.delay).toBe(750)
    scheduled[0]!.callback()
    expect(sources).toHaveLength(2)
    expect(sources[1]!.url).toBe('/api/v1/workspaces/hetzner1/sites/radustanciu/events?after=12')
  })

  it('ignores events from a replaced connection and does not reconnect after stop', async () => {
    const { stream, sources, scheduled, onEvent } = setup()
    stream.start()
    const first = sources[0]!
    first.fail()
    await vi.waitFor(() => expect(scheduled).toHaveLength(1))
    stream.stop()
    first.emit('site', '1', { seq: 1, type: 'log', time: '' })
    expect(onEvent).not.toHaveBeenCalled()
    scheduled[0]!.callback()
    expect(sources).toHaveLength(1)
  })
})
