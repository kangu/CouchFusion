<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import '../../../../assets/css/dev-session.css'
import type { ActivityEvent, ComponentDeleteResult, ComponentGenerateResult, JobSnapshot, SiteEvent } from '../../../../types/api'
import { createBuilderBridge, type BridgeTarget } from '../../../../composables/useBuilderBridge'
import { createComponentDelete } from '../../../../composables/useComponentDelete'
import { createDevSession } from '../../../../composables/useDevSession'
import { createGitRepos } from '../../../../composables/useGitRepos'
import { createJobTracker, isTerminalJob } from '../../../../composables/useJobTracker'
import { useSiteEvents } from '../../../../composables/useSiteEvents'

/**
 * Full-screen dev session: the site's builder in an iframe on its dev slot, with a thin
 * control bar. Delete/create requests from the builder arrive over the postMessage bridge
 * and are always confirmed here before any API call (contract §7).
 */
definePageMeta({ layout: false, middleware: 'workspace' })

const route = useRoute()
const api = useApi()
const workspaceId = String(route.params.workspaceId)
const siteId = String(route.params.siteId)
const frameRoute = computed(() => {
  const value = typeof route.query.route === 'string' ? route.query.route : '/builder/'
  return value.startsWith('/') ? value : `/${value}`
})

const dev = createDevSession(api, workspaceId, siteId)
const git = createGitRepos(api, workspaceId)
const remove = createComponentDelete(api, workspaceId, siteId)
const tracker = createJobTracker(api)

const LOG_LIMIT = 800
const logs = ref<ActivityEvent[]>([])
const drawer = ref<'agent' | 'git' | 'logs' | null>(null)
const notice = ref('')

// ---- iframe + bridge ----
const iframe = ref<HTMLIFrameElement>()
const frameKey = ref(0)
const frameSrc = ref('')
const openedWindow = ref<Window | null>(null)

watch(() => [dev.ready.value, dev.view.value?.url] as const, ([ready, url]) => {
  // Set the frame source once per ready transition; status refreshes must not reload it.
  if (ready && url && !frameSrc.value) frameSrc.value = `${url}${frameRoute.value}`
  if (!ready && dev.status.value === 'stopped') frameSrc.value = ''
}, { immediate: true })

function reassignFrame() {
  frameKey.value += 1
}

const frameBridge = createBuilderBridge({
  slotOrigin: () => dev.slotOrigin.value,
  target: () => (iframe.value?.contentWindow ?? null) as BridgeTarget | null,
  reassignFrame,
  onDeleteRequest: (componentId, requestId) => { void openDelete(componentId, requestId, frameBridge) },
  onCreateRequest: (hint, requestId) => openAgent(hint, requestId, frameBridge),
  onReloadDeferred: () => { notice.value = 'The builder has unsaved changes. Save, then use Reload frame.' },
})

const tabBridge = createBuilderBridge({
  slotOrigin: () => dev.slotOrigin.value,
  target: () => (openedWindow.value && !openedWindow.value.closed ? openedWindow.value : null) as BridgeTarget | null,
  onDeleteRequest: (componentId, requestId) => { void openDelete(componentId, requestId, tabBridge) },
  onCreateRequest: (hint, requestId) => openAgent(hint, requestId, tabBridge),
})

type Bridge = typeof frameBridge
const requestOrigin = ref<{ bridge: Bridge; requestId: string }>()

function openInTab() {
  if (!frameSrc.value) return
  // No `noopener`: the opened builder talks back through window.opener (contract §7).
  openedWindow.value = window.open(frameSrc.value, `cf-dev-${siteId}`)
  tabBridge.reset()
}

function reloadEverywhere(reason: string, focusComponentId?: string) {
  notice.value = ''
  frameBridge.requestReload(reason, focusComponentId)
  if (tabBridge.connected.value) tabBridge.requestReload(reason, focusComponentId)
}

