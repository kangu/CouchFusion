import { describe, expect, it } from 'vitest'
import { builderURL, deriveOnboardingMilestone, shouldAutoOpenOnboarding, validatedDevURL } from '../composables/useOnboarding'
import type { OnboardingState, ProcessSnapshot, SystemHealth } from '../types/api'

const available = { available: true }
const health: SystemHealth = { git: available, bun: available, node: available, couchdb: available, agent: { status: 'ready', available: true } }
const process: ProcessSnapshot = { id: 'p', workspaceId: 'w', siteId: 's', taskId: 'script:dev', status: 'running', startedAt: '', urls: ['http://127.0.0.1:3000/'], events: [] }

function state(value: Partial<OnboardingState> = {}): OnboardingState {
  return { version: 1, status: 'active', ...value }
}

describe('onboarding milestone derivation', () => {
  it('auto-opens only unfinished, unpaused journeys', () => {
    expect(shouldAutoOpenOnboarding('not_started')).toBe(true)
    expect(shouldAutoOpenOnboarding('active')).toBe(true)
    expect(shouldAutoOpenOnboarding('paused')).toBe(false)
    expect(shouldAutoOpenOnboarding('completed')).toBe(false)
  })
  it('returns the earliest technical milestone and re-enters run locally after a restart', () => {
    expect(deriveOnboardingMilestone({ state: state(), health, process: undefined, workspaceValid: false, siteValid: false, componentValid: false })).toBe(2)
    expect(deriveOnboardingMilestone({ state: state({ workspaceId: 'w', siteId: 's', componentId: 'hero', previewConfirmedAt: 'now' }), health, process: undefined, workspaceValid: true, siteValid: true, componentValid: true })).toBe(4)
    expect(deriveOnboardingMilestone({ state: state({ workspaceId: 'w', siteId: 's' }), health, workspaceValid: true, siteValid: true, process, componentValid: false })).toBe(5)
  })

  it('keeps completed journeys completed when local state changes', () => {
    expect(deriveOnboardingMilestone({ state: state({ status: 'completed' }), health: undefined, process: undefined, workspaceValid: false, siteValid: false, componentValid: false })).toBe(6)
  })
})

describe('safe builder handoff', () => {
  it('accepts only running loopback HTTP origins', () => {
    expect(validatedDevURL(process)).toBe('http://127.0.0.1:3000')
    expect(builderURL(process)).toBe('http://127.0.0.1:3000/builder/')
    expect(validatedDevURL({ ...process, urls: ['https://example.com'] })).toBe('')
    expect(validatedDevURL({ ...process, status: 'stopped' })).toBe('')
  })
})
