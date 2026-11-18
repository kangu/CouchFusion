<script setup lang="ts">
import '../../assets/css/services.css'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { createProduction, driftLabel, driftParts, shortSha } from '../../composables/useProduction'
import { formatBytes, serviceTone } from '../../composables/useServices'

/** Production of one site: live release, deploy, rollback (Phase 3, spec §7.2). */
const props = defineProps<{ workspaceId: string, siteId: string, collapseHistory?: boolean }>()

const prod = createProduction(useApi(), props.workspaceId, props.siteId)
const confirming = ref<{ kind: 'deploy' } | { kind: 'rollback', releaseId: string }>()
const confirmText = ref('')
const logPre = ref<HTMLElement>()

const view = computed(() => prod.view.value)
const job = computed(() => prod.job.value)
const ready = computed(() => confirmText.value === props.siteId && !prod.busy.value)
const drift = computed(() => view.value?.dev ?? [])
const devChanged = computed(() => drift.value.filter((d) => d.changed))
const jobTitle = computed(() => job.value?.type === 'release.rollback' ? 'Rollback' : 'Deploy')
const jobResult = computed(() => (job.value?.result ?? {}) as { release?: string, rolledBackTo?: string, buildSeconds?: number })

function ask(kind: 'deploy'): void
function ask(kind: 'rollback', releaseId: string): void
function ask(kind: 'deploy' | 'rollback', releaseId?: string) {
  confirming.value = kind === 'deploy' ? { kind } : { kind, releaseId: releaseId! }
  confirmText.value = ''
}

async function submit() {
  const target = confirming.value
  if (!target || !ready.value) return
  const started = target.kind === 'deploy'
    ? await prod.deploy(confirmText.value)
    : await prod.rollback(target.releaseId, confirmText.value)
  if (started) confirming.value = undefined
}

function when(value: string | undefined) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function time(value: string) {
  return value ? new Date(value).toLocaleTimeString() : ''
}

watch(() => job.value?.events.length, async () => {
  await nextTick()
  if (logPre.value) logPre.value.scrollTop = logPre.value.scrollHeight
})

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void prod.load()
  // Keep the service state fresh; job progress has its own 2 s poll.
  timer = setInterval(() => { if (!prod.running.value) void prod.load() }, 15_000)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  prod.stop()
})
</script>