// ---- site events ----
const events = useSiteEvents({
  workspaceId,
  siteId,
  onEvent: handleSiteEvent,
  onReconnect: async () => { await dev.load() },
})

function handleSiteEvent(event: SiteEvent) {
  if (event.type === 'dev.status' && event.dev) {
    const wasReady = dev.ready.value
    dev.apply(event.dev)
    if (wasReady && !dev.ready.value) frameBridge.reset()
  } else if (event.type === 'log' && event.log) {
    const next = [...logs.value, event.log]
    logs.value = next.length > LOG_LIMIT ? next.slice(next.length - LOG_LIMIT) : next
  } else if (event.type === 'job' && event.job) {
    tracker.update(event.job)
  } else if (event.type === 'registry.changed') {
    void git.load()
  }
}

// ---- delete ----
async function openDelete(componentId: string, requestId: string, bridge: Bridge) {
  if (remove.componentId.value) {
    bridge.reply(requestId, 'cancelled', { message: 'Another delete is already open.' })
    return
  }
  requestOrigin.value = { bridge, requestId }
  await remove.open(componentId, requestId)
}

function cancelDelete() {
  const origin = requestOrigin.value
  if (origin) origin.bridge.reply(origin.requestId, 'cancelled')
  requestOrigin.value = undefined
  remove.close()
}

async function confirmDelete() {
  const origin = requestOrigin.value
  const componentId = remove.componentId.value
  const job = await remove.confirm()
  if (!job || !componentId) return
  origin?.bridge.reply(origin.requestId, 'accepted', { componentId })
  requestOrigin.value = undefined
  remove.close()
  drawer.value = 'logs'
  const done = await tracker.track(job)
  if (done.status === 'succeeded') {
    const result = done.result as ComponentDeleteResult | undefined
    origin?.bridge.reply(origin.requestId, 'done', { componentId: result?.componentId ?? componentId })
    reloadEverywhere('component.deleted')
    void git.load()
  } else {
    const message = done.error?.message ?? `Delete ${done.status}.`
    origin?.bridge.reply(origin.requestId, 'failed', { message })
    notice.value = message
  }
}

// ---- create (agent) ----
const prompt = ref('')
const idempotencyKey = ref<string>()
const agentJobId = ref<string>()
const agentBusy = ref(false)
const agentError = ref('')
const agentJob = computed(() => agentJobId.value ? tracker.jobs.value[agentJobId.value] : undefined)

watch(prompt, () => { idempotencyKey.value = undefined })

function openAgent(hint: string, requestId?: string, bridge?: Bridge) {
  if (hint && !prompt.value.trim()) prompt.value = hint
  if (requestId && bridge) {
    requestOrigin.value = { bridge, requestId }
    bridge.reply(requestId, 'accepted')
  }
  drawer.value = 'agent'
}

async function generate() {
  agentBusy.value = true
  agentError.value = ''
  // Reuse the key on retry of the same prompt so the server de-duplicates.
  idempotencyKey.value ??= crypto.randomUUID()
  try {
    const job = await api.post<JobSnapshot>(
      `/workspaces/${encodeURIComponent(workspaceId)}/sites/${encodeURIComponent(siteId)}/components/generate`,
      { prompt: prompt.value.trim() },
      { headers: { 'Idempotency-Key': idempotencyKey.value } },
    )
    agentJobId.value = job.id
    const done = await tracker.track(job)
    const result = done.result as ComponentGenerateResult | undefined
    const origin = requestOrigin.value
    requestOrigin.value = undefined
    if (done.status === 'succeeded' && result?.componentId) {
      origin?.bridge.reply(origin.requestId, 'done', { componentId: result.componentId })
      idempotencyKey.value = undefined
      reloadEverywhere('component.generated', result.componentId)
      void git.load()
    } else {
      origin?.bridge.reply(origin.requestId, 'failed', { message: done.error?.message ?? `Generation ${done.status}.` })
    }
  } catch (cause) {
    agentError.value = cause instanceof Error ? cause.message : 'Could not start Codex.'
  } finally {
    agentBusy.value = false
  }
}

