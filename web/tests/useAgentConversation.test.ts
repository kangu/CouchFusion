import { describe, expect, it, vi } from 'vitest'
import { createAgentConversationClient } from '../composables/useAgentConversation'
import type {
  AgentConversation,
  AgentConversationCollection,
  AgentConversationEvent,
} from '../types/api'

class FakeEventSource {
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

  emit(event: AgentConversationEvent) {
    const message = new MessageEvent('conversation', {
      data: JSON.stringify(event),
      lastEventId: String(event.sequence),
    }) as MessageEvent<string>
    for (const listener of this.listeners.get('conversation') ?? []) listener(message)
  }

  fail() {
    this.onerror?.(new Event('error'))
  }
}

const readiness = { status: 'ready', available: true } as const

function conversation(sequence: number, status: AgentConversation['status'] = 'active'): AgentConversation {
  return {
    id: 'c1', workspaceId: 'w1', siteId: 'shop', title: 'Hero', status,
    registry: { state: 'not_needed' }, sequence, bindingValid: true,
    createdAt: '2026-07-14T12:00:00Z', updatedAt: '2026-07-14T12:00:00Z',
  }
}

function collection(): AgentConversationCollection {
  return { readiness, conversations: [conversation(1)] }
}

describe('agent conversation client', () => {
  it('loads the site and snapshot before opening the event stream', async () => {
    const sources: FakeEventSource[] = []
    const api = {
      get: vi.fn()
        .mockResolvedValueOnce(collection())
        .mockResolvedValueOnce(conversation(1)),
      post: vi.fn(),
    }
    const client = createAgentConversationClient({
      api,
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
    })

    await client.open('w1', 'shop', 'c1')

    expect(api.get).toHaveBeenNthCalledWith(1, '/workspaces/w1/sites/shop/agent-conversations')
    expect(api.get).toHaveBeenNthCalledWith(2, '/agent-conversations/c1')
    expect(client.conversation.value?.sequence).toBe(1)
    expect(sources[0]?.url).toBe('/api/v1/agent-conversations/c1/events?after=1')
  })

  it('refreshes on advancing events and ignores duplicate or stale sequences', async () => {
    const sources: FakeEventSource[] = []
    const api = {
      get: vi.fn()
        .mockResolvedValueOnce(collection())
        .mockResolvedValueOnce(conversation(1))
        .mockResolvedValueOnce(conversation(2)),
      post: vi.fn(),
    }
    const client = createAgentConversationClient({
      api,
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
    })
    await client.open('w1', 'shop', 'c1')

    sources[0]!.emit({ sequence: 2, type: 'item.completed', conversationId: 'c1', createdAt: '2026-07-14T12:00:01Z' })
    await vi.waitFor(() => expect(client.conversation.value?.sequence).toBe(2))
    sources[0]!.emit({ sequence: 2, type: 'item.completed', conversationId: 'c1', createdAt: '2026-07-14T12:00:01Z' })
    sources[0]!.emit({ sequence: 1, type: 'item.delta', conversationId: 'c1', createdAt: '2026-07-14T12:00:01Z' })
    await Promise.resolve()

    expect(api.get).toHaveBeenCalledTimes(3)
  })

  it('accepts a complete snapshot event without an extra fetch', async () => {
    const sources: FakeEventSource[] = []
    const api = {
      get: vi.fn().mockResolvedValueOnce(collection()).mockResolvedValueOnce(conversation(1)),
      post: vi.fn(),
    }
    const client = createAgentConversationClient({
      api,
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
    })
    await client.open('w1', 'shop', 'c1')
    const snapshot = conversation(3, 'ready')
    sources[0]!.emit({ sequence: 3, type: 'conversation.snapshot', conversationId: 'c1', snapshot, createdAt: '2026-07-14T12:00:02Z' })

    expect(client.conversation.value).toEqual(snapshot)
    expect(api.get).toHaveBeenCalledTimes(2)
  })

  it('refreshes after an error and reconnects from the fresh sequence', async () => {
    const sources: FakeEventSource[] = []
    const scheduled: Array<() => void> = []
    const api = {
      get: vi.fn()
        .mockResolvedValueOnce(collection())
        .mockResolvedValueOnce(conversation(1))
        .mockResolvedValueOnce(conversation(5)),
      post: vi.fn(),
    }
    const client = createAgentConversationClient({
      api,
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
      schedule: (callback, delay) => {
        expect(delay).toBe(750)
        scheduled.push(callback)
        return scheduled.length
      },
    })
    await client.open('w1', 'shop', 'c1')
    sources[0]!.fail()
    await vi.waitFor(() => expect(scheduled).toHaveLength(1))
    scheduled[0]!()

    expect(sources[0]!.closed).toBe(true)
    expect(sources[1]!.url).toBe('/api/v1/agent-conversations/c1/events?after=5')
  })

  it('does not reconnect after close', async () => {
    const sources: FakeEventSource[] = []
    const scheduled: Array<() => void> = []
    const api = {
      get: vi.fn()
        .mockResolvedValueOnce(collection())
        .mockResolvedValueOnce(conversation(1))
        .mockResolvedValueOnce(conversation(2)),
      post: vi.fn(),
    }
    const client = createAgentConversationClient({
      api,
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
      schedule: (callback) => {
        scheduled.push(callback)
        return scheduled.length
      },
    })
    await client.open('w1', 'shop', 'c1')
    sources[0]!.fail()
    await vi.waitFor(() => expect(scheduled).toHaveLength(1))
    client.close()
    scheduled[0]!()

    expect(sources).toHaveLength(1)
    expect(client.conversation.value).toBeUndefined()
  })

  it('adds a unique idempotency key to create and follow-up actions', async () => {
    const post = vi.fn()
      .mockResolvedValueOnce(conversation(1))
      .mockResolvedValueOnce(conversation(2))
    const api = { get: vi.fn().mockResolvedValue(collection()), post }
    const client = createAgentConversationClient({ api, createEventSource: (url) => new FakeEventSource(url) })
    await client.open('w1', 'shop')
    await client.create('Build a hero')
    await client.followUp('Use a quieter palette')

    const createHeaders = post.mock.calls[0]![2].headers
    const followUpHeaders = post.mock.calls[1]![2].headers
    expect(createHeaders['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    expect(followUpHeaders['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    expect(followUpHeaders['Idempotency-Key']).not.toBe(createHeaders['Idempotency-Key'])
  })

  it('reuses an idempotency key when the same action is retried after transport failure', async () => {
    const post = vi.fn()
      .mockRejectedValueOnce(new Error('connection reset'))
      .mockResolvedValueOnce(conversation(1))
    const api = { get: vi.fn().mockResolvedValue(collection()), post }
    const client = createAgentConversationClient({ api, createEventSource: (url) => new FakeEventSource(url) })
    await client.open('w1', 'shop')

    await expect(client.create('Build a hero')).rejects.toThrow('connection reset')
    await client.create('Build a hero')

    expect(post.mock.calls[0]![2].headers['Idempotency-Key']).toBe(post.mock.calls[1]![2].headers['Idempotency-Key'])
  })
})
