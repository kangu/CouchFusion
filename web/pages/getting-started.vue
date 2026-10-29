<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import JobStatus from '../components/activity/JobStatus.vue'
import LogViewer from '../components/activity/LogViewer.vue'
import SiteProvisionForm from '../components/provision/SiteProvisionForm.vue'
import ComponentGenerator from '../components/sites/ComponentGenerator.vue'
import ComponentPreviewOverlay from '../components/sites/ComponentPreviewOverlay.vue'
import AgentHealthCard from '../components/system/AgentHealthCard.vue'
import HealthCard from '../components/system/HealthCard.vue'
import WorkspaceForm from '../components/workspaces/WorkspaceForm.vue'
import { builderURL, deriveOnboardingMilestone, validatedDevURL } from '../composables/useOnboarding'
import type { SiteFormValue } from '../components/provision/SiteProvisionForm.vue'
import type { WorkspaceFormValue } from '../components/workspaces/WorkspaceForm.vue'
import type {
  AgentConversation,
  ComponentPreviewHost,
  ComponentRegistryResponse,
  ContentComponent,
  JobSnapshot,
  ProcessSnapshot,
  Site,
  SystemHealth,
  TaskRunResult,
  WorkspaceDetail,
  WorkspaceSummary,
} from '../types/api'

const api = useApi()
const onboarding = useOnboarding()
const activity = useActivityStore()
const agent = useAgentConversation()
const health = ref<SystemHealth>()
const workspaces = ref<WorkspaceSummary[]>([])
const workspace = ref<WorkspaceDetail>()
const selectedSite = ref<Site>()
const registry = ref<ComponentRegistryResponse>({ status: 'missing', components: [], canRebuild: false })
const previewHost = ref<ComponentPreviewHost>({ status: 'stopped' })
const process = ref<ProcessSnapshot>()
const activeJob = ref<JobSnapshot>()
const workspaceMode = ref<'initialize' | 'register'>('initialize')
const busy = ref('')
const error = ref('')
const announcement = ref('')
const previewComponent = ref<ContentComponent>()
const previewOpenedThisSession = ref(false)
const placementChecked = ref(false)
const lastInitializePayload = ref<WorkspaceFormValue>()
const stepHeading = ref<HTMLElement>()
const inspectedStep = ref(1)
let jobTimer: ReturnType<typeof setTimeout> | undefined
let processTimer: ReturnType<typeof setTimeout> | undefined
let previewTimer: ReturnType<typeof setTimeout> | undefined

const state = computed(() => onboarding.state.value)
const workspaceValid = computed(() => Boolean(workspace.value?.available && workspace.value.id === state.value?.workspaceId))
const siteValid = computed(() => Boolean(selectedSite.value && compatibleSite(selectedSite.value)))
const componentValid = computed(() => registry.value.status === 'ready' && registry.value.components.some(item => item.id === state.value?.componentId))
const currentStep = computed(() => deriveOnboardingMilestone({
  state: state.value,
  health: health.value,
  workspaceValid: workspaceValid.value,
  siteValid: siteValid.value,
  process: process.value,
  componentValid: componentValid.value,
}))
const devURL = computed(() => validatedDevURL(process.value))
const handoffURL = computed(() => builderURL(process.value))
const generatedComponent = computed(() => registry.value.components.find(item => item.id === state.value?.componentId))
const milestones = [
  ['Prepare local tools', 'Git, Bun, Node, and CouchDB'],
  ['Connect workspace', 'Initialize new or register exact path'],
  ['Create or select site', 'Content and Auth enabled'],
  ['Run locally', 'Install and start a managed dev server'],
  ['Create and preview', 'Generate with Codex, then inspect'],
  ['Place and verify', 'Save the homepage in Content Builder'],
] as const
const currentMilestone = computed(() => milestones[currentStep.value - 1]!)

onMounted(load)
onBeforeUnmount(() => {
  if (jobTimer) clearTimeout(jobTimer)
  if (processTimer) clearTimeout(processTimer)
  if (previewTimer) clearTimeout(previewTimer)
  agent.close()
})

watch(currentStep, async (step, previous) => {
  if (step === previous) return
  announcement.value = `Milestone ${step}: ${milestones[step - 1]![0]}`
  inspectedStep.value = step
  await nextTick()
  stepHeading.value?.focus({ preventScroll: true })
})

