import { computed, readonly } from 'vue'
import type { OnboardingAction, OnboardingState, OnboardingStatus, ProcessSnapshot, SystemHealth } from '../types/api'

export function shouldAutoOpenOnboarding(status?: OnboardingStatus) {
  return status === 'not_started' || status === 'active'
}

export interface MilestoneEvidence {
  state: OnboardingState | undefined
  health: SystemHealth | undefined
  workspaceValid: boolean
  siteValid: boolean
  process: ProcessSnapshot | undefined
  componentValid: boolean
}

export function deriveOnboardingMilestone(evidence: MilestoneEvidence) {
  if (evidence.state?.status === 'completed') return 6
  const health = evidence.health
  if (!health || !health.git.available || !health.bun.available || !health.node.available || !health.couchdb.available) return 1
  if (!evidence.state?.workspaceId || !evidence.workspaceValid) return 2
  if (!evidence.state.siteId || !evidence.siteValid) return 3
  if (!validatedDevURL(evidence.process)) return 4
  if (!evidence.state.componentId || !evidence.componentValid || !evidence.state.previewConfirmedAt) return 5
  return 6
}

export function validatedDevURL(process?: ProcessSnapshot) {
  if (!process || process.status !== 'running') return ''
  for (const candidate of process.urls) {
    try {
      const url = new URL(candidate)
      if ((url.protocol === 'http:' || url.protocol === 'https:') && isLoopbackHost(url.hostname)) return url.origin
    } catch { /* ignore advisory output that is not a URL */ }
  }
  return ''
}

export function builderURL(process?: ProcessSnapshot) {
  const origin = validatedDevURL(process)
  return origin ? new URL('/builder/', origin).toString() : ''
}

function isLoopbackHost(hostname: string) {
  const normalized = hostname.replace(/^\[|\]$/g, '').toLowerCase()
  if (normalized === 'localhost' || normalized === '::1') return true
  const pieces = normalized.split('.').map(Number)
  return pieces.length === 4 && pieces.every(Number.isInteger) && pieces[0] === 127 && pieces.every(value => value >= 0 && value <= 255)
}

export function useOnboarding() {
  const api = useApi()
  const state = useState<OnboardingState | undefined>('onboarding-state', () => undefined)
  const loading = useState('onboarding-loading', () => false)
  const error = useState('onboarding-error', () => '')

  async function load(force = false) {
    if (state.value && !force) return state.value
    loading.value = true
    try {
      state.value = await api.get<OnboardingState>('/onboarding')
      error.value = ''
      return state.value
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not load onboarding state.'
      throw cause
    } finally {
      loading.value = false
    }
  }

  async function dispatch(action: OnboardingAction) {
    loading.value = true
    try {
      state.value = await api.patch<OnboardingState>('/onboarding', action)
      error.value = ''
      return state.value
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not update onboarding state.'
      throw cause
    } finally {
      loading.value = false
    }
  }

  return {
    state: readonly(state),
    loading: readonly(loading),
    error: readonly(error),
    autoOpen: computed(() => shouldAutoOpenOnboarding(state.value?.status)),
    load,
    dispatch,
  }
}
