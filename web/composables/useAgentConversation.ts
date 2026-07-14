import { getCurrentScope, onScopeDispose, readonly, ref } from 'vue'
import type { Ref } from 'vue'
import type { ApiRequestOptions } from './useApi'
import type { EventSourceLike } from './useActivity'
import type {
  AgentApprovalDecision,
  AgentConsentKind,
  AgentConversation,
  AgentConversationCollection,
  AgentConversationEvent,
  AgentReadiness,
} from '../types/api'

interface AgentApi {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown, options?: ApiRequestOptions): Promise<T>
}

interface AgentConversationClientOptions {
  api: AgentApi
  createEventSource: (url: string) => EventSourceLike
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
}

interface MutableAgentState {
  readiness: Ref<AgentReadiness | undefined>
  conversations: Ref<AgentConversation[]>
  conversation: Ref<AgentConversation | undefined>
  busy: Ref<boolean>
  error: Ref<string | undefined>
}

export function createAgentConversationClient(options: AgentConversationClientOptions) {
  const state: MutableAgentState = {
    readiness: ref(),
    conversations: ref([]),
    conversation: ref(),
    busy: ref(false),
    error: ref(),
  }
  const schedule = options.schedule ?? ((callback, delay) => setTimeout(callback, delay))
  const cancelSchedule = options.cancelSchedule ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  let workspaceId = ''
  let siteId = ''
  let source: EventSourceLike | undefined
  let reconnectHandle: unknown
  let generation = 0
  let lastSequence = 0
  const pendingIdempotencyKeys = new Map<string, string>()

  async function open(nextWorkspaceId: string, nextSiteId: string, conversationId?: string) {
    close()
    const currentGeneration = generation
    workspaceId = nextWorkspaceId
    siteId = nextSiteId
    state.busy.value = true
    state.error.value = undefined
    try {
      const collection = await options.api.get<AgentConversationCollection>(siteCollectionPath())
      if (currentGeneration !== generation) return
      state.readiness.value = collection.readiness
      state.conversations.value = collection.conversations
      if (conversationId) {
        const snapshot = await options.api.get<AgentConversation>(conversationPath(conversationId))
        if (currentGeneration !== generation) return
        activate(snapshot, currentGeneration)
      }
    } catch (error) {
      if (currentGeneration === generation) state.error.value = errorMessage(error)
    } finally {
      if (currentGeneration === generation) state.busy.value = false
    }
  }

  function close() {
    generation++
    source?.close()
    source = undefined
    if (reconnectHandle !== undefined) cancelSchedule(reconnectHandle)
    reconnectHandle = undefined
    workspaceId = ''
    siteId = ''
    lastSequence = 0
    state.conversation.value = undefined
    state.busy.value = false
    pendingIdempotencyKeys.clear()
  }

  async function create(prompt: string) {
    if (!workspaceId || !siteId) throw new Error('Open a site before creating a conversation.')
    return runMutation(async () => {
      const action = `create:${workspaceId}:${siteId}:${prompt}`
      const snapshot = await idempotentMutation(action, key => options.api.post<AgentConversation>(siteCollectionPath(), { prompt }, idempotencyOptions(key)))
      activate(snapshot, generation)
      state.conversations.value = replaceConversation(state.conversations.value, snapshot)
      return snapshot
    })
  }

  async function followUp(prompt: string) {
    const current = requireConversation()
    return runMutation(async () => {
      const action = `turn:${current.id}:${prompt}`
      const snapshot = await idempotentMutation(action, key => options.api.post<AgentConversation>(`${conversationPath(current.id)}/turns`, { prompt }, idempotencyOptions(key)))
      activate(snapshot, generation)
      state.conversations.value = replaceConversation(state.conversations.value, snapshot)
      return snapshot
    })
  }

  async function answer(requestId: string, answers: Record<string, string>) {
    const current = requireConversation()
    await runMutation(() => options.api.post<void>(`${conversationPath(current.id)}/questions/${encodeURIComponent(requestId)}/answer`, { answers }))
    await refresh(current.id, generation)
  }

  async function approve(requestId: string, decision: AgentApprovalDecision) {
    const current = requireConversation()
    await runMutation(() => options.api.post<void>(`${conversationPath(current.id)}/approvals/${encodeURIComponent(requestId)}`, { decision }))
    await refresh(current.id, generation)
  }

  async function consent(kind: AgentConsentKind, granted: boolean) {
    const current = requireConversation()
    await runMutation(() => options.api.post<void>(`${conversationPath(current.id)}/consents`, { kind, granted }))
    await refresh(current.id, generation)
  }

  async function interrupt() {
    const current = requireConversation()
    await runMutation(() => options.api.post<void>(`${conversationPath(current.id)}/interrupt`, {}))
    await refresh(current.id, generation)
  }

  function activate(snapshot: AgentConversation, expectedGeneration: number) {
    if (expectedGeneration !== generation) return
    source?.close()
    source = undefined
    if (reconnectHandle !== undefined) cancelSchedule(reconnectHandle)
    reconnectHandle = undefined
    state.conversation.value = snapshot
    lastSequence = snapshot.sequence
    connect(snapshot.id, expectedGeneration)
  }

  function connect(conversationId: string, expectedGeneration: number) {
    if (expectedGeneration !== generation || state.conversation.value?.id !== conversationId) return
    const query = lastSequence > 0 ? `?after=${lastSequence}` : ''
    const nextSource = options.createEventSource(`/api/v1${conversationPath(conversationId)}/events${query}`)
    source = nextSource
    nextSource.addEventListener('conversation', (message) => {
      if (source !== nextSource || expectedGeneration !== generation) return
      let event: AgentConversationEvent
      try {
        event = JSON.parse(message.data) as AgentConversationEvent
      } catch {
        return
      }
      const sequence = Number(message.lastEventId || event.sequence)
      if (!Number.isSafeInteger(sequence) || sequence <= lastSequence) return
      lastSequence = sequence
      if (event.type === 'conversation.snapshot' && event.snapshot) {
        state.conversation.value = event.snapshot
        state.conversations.value = replaceConversation(state.conversations.value, event.snapshot)
        return
      }
      void refresh(conversationId, expectedGeneration)
    })
    nextSource.onerror = () => {
      if (source !== nextSource || expectedGeneration !== generation) return
      nextSource.close()
      source = undefined
      void refresh(conversationId, expectedGeneration).finally(() => {
        if (expectedGeneration !== generation || state.conversation.value?.id !== conversationId) return
        reconnectHandle = schedule(() => {
          reconnectHandle = undefined
          connect(conversationId, expectedGeneration)
        }, 750)
      })
    }
  }

  async function refresh(conversationId: string, expectedGeneration: number) {
    try {
      const snapshot = await options.api.get<AgentConversation>(conversationPath(conversationId))
      if (expectedGeneration !== generation || state.conversation.value?.id !== conversationId) return
      if (snapshot.sequence < lastSequence) return
      state.conversation.value = snapshot
      state.conversations.value = replaceConversation(state.conversations.value, snapshot)
      lastSequence = Math.max(lastSequence, snapshot.sequence)
      state.error.value = undefined
    } catch (error) {
      if (expectedGeneration === generation) state.error.value = errorMessage(error)
    }
  }

  async function runMutation<T>(operation: () => Promise<T>) {
    state.busy.value = true
    state.error.value = undefined
    try {
      return await operation()
    } catch (error) {
      state.error.value = errorMessage(error)
      throw error
    } finally {
      state.busy.value = false
    }
  }

  async function idempotentMutation<T>(action: string, operation: (key: string) => Promise<T>) {
    const key = pendingIdempotencyKeys.get(action) ?? globalThis.crypto.randomUUID()
    pendingIdempotencyKeys.set(action, key)
    const result = await operation(key)
    pendingIdempotencyKeys.delete(action)
    return result
  }

  function requireConversation() {
    if (!state.conversation.value) throw new Error('Open a conversation before performing this action.')
    return state.conversation.value
  }

  function siteCollectionPath() {
    return `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}/agent-conversations`
  }

  return {
    readiness: readonly(state.readiness),
    conversations: readonly(state.conversations),
    conversation: readonly(state.conversation),
    busy: readonly(state.busy),
    error: readonly(state.error),
    open,
    close,
    create,
    followUp,
    answer,
    approve,
    consent,
    interrupt,
  }
}

export function useAgentConversation() {
  const client = createAgentConversationClient({
    api: useApi(),
    createEventSource: (url) => new EventSource(url),
  })
  if (getCurrentScope()) onScopeDispose(client.close)
  return client
}

function conversationPath(conversationId: string) {
  return `/agent-conversations/${encodeURIComponent(conversationId)}`
}

function idempotencyOptions(key: string): ApiRequestOptions {
  return { headers: { 'Idempotency-Key': key } }
}

function replaceConversation(conversations: AgentConversation[], snapshot: AgentConversation) {
  const remaining = conversations.filter(conversation => conversation.id !== snapshot.id)
  return [snapshot, ...remaining]
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'The agent conversation could not be updated.'
}
