<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import '../assets/css/services.css'
import { createServices, formatBytes, serviceTone } from '../composables/useServices'

/** Read-only status and recent logs of the server's systemd units (phase 2 contract §4). */
const bootstrap = useBootstrap()
const state = createServices(useApi())
const logPre = ref<HTMLElement>()
const available = computed(() => bootstrap.has('services'))

function since(value: string | undefined) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function time(value: string) {
  return value ? new Date(value).toLocaleTimeString() : ''
}

watch(() => state.logs.value.length, async () => {
  await nextTick()
  if (logPre.value) logPre.value.scrollTop = logPre.value.scrollHeight
})

onMounted(async () => {
  await bootstrap.load()
  if (available.value) state.start()
})

onBeforeUnmount(() => state.stop())
</script>

<template>
  <section class="page-sheet">
    <header class="page-header">
      <div>
        <p class="eyebrow">Server · read only</p>
        <h1>Services</h1>
        <p>systemd units on this server. Refreshes every 10 seconds; starting and stopping production arrives with deploys.</p>
      </div>
      <button class="compact-button" type="button" :disabled="!available || state.loading.value" @click="state.load()">Refresh</button>
    </header>

    <div v-if="!available" class="empty-state">
      <h2>Not available.</h2>
      <p>This control plane does not report services.</p>
    </div>
    <template v-else>
      <p v-if="state.error.value" class="notice notice--error" role="alert">{{ state.error.value }}</p>
      <p v-if="state.lastLoaded.value" class="dialog__note">
        Updated {{ state.lastLoaded.value.toLocaleTimeString() }}<template v-if="state.failing.value"> · {{ state.failing.value }} not running</template>
      </p>

      <table class="services-table" data-testid="services">
        <thead><tr><th>Unit</th><th>State</th><th>Since</th><th>Memory</th><th>User</th><th /></tr></thead>
        <tbody>
          <tr v-for="service in state.services.value" :key="service.unit" :data-tone="serviceTone(service)" :aria-selected="state.selected.value === service.unit">
            <td><strong><code>{{ service.unit }}</code></strong><small>{{ service.description }}</small></td>
            <td><span class="services-state" :data-tone="serviceTone(service)">{{ service.activeState }}<template v-if="service.subState"> · {{ service.subState }}</template></span></td>
            <td>{{ since(service.since) }}</td>
            <td>{{ formatBytes(service.memoryCurrentBytes) }}</td>
            <td>{{ service.user ?? '' }}</td>
            <td><button class="compact-button" type="button" @click="state.loadLogs(service.unit)">Logs</button></td>
          </tr>
          <tr v-if="!state.services.value.length && !state.loading.value"><td colspan="6" class="empty-state">No services reported.</td></tr>
        </tbody>
      </table>

      <section v-if="state.selected.value" class="log-viewer services-logs" aria-live="polite">
        <header>
          <span>{{ state.selected.value }}</span>
          <small>{{ state.logs.value.length }} lines</small>
          <button class="compact-button" type="button" :disabled="state.logsLoading.value" @click="state.loadLogs()">{{ state.logsLoading.value ? 'Loading…' : 'Refresh' }}</button>
          <button class="compact-button" type="button" aria-label="Close logs" @click="state.closeLogs()">×</button>
        </header>
        <p v-if="state.logsError.value" class="notice notice--error" role="alert">{{ state.logsError.value }}</p>
        <pre ref="logPre"><code v-for="line in state.logs.value" :key="line.id" :class="{ 'dev-log--err': line.stream === 'stderr' || line.level === 'error' }"><span>{{ time(line.time) }} </span>{{ line.text }}
</code></pre>
      </section>
    </template>
  </section>
</template>
