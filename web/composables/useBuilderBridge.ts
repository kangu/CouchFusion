import { getCurrentScope, onScopeDispose, ref } from 'vue'

/**
 * Parent side of the control-plane ⇄ builder postMessage protocol v1
 * (docs/superpowers/specs/2026-10-29-phase1-mvp-contract.md §7).
 *
 * The child (layers/content, inside the dev-slot iframe or an opened tab) only *requests*.
 * Every destructive action is confirmed by the parent's own UI before any API call.
 */

export const BRIDGE_CAPABILITIES = ['component.delete', 'component.create', 'reload'] as const
export const BRIDGE_RATE_LIMIT = 5
export const BRIDGE_RATE_WINDOW_MS = 1000
export const BRIDGE_RELOAD_FALLBACK_MS = 3000
/** After the child acknowledged it is reloading, wait this long for its next `ready` (slow dev compiles). */
export const BRIDGE_RELOAD_ACKED_TIMEOUT_MS = 90000
export const BRIDGE_PROMPT_HINT_MAX = 2048
const COMPONENT_ID = /^[a-z][a-z0-9-]{0,127}$/

export type BridgeRequestStatus = 'accepted' | 'cancelled' | 'failed' | 'done'

export interface BridgeEnvelope {
  cf: 1
  type: string
  id: string
  nonce?: string
  replyTo?: string
  payload?: Record<string, unknown>
}

export interface BridgeMessageEvent {
  origin: string
  source: unknown
  data: unknown
}

export interface BridgeTarget {
  postMessage(message: unknown, targetOrigin: string): void
}

export interface BridgeReadyInfo {
  app: string
  route: string
  builder: boolean
  dirty: boolean
}

export interface BuilderBridgeOptions {
  /** Expected `event.origin` (the dev slot origin). Empty disables the bridge. */
  slotOrigin: () => string
  /** The child window: `iframe.contentWindow`, or a tab opened with `window.open`. */
  target: () => BridgeTarget | null | undefined
  /** Hard-reload fallback when the child does not answer (re-mount the iframe). */
  reassignFrame?: () => void
  onReady?: (info: BridgeReadyInfo) => void
  onDeleteRequest?: (componentId: string, requestId: string) => void
  onCreateRequest?: (promptHint: string, requestId: string) => void
  onReloadDeferred?: (reason: string) => void
  /** Listener host; defaults to `window`. */
  host?: Pick<Window, 'addEventListener' | 'removeEventListener'>
  now?: () => number
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
  randomId?: () => string
}

