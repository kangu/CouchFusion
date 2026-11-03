import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import AgentHealthCard from '../components/system/AgentHealthCard.vue'
import SystemPage from '../pages/system.vue'
import type { AgentReadiness } from '../types/api'

describe('agent health card', () => {
  it.each([
    [{ status: 'ready', available: true, version: '0.144.4', accountType: 'chatgpt' }, '0.144.4'],
    [{ status: 'missing', available: false }, 'codex --version'],
    [{ status: 'launch_failed', available: false, problem: 'Could not launch app-server' }, 'codex --version'],
    [{ status: 'unsupported', available: false, version: '0.100.0' }, 'supported Codex'],
    [{ status: 'unauthenticated', available: false }, 'codex login'],
    [{ status: 'restarting', available: false }, 'restarting'],
  ] as Array<[AgentReadiness, string]>)('renders safe read-only checks for %s', (readiness, expected) => {
    const wrapper = mount(AgentHealthCard, { props: { readiness } })
    expect(wrapper.text()).toContain(expected)
    expect(wrapper.text()).toContain('Codex binary')
    expect(wrapper.text()).toContain('App server')
    expect(wrapper.text()).toContain('Local account')
    expect(wrapper.text()).toContain('couchfusion-control')
    expect(wrapper.text()).toContain('Component skill')
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('Install Codex')
    expect(wrapper.text()).not.toContain('credential')
    expect(wrapper.text()).not.toContain('stderr')
  })

  it('is mounted by the system page beside read-only dependency checks', async () => {
    const available = { available: true, version: 'fixture' }
    vi.stubGlobal('useApi', () => ({
      get: vi.fn().mockResolvedValue({
        git: available, bun: available, node: available, couchdb: available,
        agent: { status: 'ready', available: true, version: '0.144.4', accountType: 'chatgpt' },
      }),
      post: vi.fn(),
    }))
    vi.stubGlobal('useWorkspace', () => ({
      ready: ref(true), activeWorkspaceId: ref('w1'), validate: vi.fn(),
    }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn() }))
    vi.stubGlobal('useBootstrap', () => ({ isRust: ref(false) }))

    const wrapper = mount(SystemPage)
    await flushPromises()

    expect(wrapper.findAll('.health-card')).toHaveLength(5)
    expect(wrapper.get('.agent-health-card').text()).toContain('Codex generation')
  })

  it('offers no repairs on the Rust control plane', async () => {
    const down = { available: false, problem: 'not installed' }
    vi.stubGlobal('useApi', () => ({
      get: vi.fn().mockResolvedValue({
        git: { available: true, version: 'git version 2.47' }, bun: down, node: down, couchdb: down,
        agent: { status: 'ready', available: true, version: 'codex-cli 0.144.4', accountType: 'model gpt-5.6-sol' },
      }),
      post: vi.fn(),
    }))
    vi.stubGlobal('useWorkspace', () => ({ ready: ref(true), activeWorkspaceId: ref('hydra1'), validate: vi.fn() }))
    vi.stubGlobal('useActivityStore', () => ({ trackJob: vi.fn() }))
    vi.stubGlobal('useBootstrap', () => ({ isRust: ref(true) }))

    const wrapper = mount(SystemPage)
    await flushPromises()

    expect(wrapper.text()).toContain('not installed')
    expect(wrapper.text()).not.toContain('Prepare repair')
  })
})
