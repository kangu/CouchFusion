import { describe, expect, it, vi } from 'vitest'
import { createApiClient } from '../composables/useApi'

describe('useApi', () => {
  it('uses authenticated relative API requests', async () => {
    const fetcher = vi.fn().mockResolvedValue({ data: { id: 'w1' } })
    const api = createApiClient(fetcher)

    await expect(api.post<{ id: string }>('/workspaces/register', { root: '/workspace' })).resolves.toEqual({ id: 'w1' })
    expect(fetcher).toHaveBeenCalledWith('/api/v1/workspaces/register', {
      method: 'POST',
      credentials: 'same-origin',
      body: { root: '/workspace' },
    })
  })

  it('forwards explicit same-origin request headers', async () => {
    const fetcher = vi.fn().mockResolvedValue({ data: { id: 'c1' } })
    const api = createApiClient(fetcher)

    await api.post('/agent-conversations', { prompt: 'Build a hero' }, {
      headers: { 'Idempotency-Key': 'generation-1' },
    })
    expect(fetcher).toHaveBeenCalledWith('/api/v1/agent-conversations', {
      method: 'POST',
      credentials: 'same-origin',
      body: { prompt: 'Build a hero' },
      headers: { 'Idempotency-Key': 'generation-1' },
    })
  })

  it('maps field errors from the API', async () => {
    const fetcher = vi.fn().mockRejectedValue({
      data: { error: { code: 'validation', message: 'The request is invalid', fields: { root: 'Workspace is invalid' } } },
    })
    const api = createApiClient(fetcher)

    await expect(api.post('/workspaces/register', { root: '/missing' })).rejects.toMatchObject({
      code: 'validation',
      fields: { root: 'Workspace is invalid' },
    })
  })

  it('normalizes unknown transport failures', async () => {
    const fetcher = vi.fn().mockRejectedValue(new Error('socket closed'))
    const api = createApiClient(fetcher)

    await expect(api.get('/workspaces')).rejects.toMatchObject({
      code: 'unavailable',
      message: 'Could not reach the CouchFusion server.',
    })
  })

  it('accepts successful no-content mutations', async () => {
    const fetcher = vi.fn().mockResolvedValue(undefined)
    const api = createApiClient(fetcher)

    await expect(api.delete('/workspaces/w1')).resolves.toBeUndefined()
  })
})
