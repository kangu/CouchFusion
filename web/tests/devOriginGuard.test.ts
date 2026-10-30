import { describe, expect, it, vi } from 'vitest'
import { alignSlotUrl, createDevSession } from '../composables/useDevSession'
import type { DevSessionView } from '../types/api'

const TAILNET_PAGE = 'https://hetzner1.tailb7471b.ts.net:9180'
const TUNNEL_PAGE = 'http://localhost:9180'

function view(url: string): DevSessionView {
  return { siteId: 'radustanciu', unit: 'cf-dev@radustanciu.service', status: 'ready', url }
}

describe('alignSlotUrl', () => {
  it('keeps a slot URL on the page scheme and host', () => {
    const warn = vi.fn()
    expect(alignSlotUrl('https://hetzner1.tailb7471b.ts.net:9181', TAILNET_PAGE, warn)).toBe('https://hetzner1.tailb7471b.ts.net:9181')
    expect(alignSlotUrl('http://localhost:9181', TUNNEL_PAGE, warn)).toBe('http://localhost:9181')
    expect(warn).not.toHaveBeenCalled()
  })

  it('moves a tailnet URL onto the tunnel origin, keeping the slot port', () => {
    const warn = vi.fn()
    expect(alignSlotUrl('https://hetzner1.tailb7471b.ts.net:9181', TUNNEL_PAGE, warn)).toBe('http://localhost:9181')
    expect(warn).toHaveBeenCalledOnce()
  })

  it('moves a tunnel URL onto the tailnet origin', () => {
    expect(alignSlotUrl('http://127.0.0.1:9181', TAILNET_PAGE)).toBe('https://hetzner1.tailb7471b.ts.net:9181')
  })

  it('leaves unparsable URLs and a missing page origin alone', () => {
    expect(alignSlotUrl('', TUNNEL_PAGE)).toBe('')
    expect(alignSlotUrl('not a url', TUNNEL_PAGE)).toBe('not a url')
    expect(alignSlotUrl('https://hetzner1.tailb7471b.ts.net:9181', undefined)).toBe('https://hetzner1.tailb7471b.ts.net:9181')
  })
})

describe('dev session origin guard', () => {
  it('never switches the slot origin away from the page origin on site events', async () => {
    const warn = vi.fn()
    const api = { get: vi.fn().mockResolvedValue(view('http://localhost:9181')), post: vi.fn() }
    const dev = createDevSession(api, 'hetzner1', 'radustanciu', { pageOrigin: () => TUNNEL_PAGE, warn })
    await dev.load()
    expect(dev.slotOrigin.value).toBe('http://localhost:9181')

    // A site event carrying the tailnet URL must not move a tunnel page onto the lossy path.
    dev.apply(view('https://hetzner1.tailb7471b.ts.net:9181'))
    expect(dev.slotOrigin.value).toBe('http://localhost:9181')
    expect(warn).toHaveBeenCalledOnce()
  })

  it('also guards responses of start/stop/restart', async () => {
    const api = { get: vi.fn(), post: vi.fn().mockResolvedValue(view('https://hetzner1.tailb7471b.ts.net:9181')) }
    const dev = createDevSession(api, 'hetzner1', 'radustanciu', { pageOrigin: () => TUNNEL_PAGE, warn: () => {} })
    await dev.restart()
    expect(dev.view.value?.url).toBe('http://localhost:9181')
  })
})
