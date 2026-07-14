import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { createWorkspaceSelection } from '../composables/useWorkspace'

describe('workspace selection', () => {
  it('keeps a persisted workspace only after validating it against the server', async () => {
    const storage = ref<string | null>('w1')
    const load = vi.fn().mockResolvedValue([{ id: 'w1', name: 'Demo' }])
    const selection = createWorkspaceSelection(load, storage)

    await selection.validate()
    expect(selection.activeWorkspaceId.value).toBe('w1')
    expect(selection.ready.value).toBe(true)
  })

  it('clears stale persisted workspace IDs', async () => {
    const storage = ref<string | null>('missing')
    const selection = createWorkspaceSelection(async () => [{ id: 'w1', name: 'Demo' }], storage)

    await selection.validate()
    expect(selection.activeWorkspaceId.value).toBeNull()
    expect(storage.value).toBeNull()
  })
})
