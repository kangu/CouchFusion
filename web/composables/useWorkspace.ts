import { createGlobalState, useLocalStorage } from '@vueuse/core'
import { ref, watch, type Ref } from 'vue'
import type { WorkspaceSummary } from '../types/api'

type WorkspaceIdentity = Pick<WorkspaceSummary, 'id'>

export function createWorkspaceSelection(loadWorkspaces: () => Promise<WorkspaceIdentity[]>, storage: Ref<string | null>) {
  const activeWorkspaceId = ref<string | null>(storage.value)
  const ready = ref(false)
  const loading = ref(false)
  const workspaces = ref<WorkspaceIdentity[]>([])

  watch(activeWorkspaceId, (value) => {
    storage.value = value
  })

  async function validate() {
    loading.value = true
    try {
      workspaces.value = await loadWorkspaces()
      if (activeWorkspaceId.value && !workspaces.value.some(({ id }) => id === activeWorkspaceId.value)) {
        activeWorkspaceId.value = null
        storage.value = null
      }
      ready.value = true
    } finally {
      loading.value = false
    }
  }

  function select(workspaceId: string | null) {
    if (workspaceId !== null && ready.value && !workspaces.value.some(({ id }) => id === workspaceId)) return false
    activeWorkspaceId.value = workspaceId
    return true
  }

  return { activeWorkspaceId, ready, loading, workspaces, validate, select }
}

const useWorkspaceState = createGlobalState(() => {
  const api = useApi()
  const storage = useLocalStorage<string | null>('couchfusion.active-workspace', null)
  return createWorkspaceSelection(() => api.get<WorkspaceSummary[]>('/workspaces'), storage)
})

export function useWorkspace() {
  const state = useWorkspaceState()
  if (import.meta.client && !state.ready.value && !state.loading.value) void state.validate()
  return state
}
