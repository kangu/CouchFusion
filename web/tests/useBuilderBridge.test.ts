import { describe, expect, it, vi } from 'vitest'
import {
  BRIDGE_RELOAD_FALLBACK_MS,
  createBuilderBridge,
  type BridgeEnvelope,
  type BuilderBridgeOptions,
} from '../composables/useBuilderBridge'

const ORIGIN = 'https://hetzner1.tailb7471b.ts.net:9181'

class FakeChild {
  readonly sent: Array<{ message: BridgeEnvelope; origin: string }> = []
  postMessage(message: unknown, origin: string) {
    this.sent.push({ message: message as BridgeEnvelope, origin })
  }
  last(type: string) {
    return [...this.sent].reverse().find((entry) => entry.message.type === type)
  }
}

function setup(overrides: Partial<BuilderBridgeOptions> = {}) {
  const child = new FakeChild()
  let clock = 0
  const timers: Array<{ callback: () => void; delay: number; cancelled: boolean }> = []
  let ids = 0
  const options: BuilderBridgeOptions = {
    slotOrigin: () => ORIGIN,
    target: () => child,
    now: () => clock,
    randomId: () => `id-${++ids}`,
    schedule: (callback, delay) => {
      const timer = { callback, delay, cancelled: false }
      timers.push(timer)
      return timer
    },
    cancelSchedule: (handle) => { (handle as { cancelled: boolean }).cancelled = true },
    ...overrides,
  }
  const bridge = createBuilderBridge(options)
  const send = (type: string, payload: Record<string, unknown> = {}, extra: Partial<BridgeEnvelope> & { origin?: string; source?: unknown } = {}) => {
    const { origin = ORIGIN, source = child, ...envelope } = extra
    return bridge.handleMessage({ origin, source, data: { cf: 1, type, id: `child-${++ids}`, payload, ...envelope } })
  }
  const handshake = (payload: Record<string, unknown> = {}) => {
    send('cf:host.ready', { app: 'radustanciu', route: '/builder/', builder: true, dirty: false, ...payload })
    return child.last('cf:host.welcome')!.message.payload!.nonce as string
  }
  const advance = (ms: number) => { clock += ms }
  const fire = () => {
    for (const timer of timers.splice(0)) if (!timer.cancelled) timer.callback()
  }
  return { bridge, child, send, handshake, advance, fire, timers }
}

