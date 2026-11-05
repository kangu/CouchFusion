<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import LogViewer from '../../../../components/activity/LogViewer.vue'
import ComponentGenerator from '../../../../components/sites/ComponentGenerator.vue'
import ComponentRegistryPanel from '../../../../components/sites/ComponentRegistryPanel.vue'
import ComponentPreviewOverlay from '../../../../components/sites/ComponentPreviewOverlay.vue'
import TaskList from '../../../../components/sites/TaskList.vue'
import ProductionPanel from '../../../../components/prod/ProductionPanel.vue'
import DevSessionCard from '../../../../components/sites/DevSessionCard.vue'
import { modelLabel } from '../../../../composables/useLlmRequest'
import { useAgentConversation } from '../../../../composables/useAgentConversation'
import type {
  ActivityEvent,
  AgentConversation,
  AgentReadiness,
  ComponentRegistryResponse,
  ComponentPreviewHost,
  ContentComponent,
  DevSessionView,
  ProdView,
  JobSnapshot,
  ProcessSnapshot,
  Site,
  Task,
  TaskRunResult,
} from '../../../../types/api'

definePageMeta({ middleware: 'workspace' })

const route = useRoute()
const api = useApi()
const bootstrap = useBootstrap()
/** The Rust control plane (VPS) has no curated tasks, preview host or Go agent conversations. */
const isRust = computed(() => bootstrap.isRust.value)
const devView = ref<DevSessionView>()
const prodView = ref<ProdView>()
/** Dev slot origin as this browser reaches it (tailnet or SSH tunnel). */
const devSiteUrl = computed(() => devView.value?.url?.replace(/\/+$/, '') || '')
const hub = useActivity()
const activity = useActivityStore()
const agent = useAgentConversation()
const agentReadinessState = agent.readiness
const agentConversations = agent.conversations
const agentConversation = agent.conversation
const agentBusy = agent.busy
const agentError = agent.error
const workspaceId = computed(() => String(route.params.workspaceId))
const siteId = computed(() => String(route.params.siteId))
const base = computed(() => `/workspaces/${encodeURIComponent(workspaceId.value)}/sites/${encodeURIComponent(siteId.value)}`)
const site = ref<Site>()
const tasks = ref<Task[]>([])
const registry = ref<ComponentRegistryResponse>({ status: 'missing', components: [], canRebuild: false })
const previewHost = ref<ComponentPreviewHost>({ status: 'stopped' })
const activeJob = ref<JobSnapshot>()
const activeProcess = ref<ProcessSnapshot>()
const connected = ref(true)
const busyTask = ref('')
const rebuilding = ref(false)
const openingId = ref('')
const error = ref('')
const registryError = ref('')
const previewBusy = ref(false)
const previewingComponent = ref<ContentComponent>()
const previewReturnFocus = ref<HTMLElement>()
const registryPanel = ref<{ $el: HTMLElement }>()
let unsubscribe: (() => void) | undefined
let previewPoll: ReturnType<typeof setTimeout> | undefined
let highlightTimer: ReturnType<typeof setTimeout> | undefined

const agentReadiness = computed<AgentReadiness>(() => agentReadinessState.value || {
  status: 'restarting', available: false, problem: 'Agent readiness is loading.',
})
const agentConversationsView = computed<AgentConversation[]>(() => agentConversations.value as unknown as AgentConversation[])
const agentConversationView = computed<AgentConversation | undefined>(() => agentConversation.value as AgentConversation | undefined)

onMounted(load)
onBeforeUnmount(() => {
  if (isRust.value) return
  unsubscribe?.()
  if (previewPoll) clearTimeout(previewPoll)
  if (highlightTimer) clearTimeout(highlightTimer)
  agent.close()
})

watch(() => ({
  id: agentConversation.value?.id,
  state: agentConversation.value?.registry.state,
  componentId: agentConversation.value?.generatedComponentId || agentConversation.value?.registry.generatedComponentId,
}), async (current, previous) => {
  if (!current.id || current.id !== previous?.id || current.state !== 'succeeded' || previous.state === 'succeeded') return
  await loadComponents()
  if (current.componentId) await highlightGeneratedComponent(current.componentId)
})