watch(() => ({ status: agent.conversation.value?.status, component: agent.conversation.value?.generatedComponentId || agent.conversation.value?.registry.generatedComponentId }), async (value, previous) => {
  if (!value.component || value.status !== 'ready' || (previous?.status === 'ready' && previous.component === value.component)) return
  await loadRegistry()
  await act('select_component', value.component)
})

async function load() {
  busy.value = 'load'
  error.value = ''
  try {
    await onboarding.load(true)
    if (state.value?.status === 'paused') await onboarding.dispatch({ action: 'resume' })
    const [healthResult, workspacesResult] = await Promise.all([
      api.get<SystemHealth>('/system/health'),
      api.get<WorkspaceSummary[]>('/workspaces'),
    ])
    health.value = healthResult
    workspaces.value = workspacesResult
    await recoverSelections()
  } catch (cause) {
    error.value = message(cause, 'Could not prepare the getting-started journey.')
  } finally {
    busy.value = ''
  }
}

async function recoverSelections() {
  const intent = state.value
  if (!intent) return
  if (intent.workspaceId) {
    const registered = workspaces.value.find(item => item.id === intent.workspaceId && item.available)
    if (!registered) {
      if (intent.status !== 'completed') await act('reset')
      return
    }
    workspace.value = await api.get<WorkspaceDetail>(`/workspaces/${encodeURIComponent(intent.workspaceId)}`)
  }
  if (intent.siteId && workspace.value) {
    const site = workspace.value.sites.find(item => item.id === intent.siteId && compatibleSite(item))
    if (!site) {
      if (intent.status !== 'completed') await act('select_workspace', intent.workspaceId!)
      return
    }
    selectedSite.value = site
    await loadSiteResources()
  }
  if (state.value?.activeJobId) await reconnectJob(state.value.activeJobId)
}

async function loadSiteResources() {
  const base = siteBase()
  if (!base) return
  const [registryResult, previewResult, processResult] = await Promise.allSettled([
    api.get<ComponentRegistryResponse>(`${base}/components`),
    api.get<ComponentPreviewHost>(`${base}/components/preview-host`),
    api.get<ProcessSnapshot>(`${base}/processes/script%3Adev`),
    agent.open(state.value!.workspaceId!, state.value!.siteId!),
  ])
  if (registryResult.status === 'fulfilled') registry.value = registryResult.value
  if (previewResult.status === 'fulfilled') previewHost.value = previewResult.value
  if (processResult.status === 'fulfilled') {
    process.value = processResult.value
    pollProcess()
  } else {
    process.value = undefined
  }
  if (state.value?.componentId && !registry.value.components.some(item => item.id === state.value?.componentId)) {
    if (state.value.status !== 'completed') await act('select_site', state.value.siteId!)
  }
}

function compatibleSite(site: Site) {
  const modules = new Set(site.modules.map(item => item.toLowerCase()))
  return modules.has('content') && modules.has('auth') && Boolean(site.scripts.dev) && Boolean(site.scripts['registry:build'])
}

async function act(action: Parameters<typeof onboarding.dispatch>[0]['action'], resourceId?: string) {
  try {
    await onboarding.dispatch({ action, ...(resourceId ? { resourceId } : {}) })
    error.value = ''
  } catch (cause) {
    error.value = message(cause, 'The onboarding action could not be completed.')
    throw cause
  }
}

async function submitWorkspace(payload: WorkspaceFormValue) {
  busy.value = 'workspace'
  try {
    if (workspaceMode.value === 'register') {
      const created = await api.post<WorkspaceSummary>('/workspaces/register', payload)
      workspaces.value = await api.get<WorkspaceSummary[]>('/workspaces')
      await selectWorkspace(created.id)
    } else {
      lastInitializePayload.value = { ...payload }
      watchJob(await api.post<JobSnapshot>('/workspaces/initialize', payload))
    }
  } catch (cause) {
    error.value = message(cause, 'Workspace action failed.')
  } finally {
    busy.value = ''
  }
}

async function selectWorkspace(id: string) {
  if (state.value?.workspaceId && state.value.workspaceId !== id && !window.confirm('Changing workspace clears site and component progress. Continue?')) return
  await act('select_workspace', id)
  workspace.value = await api.get<WorkspaceDetail>(`/workspaces/${encodeURIComponent(id)}`)
  selectedSite.value = undefined
  registry.value = { status: 'missing', components: [], canRebuild: false }
}