export function createBuilderBridge(options: BuilderBridgeOptions) {
  const now = options.now ?? (() => Date.now())
  const schedule = options.schedule ?? ((callback, delay) => setTimeout(callback, delay))
  const cancelSchedule = options.cancelSchedule ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  const randomId = options.randomId ?? defaultRandomId

  const connected = ref(false)
  const dirty = ref(false)
  const ready = ref<BridgeReadyInfo>()
  const reloadDeferred = ref<string>()
  const dropped = ref(0)

  let nonce = ''
  let pendingFocus: string | undefined
  let reloadFallback: unknown
  let recent: number[] = []
  let attachedHost: BuilderBridgeOptions['host'] | undefined

  function withinRateLimit() {
    const at = now()
    recent = recent.filter((time) => at - time < BRIDGE_RATE_WINDOW_MS)
    if (recent.length >= BRIDGE_RATE_LIMIT) return false
    recent.push(at)
    return true
  }

  function drop() {
    dropped.value += 1
    return false
  }

  /** Validates and dispatches one message. Returns true when it was accepted. */
  function handleMessage(event: BridgeMessageEvent): boolean {
    const expectedOrigin = options.slotOrigin()
    const target = options.target()
    if (!expectedOrigin || !target) return false
    // Other postMessage traffic (builder live updates, Vite, extensions) is ignored silently.
    const data = event.data as Partial<BridgeEnvelope> | null
    if (!data || typeof data !== 'object' || data.cf !== 1) return false
    if (typeof data.type !== 'string' || !data.type.startsWith('cf:host.')) return false
    if (event.origin !== expectedOrigin) return drop()
    if (event.source !== target) return drop()
    if (typeof data.id !== 'string' || !data.id) return drop()
    if (!withinRateLimit()) return drop()
    const payload = (data.payload && typeof data.payload === 'object' ? data.payload : {}) as Record<string, unknown>

    if (data.type === 'cf:host.ready') {
      handleReady(payload)
      return true
    }
    if (!connected.value || !nonce || data.nonce !== nonce) return drop()

    switch (data.type) {
      case 'cf:host.dirty':
        dirty.value = payload.dirty === true
        return true
      case 'cf:host.component.delete.request': {
        const componentId = payload.componentId
        if (typeof componentId !== 'string' || !COMPONENT_ID.test(componentId)) {
          reply(data.id, 'failed', { message: 'Invalid component id.' })
          return drop()
        }
        options.onDeleteRequest?.(componentId, data.id)
        return true
      }
      case 'cf:host.component.create.request': {
        const hint = typeof payload.promptHint === 'string' ? payload.promptHint.slice(0, BRIDGE_PROMPT_HINT_MAX) : ''
        options.onCreateRequest?.(hint, data.id)
        return true
      }
      case 'cf:host.reload.ack':
        if (payload.reloading === true) {
          // The child is reloading itself: do not force a second load after 3 s just because the
          // dev server compiles slowly. Keep only a long safety net in case it never comes back.
          clearReloadFallback()
          reloadFallback = schedule(() => {
            reloadFallback = undefined
            forceReload()
          }, BRIDGE_RELOAD_ACKED_TIMEOUT_MS)
        } else if (payload.reloading === false) {
          clearReloadFallback()
          const reason = typeof payload.reason === 'string' ? payload.reason : 'unsaved'
          reloadDeferred.value = reason
          options.onReloadDeferred?.(reason)
        }
        return true
      default:
        return false
    }
  }

  function handleReady(payload: Record<string, unknown>) {
    clearReloadFallback()
    reloadDeferred.value = undefined
    // One nonce per bridge session: the child retries `ready` until it sees `welcome`, so
    // repeated `ready` messages must get the same nonce. reset()/forced reloads clear it.
    if (!nonce) nonce = randomId()
    connected.value = true
    dirty.value = payload.dirty === true
    const info: BridgeReadyInfo = {
      app: typeof payload.app === 'string' ? payload.app : '',
      route: typeof payload.route === 'string' ? payload.route : '',
      builder: payload.builder === true,
      dirty: dirty.value,
    }
    ready.value = info
    post('cf:host.welcome', { nonce, capabilities: [...BRIDGE_CAPABILITIES] })
    options.onReady?.(info)
    if (pendingFocus) {
      const componentId = pendingFocus
      pendingFocus = undefined
      post('cf:host.component.focus', { componentId })
    }
  }

  function post(type: string, payload: Record<string, unknown>, replyTo?: string) {
    const target = options.target()
    const origin = options.slotOrigin()
    if (!target || !origin) return false
    const message: BridgeEnvelope = { cf: 1, type, id: randomId(), payload }
    if (nonce) message.nonce = nonce
    if (replyTo) message.replyTo = replyTo
    target.postMessage(message, origin)
    return true
  }

  function reply(requestId: string, status: BridgeRequestStatus, extra: { message?: string; componentId?: string } = {}) {
    return post('cf:host.request.result', { status, ...extra }, requestId)
  }

  function clearReloadFallback() {
    if (reloadFallback !== undefined) cancelSchedule(reloadFallback)
    reloadFallback = undefined
  }

  function forceReload() {
    clearReloadFallback()
    connected.value = false
    nonce = ''
    options.reassignFrame?.()
  }

  /**
   * Asks the child to reload. Falls back to re-mounting the frame when the child is not
   * connected, or when it neither acknowledges nor sends a new `ready` within 3 s. A
   * `reloading: true` ack extends the wait to 90 s; an unsaved-changes ack cancels the fallback
   * so the parent never discards the user's work.
   */
  function requestReload(reason: string, focusComponentId?: string) {
    if (focusComponentId) pendingFocus = focusComponentId
    reloadDeferred.value = undefined
    if (!connected.value || !post('cf:host.reload.request', focusComponentId ? { reason, focusComponentId } : { reason })) {
      forceReload()
      return
    }
    clearReloadFallback()
    reloadFallback = schedule(() => {
      reloadFallback = undefined
      forceReload()
    }, BRIDGE_RELOAD_FALLBACK_MS)
  }

  function focus(componentId: string) {
    if (connected.value) post('cf:host.component.focus', { componentId })
    else pendingFocus = componentId
  }

  const listener = (event: MessageEvent) => { handleMessage(event) }

  function attach(host = options.host ?? (typeof window !== 'undefined' ? window : undefined)) {
    if (!host || attachedHost) return
    attachedHost = host
    host.addEventListener('message', listener as EventListener)
  }

  function detach() {
    clearReloadFallback()
    attachedHost?.removeEventListener('message', listener as EventListener)
    attachedHost = undefined
    connected.value = false
    nonce = ''
  }

  /** Call when the target window goes away (iframe re-mounted, tab closed). */
  function reset() {
    connected.value = false
    nonce = ''
    ready.value = undefined
  }

  return { connected, dirty, ready, reloadDeferred, dropped, handleMessage, reply, requestReload, focus, attach, detach, reset }
}

export type BuilderBridge = ReturnType<typeof createBuilderBridge>

export function useBuilderBridge(options: BuilderBridgeOptions) {
  const bridge = createBuilderBridge(options)
  if (getCurrentScope()) onScopeDispose(bridge.detach)
  return bridge
}

function defaultRandomId() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}
