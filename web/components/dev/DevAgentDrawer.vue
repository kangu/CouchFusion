<script setup lang="ts">
import { computed } from 'vue'
import type { ComponentGenerateResult, JobSnapshot } from '../../types/api'
import { isTerminalJob } from '../../composables/useJobTracker'

const props = defineProps<{
  prompt: string
  job?: JobSnapshot | undefined
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  'update:prompt': [value: string]
  submit: []
  cancelJob: []
  commit: []
  close: []
}>()

const running = computed(() => !!props.job && !isTerminalJob(props.job))
const result = computed(() => props.job?.result as ComponentGenerateResult | undefined)
const canSubmit = computed(() => props.prompt.trim().length > 0 && !props.busy && !running.value)
const events = computed(() => (props.job?.events ?? []).slice(-200))

function eventClass(code: string, level?: string) {
  if (level === 'error' || code.endsWith('failed') || code === 'policy.dependency_change') return 'dev-events__row--error'
  if (level === 'warn' || code.startsWith('lint.')) return 'dev-events__row--warn'
  return ''
}
</script>

<template>
  <aside class="dev-drawer" aria-labelledby="dev-agent-title">
    <header class="dev-drawer__header">
      <div><p class="eyebrow">Codex · sandboxed to this app</p><h2 id="dev-agent-title">New component</h2></div>
      <button class="dialog__close" type="button" aria-label="Close agent drawer" @click="emit('close')">×</button>
    </header>

    <form class="dev-drawer__form" @submit.prevent="canSubmit && emit('submit')">
      <label for="dev-agent-prompt">Describe the component</label>
      <textarea
        id="dev-agent-prompt"
        rows="6"
        maxlength="16384"
        :value="prompt"
        :disabled="running"
        placeholder="A pricing table with three tiers, a highlighted middle plan and a CTA per tier…"
        @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
      />
      <div class="button-row">
        <button class="compact-button compact-button--primary" type="submit" :disabled="!canSubmit">{{ busy ? 'Starting…' : 'Generate' }}</button>
        <button v-if="running" class="compact-button compact-button--danger" type="button" @click="emit('cancelJob')">Cancel run</button>
      </div>
    </form>

    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

    <section v-if="job" class="dev-drawer__job">
      <p class="eyebrow">Run {{ job.status }}</p>
      <ol class="dev-events">
        <li v-for="event in events" :key="event.id" :class="eventClass(event.code, event.level)"><span>{{ event.code }}</span>{{ event.text }}</li>
      </ol>
      <p v-if="job.error" class="notice notice--error">{{ job.error.message }}</p>
      <template v-if="job.status === 'succeeded' && result">
        <p v-if="result.componentId" class="notice notice--success">Created <code>{{ result.componentId }}</code>. The builder reloads with it highlighted.</p>
        <p v-else class="notice notice--error">The run finished without a new content component.</p>
        <ul v-if="result.changedFiles.length" class="dev-drawer__files"><li v-for="file in result.changedFiles" :key="file"><code>{{ file }}</code></li></ul>
        <p v-for="warning in result.lint" :key="warning" class="notice">{{ warning }}</p>
        <p v-if="result.needsRestart" class="notice">Needs a dev restart: {{ result.needsRestart.reason }}</p>
        <button class="compact-button" type="button" @click="emit('commit')">Commit…</button>
      </template>
    </section>
  </aside>
</template>