async function submitSite(payload: SiteFormValue) {
  busy.value = 'site'
  try {
    watchJob(await api.post<JobSnapshot>(`/workspaces/${encodeURIComponent(state.value!.workspaceId!)}/sites`, payload))
  } catch (cause) {
    error.value = message(cause, 'Site provisioning failed.')
  } finally {
    busy.value = ''
  }
}

async function selectSite(site: Site) {
  if (state.value?.siteId && state.value.siteId !== site.id && !window.confirm('Changing site clears generated-component progress. Continue?')) return
  await act('select_site', site.id)
  selectedSite.value = site
  await loadSiteResources()
}

async function runInstall() {
  const base = siteBase()
  if (!base) return
  busy.value = 'install'
  try {
    const result = await api.post<TaskRunResult>(`${base}/tasks/install/run`)
    if (result.job) watchJob(result.job)
  } catch (cause) { error.value = message(cause, 'Dependency installation failed to start.') }
  finally { busy.value = '' }
}

async function startDev() {
  const base = siteBase()
  if (!base) return
  busy.value = 'dev'
  try {
    process.value = await api.post<ProcessSnapshot>(`${base}/processes/script%3Adev/start`)
    activity.trackProcess(process.value)
    pollProcess()
  } catch (cause) { error.value = message(cause, 'The dev server failed to start.') }
  finally { busy.value = '' }
}

function pollProcess() {
  if (processTimer) clearTimeout(processTimer)
  if (!process.value || process.value.status !== 'running') return
  processTimer = setTimeout(async () => {
    try {
      process.value = await api.get<ProcessSnapshot>(`/processes/${encodeURIComponent(process.value!.id)}`)
      activity.trackProcess(process.value)
    } catch { process.value = undefined }
    pollProcess()
  }, 1000)
}

function watchJob(job: JobSnapshot) {
  activeJob.value = job
  activity.trackJob(job)
  void act('track_job', job.id).then(pollJob)
}

async function reconnectJob(id: string) {
  try {
    activeJob.value = await api.get<JobSnapshot>(`/jobs/${encodeURIComponent(id)}`)
    pollJob()
  } catch {
    activeJob.value = undefined
    await act('clear_job')
    await refreshInventory()
  }
}

function pollJob() {
  if (jobTimer) clearTimeout(jobTimer)
  if (!activeJob.value || terminal(activeJob.value.status)) {
    if (activeJob.value) void finishJob(activeJob.value)
    return
  }
  jobTimer = setTimeout(async () => {
    try {
      activeJob.value = await api.get<JobSnapshot>(`/jobs/${encodeURIComponent(activeJob.value!.id)}`)
      activity.trackJob(activeJob.value)
    } catch {
      activeJob.value = undefined
      await act('clear_job')
    }
    pollJob()
  }, 700)
}

async function finishJob(job: JobSnapshot) {
  if (state.value?.activeJobId === job.id) await act('clear_job')
  if (job.status === 'succeeded') await refreshInventory(job)
}

async function refreshInventory(job?: JobSnapshot) {
  workspaces.value = await api.get<WorkspaceSummary[]>('/workspaces')
  if (!state.value?.workspaceId) return
  workspace.value = await api.get<WorkspaceDetail>(`/workspaces/${encodeURIComponent(state.value.workspaceId)}`)
  if (job?.siteId) {
    const site = workspace.value.sites.find(item => item.id === job.siteId)
    if (site && compatibleSite(site)) await selectSite(site)
  }
  if (selectedSite.value) selectedSite.value = workspace.value.sites.find(item => item.id === selectedSite.value?.id)
}

async function repair(component: string) {
  busy.value = component
  try { watchJob(await api.post<JobSnapshot>(`/system/dependencies/${encodeURIComponent(component)}/install`)) }
  catch (cause) { error.value = message(cause, 'Repair failed to start.') }
  finally { busy.value = '' }
}

async function loadRegistry() {
  const base = siteBase()
  if (base) registry.value = await api.get<ComponentRegistryResponse>(`${base}/components`)
}

async function startPreviewHost() {
  const base = siteBase()
  if (!base) return
  busy.value = 'preview'
  try {
    previewHost.value = await api.post<ComponentPreviewHost>(`${base}/components/preview-host/start`)
    pollPreviewHost()
  } catch (cause) { error.value = message(cause, 'Preview host failed to start.') }
  finally { busy.value = '' }
}