<template>
  <section class="prod-panel" data-testid="production" aria-labelledby="prod-title">
    <header class="prod-panel__header">
      <div>
        <p class="eyebrow">Production</p>
        <h2 id="prod-title">{{ siteId }}</h2>
        <p v-if="view">
          <span class="services-state" :data-tone="serviceTone(view.service)">{{ view.service.activeState }}<template v-if="view.service.subState"> · {{ view.service.subState }}</template></span>
          <code>{{ view.unit }}</code> on 127.0.0.1:{{ view.port }}<template v-if="view.service.memoryCurrentBytes"> · {{ formatBytes(view.service.memoryCurrentBytes) }}</template>
          <template v-if="view.service.startsAtBoot"> · starts at boot</template>
          <template v-if="view.previewUrl"> · <a :href="view.previewUrl" target="_blank" rel="noreferrer" data-testid="prod-preview-link">Preview ↗</a></template>
        </p>
      </div>
      <div class="prod-panel__deploy">
        <div class="prod-panel__actions">
          <button class="compact-button" type="button" :disabled="prod.loading.value" @click="prod.load()">Refresh</button>
          <button class="compact-button compact-button--primary" type="button" :disabled="!view || prod.busy.value || !view.admission.ok" data-testid="deploy" @click="ask('deploy')">Deploy…</button>
        </div>
        <div v-if="drift.length" class="prod-drift" :data-changed="devChanged.length > 0" data-testid="prod-drift" role="status">
          <template v-if="devChanged.length">
            <strong>Dev has changes that are not live</strong>
            <ul>
              <li v-for="d in devChanged" :key="d.repo">
                <span>{{ driftLabel(d.repo) }}: {{ driftParts(d).join(' · ') || 'differs from the live build' }}</span>
                <details v-if="d.changedFiles.length">
                  <summary>Files</summary>
                  <code v-for="file in d.changedFiles" :key="file">{{ file }}</code>
                  <small v-if="d.changedFileCount > d.changedFiles.length">and {{ d.changedFileCount - d.changedFiles.length }} more</small>
                </details>
              </li>
            </ul>
          </template>
          <template v-else>
            <strong>Dev matches the live release</strong>
            <span>Deploying now would build the same app and layers.</span>
          </template>
        </div>
      </div>
    </header>

    <p v-if="prod.error.value" class="notice notice--error" role="alert">{{ prod.error.value }}</p>
    <p v-if="view && view.service.startsAtBoot === false" class="notice notice--error" role="alert" data-testid="prod-not-at-boot">Production does not start at boot. On the server run <code>systemctl enable {{ view.unit }}</code> (phase3/03-units.sh does this).</p>
    <p v-if="view && !view.admission.ok" class="notice notice--error" role="alert">Can't build now: {{ view.admission.problem }}.</p>
    <p v-if="prod.blockedByOther.value" class="notice" role="status">Another job is running for this site; deploy and rollback wait until it ends.</p>

    <form v-if="confirming" class="prod-panel__confirm" data-testid="prod-confirm" @submit.prevent="submit">
      <p v-if="confirming.kind === 'deploy'">
        Builds the workspace <strong>as it is now</strong> (uncommitted changes included) and makes it live. If the new release fails its health check, the current one comes back on its own.
      </p>
      <p v-else>Makes <code>{{ confirming.releaseId }}</code> live again (no build).</p>
      <label for="prod-confirm-input">Type <code>{{ siteId }}</code> to confirm</label>
      <div class="prod-panel__confirm-row">
        <input id="prod-confirm-input" v-model="confirmText" autocomplete="off" spellcheck="false">
        <button class="compact-button compact-button--primary" type="submit" :disabled="!ready">{{ confirming.kind === 'deploy' ? 'Deploy' : 'Roll back' }}</button>
        <button class="compact-button" type="button" @click="confirming = undefined">Cancel</button>
      </div>
    </form>
    <p v-if="prod.actionError.value" class="notice notice--error" role="alert">{{ prod.actionError.value }}</p>

    <section v-if="job" class="log-viewer prod-panel__job" aria-live="polite" data-testid="prod-job">
      <header>
        <span>{{ jobTitle }} · {{ job.status }}</span>
        <small v-if="jobResult.release">{{ jobResult.release }}<template v-if="jobResult.buildSeconds !== undefined"> · build {{ jobResult.buildSeconds }} s</template></small>
      </header>
      <pre ref="logPre"><code v-for="event in job.events" :key="event.id" :class="{ 'dev-log--err': event.level === 'error' || event.level === 'warn' }"><span>{{ time(event.time) }} </span>{{ event.text }}
</code></pre>
      <p v-if="job.error" class="notice notice--error" role="alert">{{ job.error.message }}</p>
      <p v-else-if="job.status === 'succeeded' && jobResult.release" class="notice" role="status">{{ jobResult.release }} is live.</p>
    </section>

    <details v-if="view" class="prod-history" :open="!collapseHistory">
      <summary>Release history <small>{{ view.releases.length }}</small></summary>
      <table class="services-table prod-panel__releases" data-testid="releases">
        <thead><tr><th>Release</th><th>Built</th><th>App</th><th>Layers</th><th>State / action</th></tr></thead>
        <tbody>
          <tr v-for="release in view.releases" :key="release.id" :aria-selected="release.current">
            <td><code>{{ release.id }}</code></td>
            <td>{{ when(release.createdAt) }}</td>
            <td><code>{{ shortSha(release.appHead) || '?' }}</code><small v-if="release.appDirty">uncommitted changes</small></td>
            <td><code>{{ shortSha(release.layersHead) || '?' }}</code><small v-if="release.layersDirty">uncommitted changes</small></td>
            <td>
              <span v-if="release.current" class="services-state" data-tone="ok">live</span>
              <button v-else class="compact-button" type="button" :disabled="prod.busy.value" @click="ask('rollback', release.id)" :aria-label="`Roll back to ${release.id}`">Roll back…</button>
            </td>
          </tr>
          <tr v-if="!view.releases.length"><td colspan="5" class="empty-state">No releases yet.</td></tr>
        </tbody>
      </table>
    </details>
  </section>
</template>
