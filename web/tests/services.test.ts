import { describe, expect, it, vi } from 'vitest'
import { createServices, formatBytes, SERVICES_REFRESH_MS, serviceTone } from '../composables/useServices'
import type { ServiceSnapshot } from '../types/api'

function service(overrides: Partial<ServiceSnapshot> = {}): ServiceSnapshot {
  return { unit: 'kangu.service', description: 'kangu', kind: 'adopted', activeState: 'active', subState: 'running', ...overrides }
}

function setup(get: (path: string) => unknown) {
  const scheduled: Array<{ callback: () => void; delay: number; cancelled?: boolean }> = []
  const api = { get: vi.fn().mockImplementation(async (path: string) => get(path)) }
  const state = createServices(api, {
    schedule: (callback, delay) => { const item = { callback, delay }; scheduled.push(item); return item },
    cancelSchedule: (handle) => { (handle as { cancelled?: boolean }).cancelled = true },
  })
  return { api, state, scheduled }
}

describe('service helpers', () => {
  it('maps systemd states to tones', () => {
    expect(serviceTone(service())).toBe('ok')
    expect(serviceTone(service({ activeState: 'activating' }))).toBe('busy')
    expect(serviceTone(service({ activeState: 'failed' }))).toBe('down')
    expect(serviceTone(service({ activeState: 'inactive' }))).toBe('down')
    expect(serviceTone(service({ unit: 'cf-build@radustanciu.service', activeState: 'inactive' }))).toBe('ok')
    expect(serviceTone(service({ unit: 'cf-build@radustanciu.service', activeState: 'failed' }))).toBe('down')
  })

  it('formats memory', () => {
    expect(formatBytes(undefined)).toBe('')
    expect(formatBytes(512 * 1048576)).toBe('512 MiB')
    expect(formatBytes(2 * 1073741824)).toBe('2.0 GiB')
  })
})

describe('services refresh', () => {
  it('loads immediately, then every 10 s until stopped', async () => {
    const { api, state, scheduled } = setup(() => [service(), service({ unit: 'caddy.service', activeState: 'failed' })])
    state.start()
    await vi.waitFor(() => expect(scheduled).toHaveLength(1))
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(scheduled[0]!.delay).toBe(SERVICES_REFRESH_MS)
    expect(state.services.value).toHaveLength(2)
    expect(state.failing.value).toBe(1)

    scheduled[0]!.callback()
    await vi.waitFor(() => expect(scheduled).toHaveLength(2))
    expect(api.get).toHaveBeenCalledTimes(2)

    state.stop()
    expect(scheduled[1]!.cancelled).toBe(true)
    scheduled[1]!.callback() // a tick that fires after stop does nothing
    expect(api.get).toHaveBeenCalledTimes(2)
  })

  it('keeps the last list and reports the error when a refresh fails', async () => {
    let fail = false
    const { state } = setup(() => { if (fail) throw new Error('unreachable'); return [service()] })
    await state.load()
    fail = true
    await state.load()
    expect(state.services.value).toHaveLength(1)
    expect(state.error.value).toBe('unreachable')
  })

  it('does not stack requests while one is pending', async () => {
    let release: (value: unknown) => void = () => {}
    const { api, state } = setup(() => new Promise((resolve) => { release = resolve }))
    const first = state.load()
    const second = state.load()
    expect(api.get).toHaveBeenCalledTimes(1)
    release([service()])
    await Promise.all([first, second])
    expect(state.services.value).toHaveLength(1)
  })

  it('loads logs for the selected unit and drops a late answer for another unit', async () => {
    let releaseKangu: (value: unknown) => void = () => {}
    const { api, state } = setup((path) => path.startsWith('/services/kangu.service/logs')
      ? new Promise((resolve) => { releaseKangu = resolve })
      : [{ id: 1, time: '2026-10-30T07:00:00Z', code: 'log', text: 'caddy line' }])
    const slow = state.loadLogs('kangu.service')
    await state.loadLogs('caddy.service')
    releaseKangu([{ id: 9, time: '2026-10-30T07:00:00Z', code: 'log', text: 'kangu line' }])
    await slow
    expect(api.get).toHaveBeenCalledWith('/services/caddy.service/logs?lines=200')
    expect(state.selected.value).toBe('caddy.service')
    expect(state.logs.value.map((line) => line.text)).toEqual(['caddy line'])
  })
})
