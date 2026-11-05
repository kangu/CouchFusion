<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { createDevSession } from '../../composables/useDevSession'
import { formatBytes } from '../../composables/useServices'
import type { DevSessionView } from '../../types/api'

/**
 * Start / stop / restart the site's dev server from the site page. Dev never starts at boot;
 * production (cf-site@) is a separate unit and keeps running either way.
 */
const props = defineProps<{ workspaceId: string; siteId: string }>()
const emit = defineEmits<{ update: [view: DevSessionView] }>()

const dev = createDevSession(useApi(), props.workspaceId, props.siteId)
const confirmingStop = ref(false)

const status = computed(() => dev.status.value)
const label = computed(() => ({ ready: 'Running', starting: 'Starting…', stopping: 'Stopping…', stopped: 'Stopped', failed: 'Failed' } as Record<string, string>)[status.value] ?? status.value)
const running = computed(() => status.value === 'ready' || status.value === 'starting')
const transient = computed(() => status.value === 'starting' || status.value === 'stopping' || !!dev.busy.value)
const since = computed(() => {
  const at = dev.view.value?.startedAt
  if (!at) return ''
  const date = new Date(at)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
})
const detail = computed(() => {
  const view = dev.view.value
  if (status.value === 'ready') return [since.value && `Since ${since.value}`, view?.memoryBytes && formatBytes(view.memoryBytes)].filter(Boolean).join(' · ')
  if (status.value === 'starting') return 'Nuxt compiles on start, usually 20–70 s.'
  if (status.value === 'failed') return view?.problem ?? 'The dev server stopped unexpectedly.'
  if (status.value === 'stopped') return 'Start it to use the builder, New component and LLM request.'
  return ''
})

watch(() => dev.view.value, (view) => { if (view) emit('update', view) })

async function stop() {
  confirmingStop.value = false
  await dev.stop()
}

let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false
function schedule() {
  if (stopped) return
  const mine = setTimeout(async () => {
    await dev.load()
    // A status change may have rescheduled meanwhile; only the latest timer continues.
    if (timer === mine) schedule()
  }, transient.value ? 2000 : 15000)
  timer = mine
}
// Poll fast while the server is starting or stopping, slowly otherwise.
watch(transient, () => {
  if (timer) clearTimeout(timer)
  schedule()
})
onMounted(async () => {
  await dev.load()
  schedule()
})
onBeforeUnmount(() => {
  stopped = true
  if (timer) clearTimeout(timer)
})
</script>

<template>
  <section class="panel dev-card" data-testid="dev-card" :data-status="status">
    <p class="eyebrow">Dev server</p>
    <h2 class="dev-card__state"><span class="dev-card__dot" aria-hidden="true" />{{ label }}</h2>
    <p v-if="detail" class="site-rust__meta" data-testid="dev-card-detail">{{ detail }}</p>
    <p v-if="dev.view.value?.needsRestart && running" class="notice">Needs a restart: {{ dev.view.value.needsRestart.reason }}</p>

    <div v-if="!confirmingStop" class="button-row">
      <button v-if="!running && status !== 'stopping'" class="compact-button compact-button--primary" type="button" :disabled="!!dev.busy.value" data-testid="dev-card-start" @click="dev.start()">
        {{ dev.busy.value === 'start' ? 'Starting…' : 'Start dev server' }}
      </button>
      <template v-if="running">
        <button class="compact-button" type="button" :disabled="!!dev.busy.value" data-testid="dev-card-stop" @click="confirmingStop = true">Stop…</button>
        <button class="compact-button" type="button" :disabled="!!dev.busy.value || status === 'starting'" data-testid="dev-card-restart" @click="dev.restart()">
          {{ dev.busy.value === 'restart' ? 'Restarting…' : 'Restart' }}
        </button>
      </template>
    </div>
    <div v-else class="dev-card__confirm" role="alertdialog" aria-labelledby="dev-card-confirm-text">
      <p id="dev-card-confirm-text">Stop the dev server? The builder and dev site go offline until you start it again; unsaved builder edits are lost. Production keeps running.</p>
      <div class="button-row">
        <button class="compact-button compact-button--danger" type="button" data-testid="dev-card-stop-confirm" @click="stop">Stop dev server</button>
        <button class="compact-button" type="button" @click="confirmingStop = false">Cancel</button>
      </div>
    </div>
    <p v-if="dev.error.value" class="notice notice--error" role="alert">{{ dev.error.value }}</p>

    <slot />
    <p class="site-rust__meta">Dev runs only when you start it (never at boot). Production is separate and always runs.</p>
  </section>
</template>
