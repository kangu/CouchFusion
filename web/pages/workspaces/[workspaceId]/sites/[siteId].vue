<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import LogViewer from '../../../../components/activity/LogViewer.vue'
import ComponentRegistryPanel from '../../../../components/sites/ComponentRegistryPanel.vue'
import TaskList from '../../../../components/sites/TaskList.vue'
import type {
  ActivityEvent,
  ComponentRegistryResponse,
  JobSnapshot,
  ProcessSnapshot,
  Site,
  Task,
  TaskRunResult,
} from '../../../../types/api'

definePageMeta({ middleware: 'workspace' })

const route = useRoute()
const api = useApi()
const hub = useActivity()
const activity = useActivityStore()
const workspaceId = computed(() => String(route.params.workspaceId))
const siteId = computed(() => String(route.params.siteId))
const base = computed(() => `/workspaces/${encodeURIComponent(workspaceId.value)}/sites/${encodeURIComponent(siteId.value)}`)
const site = ref<Site>()
const tasks = ref<Task[]>([])
const registry = ref<ComponentRegistryResponse>({ status: 'missing', components: [], canRebuild: false })
const activeJob = ref<JobSnapshot>()
const activeProcess = ref<ProcessSnapshot>()
const connected = ref(true)
const busyTask = ref('')
const rebuilding = ref(false)
const openingId = ref('')
const error = ref('')
const registryError = ref('')
let unsubscribe: (() => void) | undefined

onMounted(load)
onBeforeUnmount(() => unsubscribe?.())

async function load() {
  error.value = ''
  registryError.value = ''
  const [siteResult, taskResult, registryResult] = await Promise.allSettled([
    api.get<Site>(base.value),
    api.get<Task[]>(`${base.value}/tasks`),
    api.get<ComponentRegistryResponse>(`${base.value}/components`),
  ])
  if (siteResult.status === 'fulfilled') site.value = siteResult.value
  else error.value = message(siteResult.reason, 'Could not inspect site.')
  if (taskResult.status === 'fulfilled') tasks.value = taskResult.value
  else error.value = message(taskResult.reason, 'Could not load curated tasks.')
  if (registryResult.status === 'fulfilled') registry.value = registryResult.value
  else registryError.value = message(registryResult.reason, 'Could not inspect local content components.')
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

      <div class="site-detail-grid">
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
          :registry="registry"
          :rebuilding="rebuilding"
          :opening-id="openingId"
          :error="registryError"
          @rebuild="rebuildRegistry"
          @open="openComponent"
        />
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
    </template>
    <p v-else-if="!error" class="empty-state">Inspecting site and task catalog…</p>
  </section>
</template>

<style scoped>
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