async function load() {
  error.value = ''
  registryError.value = ''
  await bootstrap.load()
  if (isRust.value) {
    const [siteResult, registryResult, devResult, prodResult] = await Promise.allSettled([
      api.get<Site>(base.value),
      api.get<ComponentRegistryResponse>(`${base.value}/components`),
      api.get<DevSessionView>(`${base.value}/dev`),
      bootstrap.has('prod-releases') ? api.get<ProdView>(`${base.value}/prod`) : Promise.resolve(undefined),
    ])
    if (prodResult.status === 'fulfilled' && prodResult.value) prodView.value = prodResult.value
    if (siteResult.status === 'fulfilled') site.value = siteResult.value
    else error.value = message(siteResult.reason, 'Could not inspect site.')
    if (registryResult.status === 'fulfilled') registry.value = registryResult.value
    else registryError.value = message(registryResult.reason, 'Could not inspect local content components.')
    if (devResult.status === 'fulfilled') devView.value = devResult.value
    return
  }
  const [siteResult, taskResult, registryResult, previewResult] = await Promise.allSettled([
    api.get<Site>(base.value),
    api.get<Task[]>(`${base.value}/tasks`),
    api.get<ComponentRegistryResponse>(`${base.value}/components`),
    api.get<ComponentPreviewHost>(`${base.value}/components/preview-host`),
    agent.open(workspaceId.value, siteId.value),
  ])
  if (siteResult.status === 'fulfilled') site.value = siteResult.value
  else error.value = message(siteResult.reason, 'Could not inspect site.')
  if (taskResult.status === 'fulfilled') tasks.value = taskResult.value
  else error.value = message(taskResult.reason, 'Could not load curated tasks.')
  if (registryResult.status === 'fulfilled') registry.value = registryResult.value
  else registryError.value = message(registryResult.reason, 'Could not inspect local content components.')
  if (previewResult.status === 'fulfilled') {
    previewHost.value = previewResult.value
    schedulePreviewPoll()
  } else {
    previewHost.value = { status: 'failed', problem: message(previewResult.reason, 'Could not inspect the preview host.') }
  }
}

async function createAgentConversation(prompt: string) {
  await agent.create(prompt)
}

async function openAgentConversation(conversationId: string) {
  await agent.open(workspaceId.value, siteId.value, conversationId)
}

async function answerAgentQuestion(requestId: string, answers: Record<string, string>) {
  await agent.answer(requestId, answers)
}

async function approveAgentAction(requestId: string, decision: Parameters<typeof agent.approve>[1]) {
  await agent.approve(requestId, decision)
}

async function grantAgentConsent(kind: Parameters<typeof agent.consent>[0], granted: boolean) {
  await agent.consent(kind, granted)
}

async function highlightGeneratedComponent(componentId: string) {
  await nextTick()
  const trigger = Array.from(registryPanel.value?.$el.querySelectorAll<HTMLElement>('[data-component]') || [])
    .find(candidate => candidate.dataset.component === componentId)
  const row = trigger?.closest<HTMLElement>('.component-row')
  if (!row) return
  row.classList.add('component-row--generated')
  row.tabIndex = -1
  row.focus({ preventScroll: true })
  row.scrollIntoView?.({ behavior: 'smooth', block: 'center' })
  if (highlightTimer) clearTimeout(highlightTimer)
  highlightTimer = setTimeout(() => row.classList.remove('component-row--generated'), 4000)
}

async function loadComponents() {
  try {
    registry.value = await api.get<ComponentRegistryResponse>(`${base.value}/components`)
    registryError.value = ''
  } catch (cause) {
    registryError.value = message(cause, 'Could not refresh local content components.')
  }
}

async function run(taskId: string) {
  busyTask.value = taskId
  try {
    const result = await api.post<TaskRunResult>(`${base.value}/tasks/${encodeURIComponent(taskId)}/run`)
    if (result.job) watchJob(result.job)
    if (result.process) watchProcess(result.process)
  } catch (cause) {
    error.value = message(cause, 'Task failed to start.')
  } finally {
    busyTask.value = ''
  }
}

async function start(taskId: string) {
  busyTask.value = taskId
  try {
    watchProcess(await api.post<ProcessSnapshot>(`${base.value}/processes/${encodeURIComponent(taskId)}/start`))
  } catch (cause) {
    error.value = message(cause, 'Process failed to start.')
  } finally {
    busyTask.value = ''
  }
}