async function cancelAgent() {
  if (!agentJobId.value) return
  try {
    tracker.update(await api.post<JobSnapshot>(`/jobs/${encodeURIComponent(agentJobId.value)}/cancel`, {}))
  } catch (cause) {
    agentError.value = cause instanceof Error ? cause.message : 'Cancel failed.'
  }
}

// ---- git ----
const selectedRepo = ref<string>()
const committing = ref(false)

async function openGit() {
  drawer.value = 'git'
  await git.load()
  const first = git.repos.value.find((repo) => repo.dirty > 0) ?? git.repos.value[0]
  if (!selectedRepo.value && first) await selectRepo(first.id)
  else if (selectedRepo.value) await git.loadStatus(selectedRepo.value)
}

async function selectRepo(repoId: string) {
  selectedRepo.value = repoId
  await git.loadStatus(repoId)
}

async function commit(request: { message: string; paths: string[]; expectedHead: string }) {
  if (!selectedRepo.value) return
  committing.value = true
  try {
    const done = await tracker.track(await git.commit(selectedRepo.value, request))
    if (done.status !== 'succeeded') git.error.value = done.error?.message ?? `Commit ${done.status}.`
    else git.error.value = ''
  } catch (cause) {
    git.error.value = cause instanceof Error ? cause.message : 'Commit failed.'
  } finally {
    committing.value = false
    await git.load()
    await git.loadStatus(selectedRepo.value)
  }
}

// ---- lifecycle ----
const jobs = computed(() => Object.values(tracker.jobs.value))
const runningJobs = computed(() => jobs.value.filter((job) => !isTerminalJob(job)).length)
const memory = computed(() => {
  const bytes = dev.view.value?.memoryBytes
  return bytes ? `${Math.round(bytes / 1048576)} MiB` : ''
})
const statusLabel = computed(() => ({
  stopped: 'Stopped', starting: 'Starting', ready: 'Ready', stopping: 'Stopping', failed: 'Failed',
}[dev.status.value]))

onMounted(async () => {
  frameBridge.attach()
  tabBridge.attach()
  await dev.load()
  events.start()
  void git.load()
})

onBeforeUnmount(() => {
  events.stop()
  frameBridge.detach()
  tabBridge.detach()
  tracker.stop()
})
</script>

