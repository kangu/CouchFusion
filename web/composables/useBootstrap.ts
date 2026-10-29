import { createGlobalState } from '@vueuse/core'
import { computed, ref } from 'vue'
import type { Bootstrap, BootstrapCapability } from '../types/api'

interface BootstrapApi {
  get<T>(path: string): Promise<T>
}

/**
 * Loads `GET /bootstrap` once and answers capability questions.
 * The Go backend returns only `{version}`, so every capability check is false there
 * and Rust-only UI (dev sessions, delete, generate, git) stays hidden.
 */
export function createBootstrap(api: BootstrapApi) {
  const state = ref<Bootstrap>()
  const error = ref('')
  let pending: Promise<Bootstrap | undefined> | undefined

  function load(force = false): Promise<Bootstrap | undefined> {
    if (state.value && !force) return Promise.resolve(state.value)
    if (pending && !force) return pending
    pending = api.get<Bootstrap>('/bootstrap')
      .then((value) => {
        state.value = value
        error.value = ''
        return value
      })
      .catch((cause: unknown) => {
        error.value = cause instanceof Error ? cause.message : 'Bootstrap unavailable.'
        return undefined
      })
      .finally(() => { pending = undefined })
    return pending
  }

  function has(capability: BootstrapCapability) {
    return state.value?.capabilities?.includes(capability) ?? false
  }

  const isRust = computed(() => state.value?.backend === 'rust')

  return { state, error, load, has, isRust }
}

const useBootstrapState = createGlobalState(() => createBootstrap(useApi()))

export function useBootstrap() {
  const bootstrap = useBootstrapState()
  if (import.meta.client) void bootstrap.load()
  return bootstrap
}
