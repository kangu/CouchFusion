<script setup lang="ts">
import type { JobSnapshot, WorkspaceSummary } from '../types/api'
const api = useApi()
const selection = useWorkspace()
const activity = useActivityStore()
const workspaces = ref<WorkspaceSummary[]>([])
const loading = ref(true)
const error = ref('')
const dialog = ref<'register' | 'initialize' | null>(null)
const forgetting = ref<WorkspaceSummary | null>(null)

onMounted(load)
async function load() {
  loading.value = true
  try { workspaces.value = await api.get<WorkspaceSummary[]>('/workspaces'); error.value = '' }
  catch (cause) { error.value = cause instanceof Error ? cause.message : 'Could not load workspaces.' }
  finally { loading.value = false }
}
async function submit(payload: { root: string; name: string; layerBranch?: string; force?: boolean }) {
  try {
    if (dialog.value === 'register') {
      const workspace = await api.post<WorkspaceSummary>('/workspaces/register', payload)
      await selection.validate(); selection.select(workspace.id); dialog.value = null; await navigateTo(`/workspaces/${workspace.id}`)
    } else {
      const job = await api.post<JobSnapshot>('/workspaces/initialize', payload)
      activity.trackJob(job); dialog.value = null; await navigateTo('/activity')
    }
  } catch (cause) { error.value = cause instanceof Error ? cause.message : 'Workspace action failed.' }
}
async function forget() {
  if (!forgetting.value) return
  await api.delete(`/workspaces/${encodeURIComponent(forgetting.value.id)}`)
  if (selection.activeWorkspaceId.value === forgetting.value.id) selection.select(null)
  forgetting.value = null; await selection.validate(); await load()
}
async function openWorkspace(workspace: WorkspaceSummary) {
  await selection.validate()
  if (selection.select(workspace.id)) await navigateTo(`/workspaces/${workspace.id}`)
}
function date(value: string) { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : 'Never' }
</script>

<template>
  <section class="page-sheet">
    <header class="page-header"><div><p class="eyebrow">Explicit registry</p><h1>Workspaces</h1><p>Known locations only. CouchFusion never searches your disk for projects.</p></div><div class="button-row"><button class="compact-button" type="button" @click="dialog = 'register'">Register existing</button><button class="compact-button compact-button--primary" type="button" @click="dialog = 'initialize'">Initialize new</button></div></header>
    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p><p v-if="loading" class="empty-state">Reading the explicit registry…</p>
    <div v-else-if="workspaces.length" class="card-grid">
      <article v-for="workspace in workspaces" :key="workspace.id" class="workspace-card" :class="{ 'workspace-card--unavailable': !workspace.available }"><header><span class="signal" :class="{ 'signal--danger': !workspace.available }" /><span>{{ workspace.available ? 'Available' : 'Unavailable' }}</span></header><h2>{{ workspace.name }}</h2><code>{{ workspace.root }}</code><p v-if="workspace.problem" class="notice notice--error">{{ workspace.problem }}</p><small>Last opened {{ date(workspace.lastOpenedAt) }}</small><div class="button-row"><button class="compact-button compact-button--primary" type="button" :disabled="!workspace.available" @click="openWorkspace(workspace)">Open</button><button class="compact-button" type="button" @click="forgetting = workspace">Forget</button></div></article>
    </div>
    <div v-else class="empty-state"><p class="eyebrow">Registry empty</p><h2>Connect the first workspace.</h2><p>Enter an exact absolute path to register an existing project, or initialize a new CouchFusion structure.</p></div>
  </section>
  <WorkspaceDialog :open="dialog !== null" :mode="dialog || 'register'" @close="dialog = null" @submit="submit" />
  <div v-if="forgetting" class="dialog-backdrop" @click.self="forgetting = null"><section class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="forget-title"><p class="eyebrow">Registry only</p><h2 id="forget-title">Forget {{ forgetting.name }}?</h2><p>This removes the workspace from CouchFusion’s registry. Files at <code>{{ forgetting.root }}</code> remain untouched.</p><div class="button-row"><button class="action-button" type="button" @click="forgetting = null">Keep workspace</button><button class="action-button action-button--primary" type="button" @click="forget">Forget registration</button></div></section></div>
</template>
