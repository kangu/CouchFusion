import { describe, expect, it } from 'vitest'
import { formatSecs, startProgress } from '../composables/useDevStartProgress'
import type { ActivityEvent, DevSessionView } from '../types/api'

const t0 = Date.parse('2026-11-02T10:00:00Z')
const at = (secs: number) => new Date(t0 + secs * 1000).toISOString()
const view: DevSessionView = { siteId: 'radustanciu', unit: 'cf-dev@radustanciu.service', status: 'starting', url: '', startedAt: at(0), readyTimeoutSecs: 180 }
const log = (secs: number, text: string, id = secs): ActivityEvent => ({ id, time: at(secs), code: 'dev.log', text })

describe('dev start progress', () => {
  it('counts elapsed time against the timeout and keeps the latest lines of this start', () => {
    const logs = [log(-30, 'old run'), log(2, 'Nuxt 4.1.2'), log(10, 'Vite client built'), log(20, '')]
    const p = startProgress(view, logs, t0 + 30_000)
    expect(p.elapsedSecs).toBe(30)
    expect(p.fraction).toBeCloseTo(30 / 180)
    expect(p.lines).toEqual(['Nuxt 4.1.2', 'Vite client built'])
    expect(p.quietSecs).toBe(20)
    expect(p.stalled).toBe(false)
  })

  it('flags a start with no output for a while after the grace period', () => {
    expect(startProgress(view, [log(5, 'Nuxt')], t0 + 59_000).stalled).toBe(false)
    expect(startProgress(view, [log(5, 'Nuxt')], t0 + 70_000).stalled).toBe(true)
    expect(startProgress(view, [log(5, 'Nuxt'), log(65, 'still compiling')], t0 + 70_000).stalled).toBe(false)
    expect(startProgress(view, [], t0 + 61_000).stalled).toBe(true)
  })

  it('falls back to defaults without a start time', () => {
    const { startedAt: _s, readyTimeoutSecs: _r, ...bare } = view
    const p = startProgress(bare, [], t0)
    expect(p.elapsedSecs).toBe(0)
    expect(p.timeoutSecs).toBe(180)
  })

  it('formats seconds', () => {
    expect(formatSecs(42)).toBe('42 s')
    expect(formatSecs(65)).toBe('1:05')
  })
})
