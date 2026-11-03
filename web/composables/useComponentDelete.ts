import { computed, ref } from 'vue'
import type { ComponentUsage, JobSnapshot } from '../types/api'

interface DeleteApi {
  get<T>(path: string): Promise<T>
  delete<T>(path: string, body?: unknown): Promise<T>
}

/**
 * A delete needs explicit acknowledgement when anything may still use the component,
 * or when the page scan could not run (dev server down): contract §2.2, spec §6.1 step 4.
 */
export function usageRequiresAcknowledgement(usage: Pick<ComponentUsage, 'pages' | 'codeReferences' | 'pageScan'>) {
  return usage.pages.length > 0 || usage.codeReferences.length > 0 || usage.pageScan !== 'ok'
}

export function canConfirmDelete(usage: ComponentUsage | undefined, acknowledged: boolean) {
  if (!usage) return false
  return !usageRequiresAcknowledgement(usage) || acknowledged
}

/** Body for `DELETE …/components/{id}`. `acknowledgeUsage` is only true when the user ticked it. */
export function deleteRequestBody(usage: ComponentUsage, acknowledged: boolean) {
  return { expectedSha256: usage.sha256, acknowledgeUsage: usageRequiresAcknowledgement(usage) && acknowledged }
}

/** State for the parent-side delete confirmation modal. */
export function createComponentDelete(api: DeleteApi, workspaceId: string, siteId: string) {
  const componentId = ref<string>()
  const requestId = ref<string>()
  const usage = ref<ComponentUsage>()
  const acknowledged = ref(false)
  const loading = ref(false)
  const submitting = ref(false)
  const error = ref('')
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}/components`

  const requiresAck = computed(() => usage.value ? usageRequiresAcknowledgement(usage.value) : false)
  const canConfirm = computed(() => canConfirmDelete(usage.value, acknowledged.value) && !submitting.value)

  async function open(id: string, fromRequest?: string) {
    componentId.value = id
    requestId.value = fromRequest
    usage.value = undefined
    acknowledged.value = false
    error.value = ''
    loading.value = true
    try {
      usage.value = await api.get<ComponentUsage>(`${base}/${encodeURIComponent(id)}/usage`)
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : 'Could not inspect the component.'
    } finally {
      loading.value = false
    }
  }

  /** Sends the DELETE. On a 409 (changed file, new usage) it re-reads usage so the user sees fresh facts. */
  async function confirm(): Promise<JobSnapshot | undefined> {
    const current = usage.value
    if (!current || !componentId.value || !canConfirmDelete(current, acknowledged.value)) return undefined
    submitting.value = true
    try {
      const job = await api.delete<JobSnapshot>(`${base}/${encodeURIComponent(componentId.value)}`, deleteRequestBody(current, acknowledged.value))
      error.value = ''
      return job
    } catch (cause) {
      const code = (cause as { code?: string }).code
      error.value = cause instanceof Error ? cause.message : 'Delete failed.'
      if (code === 'conflict' && componentId.value) {
        const message = error.value
        await open(componentId.value, requestId.value)
        error.value = message
      }
      return undefined
    } finally {
      submitting.value = false
    }
  }

  function close() {
    componentId.value = undefined
    requestId.value = undefined
    usage.value = undefined
    acknowledged.value = false
    error.value = ''
  }

  return { componentId, requestId, usage, acknowledged, loading, submitting, error, requiresAck, canConfirm, open, confirm, close }
}
