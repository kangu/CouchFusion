<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { ActivityEvent, JobSnapshot } from '../../types/api'

const props = defineProps<{
  logs: ActivityEvent[]
  jobs: JobSnapshot[]
}>()

const emit = defineEmits<{ close: [] }>()

const tab = ref<'logs' | 'jobs'>('logs')
const pre = ref<HTMLElement>()
const follow = ref(true)
const sortedJobs = computed(() => [...props.jobs].sort((a, b) => b.createdAt.localeCompare(a.createdAt)))

watch(() => props.logs.length, async () => {
  if (!follow.value || tab.value !== 'logs') return
  await nextTick()
  if (pre.value) pre.value.scrollTop = pre.value.scrollHeight
})

function time(value: string) {
  return value ? new Date(value).toLocaleTimeString() : ''
}
</script>

<template>
  <aside class="dev-drawer dev-drawer--wide" aria-labelledby="dev-logs-title">
    <header class="dev-drawer__header">
      <div><p class="eyebrow">Dev server and jobs</p><h2 id="dev-logs-title">Activity</h2></div>
      <button class="dialog__close" type="button" aria-label="Close activity drawer" @click="emit('close')">×</button>
    </header>
    <div class="button-row dev-tabs" role="tablist">
      <button class="compact-button" :class="{ 'compact-button--primary': tab === 'logs' }" role="tab" type="button" :aria-selected="tab === 'logs'" @click="tab = 'logs'">Dev log</button>
      <button class="compact-button" :class="{ 'compact-button--primary': tab === 'jobs' }" role="tab" type="button" :aria-selected="tab === 'jobs'" @click="tab = 'jobs'">Jobs ({{ jobs.length }})</button>
      <label v-if="tab === 'logs'" class="dev-tabs__follow"><input v-model="follow" type="checkbox"> Follow</label>
    </div>

    <section v-if="tab === 'logs'" class="log-viewer">
      <header><span>cf-dev</span><small>{{ logs.length }} lines</small></header>
      <pre ref="pre"><code v-for="line in logs" :key="line.id" :class="{ 'dev-log--err': line.stream === 'stderr' }"><span>{{ time(line.time) }} </span>{{ line.text }}
</code></pre>
    </section>

    <section v-else class="dev-jobs">
      <p v-if="!jobs.length" class="empty-state">No jobs started from this page yet.</p>
      <details v-for="job in sortedJobs" :key="job.id" class="dev-jobs__job">
        <summary><strong>{{ job.type }}</strong> <span class="status-lozenge">{{ job.status }}</span> <small>{{ time(job.createdAt) }}</small></summary>
        <ol class="dev-events"><li v-for="event in job.events" :key="event.id"><span>{{ event.code }}</span>{{ event.text }}</li></ol>
        <p v-if="job.error" class="notice notice--error">{{ job.error.message }}</p>
      </details>
    </section>
  </aside>
</template>