<template>
  <div class="dev-session">
    <header class="dev-bar">
      <NuxtLink class="dev-bar__back" :to="`/workspaces/${workspaceId}`" aria-label="Back to workspace">←</NuxtLink>
      <strong class="dev-bar__site">{{ siteId }}</strong>
      <span class="dev-pill" :data-status="dev.status.value" role="status">{{ statusLabel }}</span>
      <small v-if="memory" class="dev-bar__meta">{{ memory }}</small>
      <small v-if="frameBridge.connected.value" class="dev-bar__meta" title="Builder bridge connected">bridge ●</small>

      <div class="dev-bar__group">
        <button v-if="dev.status.value === 'stopped' || dev.status.value === 'failed'" class="dev-btn" type="button" :disabled="!!dev.busy.value" @click="dev.start()">Start</button>
        <button v-else class="dev-btn" type="button" :disabled="!!dev.busy.value || dev.status.value === 'stopping'" @click="dev.stop()">Stop</button>
        <button class="dev-btn" type="button" :disabled="!!dev.busy.value || dev.status.value === 'stopped'" @click="dev.restart()">Restart</button>
        <button class="dev-btn" type="button" :disabled="!dev.ready.value" @click="reloadEverywhere('manual')">Reload frame</button>
        <button class="dev-btn" type="button" :disabled="!frameSrc" @click="openInTab">Open in new tab</button>
      </div>

      <div class="dev-bar__group dev-bar__group--end">
        <button class="dev-btn dev-btn--accent" type="button" :disabled="!dev.ready.value" @click="openAgent('')">New component</button>
        <button class="dev-btn" type="button" :aria-pressed="drawer === 'git'" @click="drawer === 'git' ? (drawer = null) : openGit()">
          Git <span v-if="git.dirtyCount.value" class="dev-badge">{{ git.dirtyCount.value }}</span>
        </button>
        <button class="dev-btn" type="button" :aria-pressed="drawer === 'logs'" @click="drawer = drawer === 'logs' ? null : 'logs'">
          Activity <span v-if="runningJobs" class="dev-badge">{{ runningJobs }}</span>
        </button>
      </div>
    </header>

    <p v-if="dev.view.value?.needsRestart" class="dev-banner" role="alert">
      Needs a dev restart: {{ dev.view.value.needsRestart.reason }}
      <button class="dev-btn" type="button" :disabled="!!dev.busy.value" @click="dev.restart()">Restart now</button>
    </p>
    <p v-if="notice" class="dev-banner dev-banner--info" role="status">{{ notice }} <button class="dev-btn" type="button" @click="notice = ''">Dismiss</button></p>
    <p v-if="dev.error.value" class="dev-banner" role="alert">{{ dev.error.value }}</p>

    <main class="dev-stage">
      <iframe
        v-if="dev.ready.value && frameSrc"
        :key="frameKey"
        ref="iframe"
        class="dev-frame"
        :src="frameSrc"
        title="Site builder"
        allow="clipboard-read; clipboard-write"
      />
      <section v-else class="dev-state" :data-status="dev.status.value">
        <template v-if="dev.status.value === 'starting'">
          <span class="dev-spinner" aria-hidden="true" />
          <h1>Starting the dev server…</h1>
          <p>Nuxt compiles on first start; this can take a minute.</p>
        </template>
        <template v-else-if="dev.status.value === 'stopping'"><h1>Stopping…</h1></template>
        <template v-else-if="dev.status.value === 'failed'">
          <h1>The dev server failed.</h1>
          <p v-if="dev.view.value?.problem"><code>{{ dev.view.value.problem }}</code></p>
          <div class="button-row"><button class="compact-button compact-button--primary" type="button" @click="dev.restart()">Restart</button><button class="compact-button" type="button" @click="drawer = 'logs'">Show log</button></div>
        </template>
        <template v-else-if="dev.view.value">
          <h1>The dev server is stopped.</h1>
          <p>Start it to open the builder for <strong>{{ siteId }}</strong>.</p>
          <button class="compact-button compact-button--primary" type="button" :disabled="!!dev.busy.value" @click="dev.start()">Start dev server</button>
        </template>
        <p v-else class="empty-state">Connecting to the control plane…</p>
      </section>

      <DevAgentDrawer
        v-if="drawer === 'agent'"
        v-model:prompt="prompt"
        :job="agentJob"
        :busy="agentBusy"
        :error="agentError"
        @submit="generate"
        @cancel-job="cancelAgent"
        @commit="openGit"
        @close="drawer = null"
      />
      <DevGitDrawer
        v-else-if="drawer === 'git'"
        :repos="git.repos.value"
        :status="git.status.value"
        :diff="git.diff.value"
        :selected-repo="selectedRepo"
        :error="git.error.value"
        :committing="committing"
        @select-repo="selectRepo"
        @view-diff="(path) => selectedRepo && git.loadDiff(selectedRepo, path)"
        @commit="commit"
        @refresh="openGit"
        @close="drawer = null"
      />
      <DevLogsDrawer v-else-if="drawer === 'logs'" :logs="logs" :jobs="jobs" @close="drawer = null" />
    </main>

    <DevDeleteModal
      v-if="remove.componentId.value"
      v-model:acknowledged="remove.acknowledged.value"
      :component-id="remove.componentId.value"
      :usage="remove.usage.value"
      :loading="remove.loading.value"
      :submitting="remove.submitting.value"
      :error="remove.error.value"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </div>
</template>