function pollPreviewHost() {
  if (previewTimer) clearTimeout(previewTimer)
  if (previewHost.value.status !== 'starting') return
  previewTimer = setTimeout(async () => {
    const base = siteBase()
    if (base) previewHost.value = await api.get<ComponentPreviewHost>(`${base}/components/preview-host`)
    pollPreviewHost()
  }, 600)
}

function openPreview(component: ContentComponent) {
  if (previewHost.value.status !== 'ready' || component.id !== state.value?.componentId) return
  previewOpenedThisSession.value = true
  previewComponent.value = component
}

async function confirmPreview() {
  if (!previewOpenedThisSession.value) return
  await act('confirm_preview')
}

function openBuilder() {
  if (!handoffURL.value) return
  const opened = window.open(handoffURL.value, '_blank', 'noopener,noreferrer')
  if (opened) opened.opener = null
}

async function confirmPlacement() {
  if (!placementChecked.value) return
  await act('confirm_placement')
}

async function pause() {
  await act('pause')
  await navigateTo('/')
}

async function reset() {
  if (!window.confirm('Start another journey? This resets onboarding progress only.')) return
  await act('reset')
  workspace.value = undefined
  selectedSite.value = undefined
  process.value = undefined
  registry.value = { status: 'missing', components: [], canRebuild: false }
  placementChecked.value = false
  previewOpenedThisSession.value = false
  await load()
}

async function registerPartialInitialization() {
  const payload = lastInitializePayload.value
  if (!payload) return
  workspaceMode.value = 'register'
  busy.value = 'workspace'
  try {
    const created = await api.post<WorkspaceSummary>('/workspaces/register', { root: payload.root, name: payload.name })
    await selectWorkspace(created.id)
  } catch (cause) { error.value = message(cause, 'The partial workspace could not be registered.') }
  finally { busy.value = '' }
}

async function changeWorkspaceSelection() {
  if (!state.value?.workspaceId || !window.confirm('Change workspace selection? Site, component, preview, and placement progress will be cleared.')) return
  await act('select_workspace', state.value.workspaceId)
  selectedSite.value = undefined
  process.value = undefined
  registry.value = { status: 'missing', components: [], canRebuild: false }
}

function siteBase() {
  return state.value?.workspaceId && state.value.siteId
    ? `/workspaces/${encodeURIComponent(state.value.workspaceId)}/sites/${encodeURIComponent(state.value.siteId)}`
    : ''
}

function terminal(status: JobSnapshot['status']) { return ['succeeded', 'failed', 'cancelled'].includes(status) }
function message(cause: unknown, fallback: string) { return cause instanceof Error ? cause.message : fallback }
</script>