async function rebuildRegistry() {
  rebuilding.value = true
  registryError.value = ''
  try {
    const job = await api.post<JobSnapshot>(`${base.value}/components/registry/rebuild`)
    watchJob(job, async (snapshot) => {
      rebuilding.value = false
      if (snapshot.status === 'succeeded') await loadComponents()
      else registryError.value = snapshot.error?.message || 'Component registry rebuild did not complete.'
    })
  } catch (cause) {
    rebuilding.value = false
    registryError.value = message(cause, 'Could not start the component registry rebuild.')
  }
}

async function openComponent(componentId: string) {
  openingId.value = componentId
  registryError.value = ''
  try {
    await api.post(`${base.value}/components/${encodeURIComponent(componentId)}/open`)
  } catch (cause) {
    registryError.value = message(cause, 'Could not open the component source.')
  } finally {
    openingId.value = ''
  }
}

async function startPreviewHost() {
  previewBusy.value = true
  registryError.value = ''
  try {
    previewHost.value = await api.post<ComponentPreviewHost>(`${base.value}/components/preview-host/start`)
    schedulePreviewPoll()
  } catch (cause) {
    previewHost.value = { status: 'failed', problem: message(cause, 'Could not start the preview host.') }
  } finally {
    previewBusy.value = false
  }
}

async function stopPreviewHost() {
  previewBusy.value = true
  registryError.value = ''
  if (previewPoll) clearTimeout(previewPoll)
  previewPoll = undefined
  try {
    previewHost.value = await api.post<ComponentPreviewHost>(`${base.value}/components/preview-host/stop`)
  } catch (cause) {
    registryError.value = message(cause, 'Could not stop the preview host.')
  } finally {
    previewBusy.value = false
  }
}

function schedulePreviewPoll() {
  if (previewPoll) clearTimeout(previewPoll)
  previewPoll = undefined
  if (previewHost.value.status !== 'starting' && !previewingComponent.value) return
  previewPoll = setTimeout(async () => {
    previewPoll = undefined
    try {
      previewHost.value = await api.get<ComponentPreviewHost>(`${base.value}/components/preview-host`)
    } catch (cause) {
      previewHost.value = { status: 'failed', problem: message(cause, 'Could not inspect the preview host.') }
    }
    schedulePreviewPoll()
  }, 500)
}

function previewComponent(componentId: string, trigger: HTMLButtonElement) {
  const component = registry.value.components.find(candidate => candidate.id === componentId)
  if (!component || previewHost.value.status !== 'ready') return
  previewReturnFocus.value = trigger
  previewingComponent.value = component
  schedulePreviewPoll()
}

function closePreview() {
  previewingComponent.value = undefined
  schedulePreviewPoll()
}

function watchJob(job: JobSnapshot, onTerminal?: (job: JobSnapshot) => void | Promise<void>) {
  unsubscribe?.()
  activeJob.value = job
  activity.trackJob(job)
  connected.value = true
  let completed = false
  const finish = (snapshot: JobSnapshot) => {
    if (completed || !terminalJob(snapshot.status)) return
    completed = true
    void onTerminal?.(snapshot)
  }
  finish(job)
  if (completed) return
  unsubscribe = hub.subscribe('job', job.id, async (_event: ActivityEvent) => {
    const snapshot = await api.get<JobSnapshot>(`/jobs/${job.id}`)
    activeJob.value = snapshot
    activity.trackJob(snapshot)
    connected.value = true
    finish(snapshot)
  })
}

function watchProcess(process: ProcessSnapshot) {
  unsubscribe?.()
  activeProcess.value = process
  activity.trackProcess(process)
  connected.value = true
  unsubscribe = hub.subscribe('process', process.id, async () => {
    const snapshot = await api.get<ProcessSnapshot>(`/processes/${process.id}`)
    activeProcess.value = snapshot
    activity.trackProcess(snapshot)
    connected.value = true
  })
}

async function processAction(action: 'stop' | 'restart') {
  if (!activeProcess.value) return
  const snapshot = await api.post<ProcessSnapshot>(`/processes/${activeProcess.value.id}/${action}`)
  activeProcess.value = snapshot
  activity.trackProcess(snapshot)
}

function terminalJob(status: JobSnapshot['status']) {
  return status === 'succeeded' || status === 'failed' || status === 'cancelled'
}

function message(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback
}
</script>