describe('builder bridge (parent)', () => {
  it('answers ready with a welcome carrying a fresh nonce and the capabilities', () => {
    const onReady = vi.fn()
    const { bridge, child, handshake } = setup({ onReady })
    const nonce = handshake({ dirty: true })
    const welcome = child.last('cf:host.welcome')!
    expect(welcome.origin).toBe(ORIGIN)
    expect(nonce).toBeTruthy()
    expect(welcome.message.payload!.capabilities).toEqual(['component.delete', 'component.create', 'reload'])
    expect(bridge.connected.value).toBe(true)
    expect(bridge.dirty.value).toBe(true)
    expect(onReady).toHaveBeenCalledWith({ app: 'radustanciu', route: '/builder/', builder: true, dirty: true })
  })

  it('answers repeated ready messages (child retries) with the same nonce', () => {
    const { bridge, handshake } = setup()
    const first = handshake()
    expect(handshake()).toBe(first)
    bridge.reset()
    expect(handshake()).not.toBe(first)
  })

  it('rejects messages from the wrong origin or the wrong window', () => {
    const { bridge, send, child } = setup()
    expect(send('cf:host.ready', {}, { origin: 'https://evil.example' })).toBe(false)
    expect(send('cf:host.ready', {}, { source: {} })).toBe(false)
    expect(bridge.connected.value).toBe(false)
    expect(child.sent).toHaveLength(0)
    expect(bridge.dropped.value).toBe(2)
  })

  it('ignores non-bridge traffic such as the builder live-update messages', () => {
    const { bridge } = setup()
    expect(bridge.handleMessage({ origin: ORIGIN, source: undefined, data: { type: 'content:update' } })).toBe(false)
    expect(bridge.dropped.value).toBe(0)
  })

  it('requires the welcome nonce on every request after the handshake', () => {
    const onDeleteRequest = vi.fn()
    const { send, handshake } = setup({ onDeleteRequest })
    // Before the handshake there is no nonce to match.
    expect(send('cf:host.component.delete.request', { componentId: 'network-hero' })).toBe(false)
    const nonce = handshake()
    expect(send('cf:host.component.delete.request', { componentId: 'network-hero' }, { nonce: 'forged' })).toBe(false)
    expect(onDeleteRequest).not.toHaveBeenCalled()
    expect(send('cf:host.component.delete.request', { componentId: 'network-hero' }, { nonce })).toBe(true)
    expect(onDeleteRequest).toHaveBeenCalledWith('network-hero', expect.any(String))
  })

  it('refuses malformed component ids and tells the child', () => {
    const onDeleteRequest = vi.fn()
    const { send, handshake, child } = setup({ onDeleteRequest })
    const nonce = handshake()
    expect(send('cf:host.component.delete.request', { componentId: '../../etc/passwd' }, { nonce })).toBe(false)
    expect(onDeleteRequest).not.toHaveBeenCalled()
    expect(child.last('cf:host.request.result')!.message.payload).toMatchObject({ status: 'failed' })
  })

  it('caps create prompt hints at 2 KB', () => {
    const onCreateRequest = vi.fn()
    const { send, handshake } = setup({ onCreateRequest })
    const nonce = handshake()
    send('cf:host.component.create.request', { promptHint: 'x'.repeat(5000) }, { nonce })
    expect(onCreateRequest.mock.calls[0]![0]).toHaveLength(2048)
  })

  it('rate limits to 5 messages per second', () => {
    const onDeleteRequest = vi.fn()
    const { send, handshake, advance } = setup({ onDeleteRequest })
    const nonce = handshake() // 1st message in this window
    for (let i = 0; i < 6; i += 1) send('cf:host.component.delete.request', { componentId: 'a' }, { nonce })
    expect(onDeleteRequest).toHaveBeenCalledTimes(4)
    advance(1001)
    send('cf:host.component.delete.request', { componentId: 'a' }, { nonce })
    expect(onDeleteRequest).toHaveBeenCalledTimes(5)
  })

  it('replies to a request with replyTo and the nonce', () => {
    const { bridge, handshake, child } = setup()
    const nonce = handshake()
    bridge.reply('req-1', 'done', { componentId: 'network-hero' })
    const result = child.last('cf:host.request.result')!.message
    expect(result).toMatchObject({ cf: 1, nonce, replyTo: 'req-1', payload: { status: 'done', componentId: 'network-hero' } })
  })

  it('reassigns the frame when no ready arrives within 3 s of a reload request', () => {
    const reassignFrame = vi.fn()
    const { bridge, handshake, child, timers, fire } = setup({ reassignFrame })
    handshake()
    bridge.requestReload('component.deleted')
    expect(child.last('cf:host.reload.request')!.message.payload).toEqual({ reason: 'component.deleted' })
    expect(timers.at(-1)!.delay).toBe(BRIDGE_RELOAD_FALLBACK_MS)
    fire()
    expect(reassignFrame).toHaveBeenCalledOnce()
    expect(bridge.connected.value).toBe(false)
  })

  it('cancels the fallback when the child comes back ready, then sends the pending focus', () => {
    const reassignFrame = vi.fn()
    const { bridge, handshake, child, fire } = setup({ reassignFrame })
    handshake()
    bridge.requestReload('component.generated', 'pricing-table')
    handshake()
    fire()
    expect(reassignFrame).not.toHaveBeenCalled()
    expect(child.last('cf:host.component.focus')!.message.payload).toEqual({ componentId: 'pricing-table' })
  })

  it('never force-reloads over unsaved builder changes', () => {
    const reassignFrame = vi.fn()
    const onReloadDeferred = vi.fn()
    const { bridge, handshake, send, fire } = setup({ reassignFrame, onReloadDeferred })
    const nonce = handshake()
    bridge.requestReload('component.deleted')
    send('cf:host.reload.ack', { reloading: false, reason: 'unsaved' }, { nonce })
    fire()
    expect(reassignFrame).not.toHaveBeenCalled()
    expect(bridge.reloadDeferred.value).toBe('unsaved')
    expect(onReloadDeferred).toHaveBeenCalledWith('unsaved')
  })

  it('reloads immediately by re-mounting the frame when the child never connected', () => {
    const reassignFrame = vi.fn()
    const { bridge, child } = setup({ reassignFrame })
    bridge.requestReload('manual', 'hero')
    expect(reassignFrame).toHaveBeenCalledOnce()
    expect(child.sent).toHaveLength(0)
  })
})