<template>
  <section class="page-sheet onboarding-page">
    <header class="onboarding-header">
      <div><p class="eyebrow">First website runway</p><h1>Getting started</h1><p>Six explicit milestones from local tools to a saved Content Builder homepage.</p></div>
      <div class="button-row">
        <button v-if="state?.status === 'active' || state?.status === 'not_started'" class="compact-button" type="button" @click="pause">Pause</button>
        <button v-if="state?.status === 'completed'" class="compact-button" type="button" @click="reset">Start another journey</button>
      </div>
    </header>

    <p class="sr-only" aria-live="polite">{{ announcement }}</p>
    <p v-if="error || onboarding.error.value" class="notice notice--error" role="alert">{{ error || onboarding.error.value }}</p>

    <div class="onboarding-layout">
      <nav class="milestone-rail" aria-label="Getting started milestones">
        <ol>
          <li v-for="(item, index) in milestones" :key="item[0]" :class="{ 'is-complete': index + 1 < currentStep || state?.status === 'completed', 'is-current': index + 1 === currentStep && state?.status !== 'completed' }" :aria-current="index + 1 === currentStep && state?.status !== 'completed' ? 'step' : undefined">
            <button type="button" :disabled="index + 1 > currentStep && state?.status !== 'completed'" :aria-pressed="inspectedStep === index + 1" @click="inspectedStep = index + 1"><span>{{ String(index + 1).padStart(2, '0') }}</span><div><strong>{{ item[0] }}</strong><small>{{ item[1] }}</small></div><b aria-hidden="true">{{ index + 1 < currentStep || state?.status === 'completed' ? '✓' : '—' }}</b></button>
          </li>
        </ol>
        <p class="milestone-mobile-summary">Step {{ currentStep }} of 6 · {{ currentMilestone[0] }}</p>
      </nav>

      <main class="runway-action">
        <header><p class="eyebrow">Current milestone · {{ currentStep }}/6</p><h2 ref="stepHeading" tabindex="-1">{{ currentMilestone[0] }}</h2><p>{{ currentMilestone[1] }}</p></header>

        <section v-if="currentStep === 1" class="runway-section">
          <div v-if="health" class="health-grid">
            <HealthCard name="Git" component="git" :health="health.git" />
            <HealthCard name="Bun" component="bun" :health="health.bun" repairable :busy="busy === 'bun'" @repair="repair" />
            <HealthCard name="Node.js" component="node" :health="health.node" />
            <HealthCard name="CouchDB" component="couchdb" :health="health.couchdb" repairable :busy="busy === 'couchdb'" @repair="repair" />
            <AgentHealthCard :readiness="health.agent" />
          </div>
          <div v-if="health && (!health.git.available || !health.node.available)" class="manual-repair">
            <p class="eyebrow">Manual repair</p>
            <p v-if="!health.git.available">Git: run <code>xcode-select --install</code> on macOS, or <code>sudo apt-get install git</code> on Debian/Ubuntu.</p>
            <p v-if="!health.node.available">Node: install the current LTS release from nodejs.org, then verify it with <code>node --version</code>.</p>
          </div>
          <p class="runway-note">Codex readiness is shown now, but becomes blocking only when you generate the component. Unsupported repairs include the exact manual command in their card.</p>
          <button class="compact-button" type="button" :disabled="busy === 'load'" @click="load">Run checks again</button>
        </section>

        <section v-else-if="currentStep === 2" class="runway-section">
          <div class="runway-tabs" role="tablist" aria-label="Workspace connection method"><button type="button" role="tab" :aria-selected="workspaceMode === 'initialize'" @click="workspaceMode = 'initialize'">Initialize new</button><button type="button" role="tab" :aria-selected="workspaceMode === 'register'" @click="workspaceMode = 'register'">Register existing</button></div>
          <div class="embedded-form"><WorkspaceForm :mode="workspaceMode" :busy="busy === 'workspace'" @submit="submitWorkspace" /></div>
          <button v-if="activeJob?.type === 'workspace.initialize' && activeJob.status === 'failed' && lastInitializePayload" class="compact-button" type="button" @click="registerPartialInitialization">Register this exact path</button>
          <div v-if="workspaces.length" class="selection-list"><h3>Or select a registered workspace</h3><button v-for="item in workspaces" :key="item.id" type="button" :disabled="!item.available" @click="selectWorkspace(item.id)"><strong>{{ item.name }}</strong><code>{{ item.root }}</code><span>{{ item.available ? 'Select →' : item.problem || 'Unavailable' }}</span></button></div>
        </section>

        <section v-else-if="currentStep === 3" class="runway-section">
          <div v-if="workspace?.sites.some(compatibleSite)" class="selection-list"><h3>Select a compatible site</h3><button v-for="item in workspace.sites.filter(compatibleSite)" :key="item.id" type="button" @click="selectSite(item)"><strong>{{ item.name }}</strong><span>Content + Auth · Select →</span></button></div>
          <div class="embedded-form"><SiteProvisionForm :busy="busy === 'site'" :required-modules="['content', 'auth']" @submit="submitSite" /></div>
        </section>

        <section v-else-if="currentStep === 4" class="runway-section">
          <div class="run-local-card"><div><span class="status-lozenge">{{ selectedSite?.dependenciesInstalled ? 'Dependencies present' : 'Install required' }}</span><h3>{{ selectedSite?.name }}</h3><p>Installation and the managed dev process are separate, explicit actions.</p></div><div class="button-row"><button v-if="!selectedSite?.dependenciesInstalled" class="compact-button" type="button" :disabled="busy === 'install'" @click="runInstall">Run install</button><button class="compact-button compact-button--primary" type="button" :disabled="busy === 'dev' || !selectedSite?.dependenciesInstalled" @click="startDev">Start dev server</button></div></div>
          <p v-if="process && !devURL" class="notice">The process is running; waiting for a validated loopback HTTP(S) URL…</p>
          <a v-if="devURL" class="runway-link" :href="devURL" target="_blank" rel="noopener noreferrer">Open local website ↗</a>
        </section>

        <section v-else-if="currentStep === 5" class="runway-section">
          <ComponentGenerator :readiness="agent.readiness.value || health?.agent!" :conversations="agent.conversations.value as AgentConversation[]" :conversation="agent.conversation.value as AgentConversation | undefined" :busy="agent.busy.value" :error="agent.error.value || ''" @create="agent.create" @open="id => agent.open(state!.workspaceId!, state!.siteId!, id)" @follow-up="agent.followUp" @answer="agent.answer" @approve="agent.approve" @consent="agent.consent" @interrupt="agent.interrupt" />
          <div class="preview-control"><div><h3>Preview generated component</h3><p>Starting the host and opening the fullscreen preview remain explicit.</p></div><button class="compact-button" type="button" :disabled="busy === 'preview' || previewHost.status === 'ready'" @click="startPreviewHost">{{ previewHost.status === 'starting' ? 'Starting…' : previewHost.status === 'ready' ? 'Preview host ready' : 'Start preview host' }}</button></div>
          <div v-if="generatedComponent" class="generated-component"><strong>{{ generatedComponent.label }}</strong><code>{{ generatedComponent.id }}</code><div class="button-row"><button class="compact-button compact-button--primary" type="button" :disabled="previewHost.status !== 'ready'" @click="openPreview(generatedComponent)">Open fullscreen preview</button><button class="compact-button" type="button" :disabled="!previewOpenedThisSession" @click="confirmPreview">Confirm preview</button></div></div>
        </section>

        <section v-else class="runway-section">
          <template v-if="state?.status !== 'completed'">
            <div class="builder-handoff"><p class="eyebrow">Manual handoff</p><h3>{{ generatedComponent?.label || state?.componentId }}</h3><ol><li>Open Content Builder and add this component to the homepage.</li><li>Save the page.</li><li>Verify the component in the live preview.</li></ol><p>Builder authentication uses your configured CouchDB account. CouchFusion never displays or transfers its credentials.</p><button class="compact-button compact-button--primary" type="button" :disabled="!handoffURL" @click="openBuilder">Open Content Builder ↗</button></div>
            <label class="placement-confirm"><input v-model="placementChecked" type="checkbox"> I saved the homepage and verified it in the live preview.</label><button class="action-button action-button--primary" type="button" :disabled="!placementChecked" @click="confirmPlacement">Complete getting started</button>
          </template>
          <div v-else class="completion-summary"><span aria-hidden="true">✓</span><p class="eyebrow">Journey completed</p><h3>Your first website is running.</h3><p>{{ generatedComponent?.label || state.componentId }} was generated, previewed, and manually verified in Content Builder.</p><div class="button-row"><NuxtLink class="compact-button compact-button--primary" :to="`/workspaces/${state.workspaceId}/sites/${state.siteId}`">Open site workbench</NuxtLink><button class="compact-button" type="button" @click="reset">Start another journey</button></div></div>
        </section>
      </main>

      <aside class="runway-evidence" aria-label="Journey evidence and recovery">
        <header><p class="eyebrow">Evidence / recovery</p><strong>{{ state?.status?.replace('_', ' ') || 'loading' }}</strong></header>
        <p class="inspected-milestone"><strong>{{ String(inspectedStep).padStart(2, '0') }} · {{ milestones[inspectedStep - 1]![0] }}</strong><span>{{ milestones[inspectedStep - 1]![1] }}</span></p>
        <dl><div><dt>Workspace</dt><dd>{{ workspace?.name || 'Not selected' }}</dd></div><div><dt>Site</dt><dd>{{ selectedSite?.name || 'Not selected' }}</dd></div><div><dt>Dev URL</dt><dd>{{ devURL || 'Not running' }}</dd></div><div><dt>Component</dt><dd>{{ generatedComponent?.label || state?.componentId || 'Not generated' }}</dd></div></dl>
        <template v-if="activeJob"><JobStatus :job="activeJob" /><LogViewer :events="activeJob.events" :connected="!terminal(activeJob.status)" /></template>
        <template v-else-if="process"><p class="notice">Dev process: {{ process.status }}</p><LogViewer :events="process.events" :connected="process.status === 'running'" /></template>
        <button v-if="state?.workspaceId && currentStep > 2 && state.status !== 'completed'" class="evidence-change" type="button" @click="changeWorkspaceSelection">Change selection from workspace</button>
      </aside>
    </div>

    <ComponentPreviewOverlay v-if="previewComponent" :open="true" :component="previewComponent" :preview-host="previewHost" @close="previewComponent = undefined" @restart-host="startPreviewHost" />
  </section>
</template>