<template>
  <section class="page-sheet">
    <p v-if="error" class="notice notice--error">{{ error }}</p>
    <template v-if="site">
      <header class="page-header">
        <div>
          <p class="eyebrow">Site detail</p>
          <h1>{{ site.name }}</h1>
          <code>{{ site.root }}</code>
        </div>
        <span class="status-lozenge">{{ site.dependenciesInstalled ? 'Ready' : 'Install required' }}</span>
      </header>

      <div v-if="isRust" class="site-detail-grid" data-testid="site-rust">
        <aside class="site-detail-rail">
          <DevSessionCard :workspace-id="workspaceId" :site-id="siteId" @update="devView = $event">
            <div class="button-row"><NuxtLink class="compact-button" :to="`/workspaces/${workspaceId}/dev/${siteId}`">Open dev page</NuxtLink></div>
            <p class="site-rust__meta">LLM · {{ modelLabel(bootstrap.state.value?.agent) }}. Builder, New component, LLM request, components, git and the dev log live on the dev page.</p>
          </DevSessionCard>
          <section class="panel" data-testid="site-links">
            <p class="eyebrow">Open the site</p>
            <ul class="site-rust__links">
              <li v-if="devSiteUrl && devView?.status === 'ready'"><a :href="`${devSiteUrl}/`" target="_blank" rel="noreferrer">Dev site ↗</a><small>nuxt dev, live code</small></li>
              <li v-if="devSiteUrl && devView?.status === 'ready'"><a :href="`${devSiteUrl}/builder/`" target="_blank" rel="noreferrer">Dev builder ↗</a><small>editor on the dev server</small></li>
              <li v-else-if="devSiteUrl"><span>Dev site</span><small>offline while the dev server is stopped</small></li>
              <li v-if="prodView?.previewUrl"><a :href="prodView.previewUrl" target="_blank" rel="noreferrer">Production preview ↗</a><small>live release, tailnet only</small></li>
              <li v-if="prodView?.publicUrl"><a :href="prodView.publicUrl" target="_blank" rel="noreferrer">Public site ↗</a><small>{{ prodView.publicUrl.replace(/^https?:\/\//, '') }}</small></li>
            </ul>
          </section>
          <section class="panel fingerprint-panel">
            <p class="eyebrow">Project fingerprint</p>
            <dl class="review-list">
              <div><dt>Package</dt><dd>{{ site.packageName || '—' }}</dd></div>
              <div><dt>Manager</dt><dd>{{ site.packageManager || '—' }}</dd></div>
              <div><dt>Lockfiles</dt><dd>{{ site.lockfiles.join(', ') || 'None' }}</dd></div>
              <div><dt>Dependencies</dt><dd>{{ Object.keys(site.dependencies).length + Object.keys(site.devDependencies).length }}</dd></div>
            </dl>
            <div class="chip-row"><span v-for="module in site.modules" :key="module">{{ module }}</span></div>
          </section>
        </aside>
        <div class="site-rust__main">
          <ProductionPanel v-if="bootstrap.has('prod-releases')" :workspace-id="workspaceId" :site-id="siteId" />
          <section class="panel">
            <p class="eyebrow">Content components · {{ registry.status }}</p>
            <h2>{{ registry.components.length }} components</h2>
            <p v-if="registryError" class="notice notice--error">{{ registryError }}</p>
            <p v-else-if="registry.problem" class="notice">{{ registry.problem }}</p>
            <table class="services-table">
              <thead><tr><th>Component</th><th>Source</th><th>Props</th></tr></thead>
              <tbody>
                <tr v-for="component in registry.components" :key="component.id"><td><strong>{{ component.label || component.id }}</strong><br><code>{{ component.id }}</code></td><td><code>{{ component.source }}</code></td><td>{{ component.propCount }}</td></tr>
              </tbody>
            </table>
          </section>
        </div>
      </div>

      <div v-else class="site-detail-grid">
        <aside class="site-detail-rail">
          <section class="panel task-panel" aria-label="Curated tasks">
            <p class="eyebrow">Curated tasks</p>
            <h2>Operations</h2>
            <TaskList :tasks="tasks" :busy-task="busyTask" @run="run" @start="start" />
          </section>
          <section class="panel fingerprint-panel">
            <p class="eyebrow">Project fingerprint</p>
            <dl class="review-list">
              <div><dt>Package</dt><dd>{{ site.packageName || '—' }}</dd></div>
              <div><dt>Manager</dt><dd>{{ site.packageManager || '—' }}</dd></div>
              <div><dt>Lockfiles</dt><dd>{{ site.lockfiles.join(', ') || 'None' }}</dd></div>
              <div><dt>Dependencies</dt><dd>{{ Object.keys(site.dependencies).length + Object.keys(site.devDependencies).length }}</dd></div>
            </dl>
            <div class="chip-row"><span v-for="module in site.modules" :key="module">{{ module }}</span></div>
          </section>
        </aside>

        <ComponentRegistryPanel
          ref="registryPanel"
          :registry="registry"
          :rebuilding="rebuilding"
          :opening-id="openingId"
          :error="registryError"
          :preview-host="previewHost"
          :preview-busy="previewBusy"
          :previewing-id="previewingComponent?.id || ''"
          @rebuild="rebuildRegistry"
          @open="openComponent"
          @start-preview-host="startPreviewHost"
          @stop-preview-host="stopPreviewHost"
          @preview="previewComponent"
        >
          <template #generator>
            <ComponentGenerator
              :readiness="agentReadiness"
              :conversations="agentConversationsView"
              :conversation="agentConversationView"
              :busy="agentBusy"
              :error="agentError || ''"
              @create="createAgentConversation"
              @open="openAgentConversation"
              @follow-up="agent.followUp"
              @answer="answerAgentQuestion"
              @approve="approveAgentAction"
              @consent="grantAgentConsent"
              @interrupt="agent.interrupt"
            />
            <button
              v-if="agentConversation?.registry.state === 'failed'"
              class="compact-button"
              type="button"
              data-action="retry-agent-registry"
              :disabled="rebuilding"
              @click="rebuildRegistry"
            >Retry rebuild</button>
          </template>
        </ComponentRegistryPanel>
      </div>

      <section v-if="activeJob || activeProcess" class="panel panel--dark">
        <header class="process-header">
          <div>
            <p class="eyebrow">Current activity</p>
            <h2>{{ activeProcess ? activeProcess.status : activeJob?.status }}</h2>
            <code class="activity-kind">{{ activeProcess ? activeProcess.taskId : activeJob?.type }}</code>
          </div>
          <div v-if="activeProcess" class="button-row">
            <button class="compact-button" type="button" @click="processAction('restart')">Restart</button>
            <button class="compact-button compact-button--danger" type="button" @click="processAction('stop')">Stop</button>
          </div>
        </header>
        <div v-if="activeProcess?.urls.length" class="url-list">
          <a v-for="url in activeProcess.urls" :key="url" :href="url" target="_blank" rel="noreferrer">{{ url }} ↗</a>
        </div>
        <LogViewer :events="activeProcess?.events || activeJob?.events || []" :connected="connected" />
      </section>

      <ComponentPreviewOverlay
        v-if="previewingComponent"
        :open="true"
        :component="previewingComponent"
        :preview-host="previewHost"
        :return-focus="previewReturnFocus"
        @close="closePreview"
        @restart-host="startPreviewHost"
      />
    </template>
    <p v-else-if="!error" class="empty-state">Inspecting site and task catalog…</p>
  </section>
</template>

<style scoped>
.site-rust__main { display: grid; gap: 1rem; min-width: 0; }
.site-rust__meta { margin: .4rem 0; font: .72rem/1.5 var(--mono); color: #4c5651; }
.site-rust__links { display: grid; gap: .55rem; margin: .4rem 0 0; padding: 0; list-style: none; font: .76rem/1.4 var(--mono); }
.site-rust__links a { font-weight: 700; color: var(--ink); }
.site-rust__links small { display: block; color: #4c5651; font-size: .66rem; }
.site-detail-grid {
  display: grid;
  grid-template-columns: minmax(15rem, .62fr) minmax(0, 1.38fr);
  align-items: start;
  gap: 1rem;
  margin-bottom: 1rem;
}

.site-detail-rail {
  display: grid;
  gap: 1rem;
  min-width: 0;
}

.site-detail-rail .panel,
.site-detail-grid :deep(.component-registry) {
  margin-top: 0;
}

.task-panel h2 {
  margin: 0 0 .9rem;
  font: 700 clamp(1.15rem, 1.8vw, 1.45rem)/1 var(--display);
  text-transform: uppercase;
}

.fingerprint-panel .eyebrow {
  margin-bottom: .65rem;
}

.activity-kind {
  display: block;
  margin-top: .45rem;
  color: var(--cyan);
  font: .6rem/1.3 var(--mono);
}

@media (max-width: 880px) {
  .site-detail-grid {
    grid-template-columns: 1fr;
  }

  .site-detail-grid :deep(.component-registry) {
    order: 1;
  }

  .site-detail-rail {
    order: 2;
  }
}
</style>
