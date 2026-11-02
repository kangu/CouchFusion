<script setup lang="ts">
import { computed } from 'vue'
import type { JobSnapshot, LlmApplyResult, LlmRequestResult, SitePageList } from '../../types/api'
import { isTerminalJob } from '../../composables/useJobTracker'

/**
 * LLM Request: a prompt that may change app files and proposes page changes. Page changes are
 * shown for review and written only when the user clicks Apply (contract 2026-11-02).
 */
const props = defineProps<{
  prompt: string
  currentPath: string
  pages?: SitePageList | undefined
  pagesError: string
  model: string
  job?: JobSnapshot | undefined
  applyJob?: JobSnapshot | undefined
  result?: LlmRequestResult | undefined
  applyResult?: LlmApplyResult | undefined
  canSubmit: boolean
  canApply: boolean
  running: boolean
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  'update:prompt': [value: string]
  'update:currentPath': [value: string]
  submit: []
  apply: []
  discard: []
  cancelJob: []
  commit: []
  close: []
}>()

const events = computed(() => [
  ...(props.job?.events ?? []).map((event) => ({ ...event, key: `r${event.id}` })),
  ...(props.applyJob?.events ?? []).map((event) => ({ ...event, key: `a${event.id}` })),
].slice(-200))
const operations = computed(() => props.result?.operations ?? [])
const problems = computed(() => props.result?.check?.problems ?? [])
const previewByIndex = computed(() => new Map((props.result?.check?.preview ?? []).map((p) => [p.index, p])))
const requestRunning = computed(() => !!props.job && !isTerminalJob(props.job))
const applyRunning = computed(() => !!props.applyJob && !isTerminalJob(props.applyJob))
const pageOptions = computed(() => props.pages?.pages ?? [])
const knownPath = computed(() => !props.currentPath || pageOptions.value.some((p) => p.path === props.currentPath))

function eventClass(code: string, level?: string) {
  if (level === 'error' || code.endsWith('failed') || code === 'policy.dependency_change') return 'dev-events__row--error'
  if (level === 'warn' || code.startsWith('lint.')) return 'dev-events__row--warn'
  return ''
}

function tokens(value: number) {
  return value >= 1000 ? `${(value / 1000).toFixed(value >= 100000 ? 0 : 1)}k` : String(value)
}
</script>

<template>
  <aside class="dev-drawer dev-drawer--wide" aria-labelledby="dev-request-title" data-testid="llm-request">
    <header class="dev-drawer__header">
      <div><p class="eyebrow">Codex · {{ model }} · sandboxed to this app</p><h2 id="dev-request-title">LLM request</h2></div>
      <button class="dialog__close" type="button" aria-label="Close request drawer" @click="emit('close')">×</button>
    </header>

    <form class="dev-drawer__form" @submit.prevent="canSubmit && emit('submit')">
      <label for="dev-request-prompt">What should change?</label>
      <textarea
        id="dev-request-prompt"
        rows="6"
        maxlength="16384"
        :value="prompt"
        :disabled="running"
        placeholder="Make the hero on this page shorter and add a contact CTA · Create a /pricing page like /services · Replace /services with the content of /services-2026 · Set /old-offer to draft"
        @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
      />
      <label for="dev-request-page">Page in focus</label>
      <select id="dev-request-page" :value="currentPath" :disabled="running" @change="emit('update:currentPath', ($event.target as HTMLSelectElement).value)">
        <option value="">None</option>
        <option v-if="!knownPath" :value="currentPath">{{ currentPath }}</option>
        <option v-for="page in pageOptions" :key="page.path" :value="page.path">{{ page.path }}<template v-if="page.title"> · {{ page.title }}</template><template v-if="page.publicationState === 'draft'"> (draft)</template></option>
      </select>
      <p v-if="pagesError" class="notice">{{ pagesError }}</p>
      <p class="dev-drawer__hint">Codex may change files in this app right away (not dependencies). Page changes are only proposed: you review them below and nothing is written to the pages until you click Apply. Pages are shared with the live site.</p>
      <div class="button-row">
        <button class="compact-button compact-button--primary" type="submit" :disabled="!canSubmit">{{ busy && !job ? 'Starting…' : 'Send request' }}</button>
        <button v-if="requestRunning" class="compact-button compact-button--danger" type="button" @click="emit('cancelJob')">Cancel run</button>
      </div>
    </form>

    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

    <section v-if="job" class="dev-drawer__job">
      <p class="eyebrow">Request {{ job.status }}<template v-if="applyJob"> · apply {{ applyJob.status }}</template></p>
      <ol class="dev-events">
        <li v-for="event in events" :key="event.key" :class="eventClass(event.code, event.level)"><span>{{ event.code }}</span>{{ event.text }}</li>
      </ol>
      <p v-if="job.error" class="notice notice--error">{{ job.error.message }}</p>

      <template v-if="result">
        <p v-if="result.summary" class="dev-request__summary" data-testid="request-summary">{{ result.summary }}</p>

        <template v-if="result.changedFiles.length">
          <p class="eyebrow">Files changed ({{ result.changedFiles.length }})</p>
          <ul class="dev-drawer__files"><li v-for="file in result.changedFiles" :key="file"><code>{{ file }}</code></li></ul>
          <p v-for="warning in result.lint ?? []" :key="warning" class="notice">{{ warning }}</p>
          <p v-if="result.needsRestart" class="notice">Needs a dev restart: {{ result.needsRestart.reason }}</p>
        </template>

        <template v-if="operations.length">
          <p class="eyebrow">Proposed page changes ({{ operations.length }})</p>
          <ol class="dev-request__ops" data-testid="request-operations">
            <li v-for="(op, index) in operations" :key="index">
              <strong>{{ op.text }}</strong>
              <dl v-if="previewByIndex.get(index)?.changes.length">
                <template v-for="change in previewByIndex.get(index)!.changes" :key="change.field">
                  <dt>{{ change.field }}</dt>
                  <dd><span class="dev-request__before">{{ change.before ?? '—' }}</span> → <span class="dev-request__after">{{ change.after ?? '—' }}</span></dd>
                </template>
              </dl>
            </li>
          </ol>
          <ul v-if="problems.length" class="dev-request__problems" role="alert">
            <li v-for="(problem, index) in problems" :key="index"><template v-if="problem.index !== null">Change {{ problem.index + 1 }}: </template>{{ problem.message }}</li>
          </ul>
        </template>
        <p v-else-if="!result.changedFiles.length" class="notice">Nothing was changed or proposed.</p>

        <p v-if="applyJob?.status === 'succeeded'" class="notice notice--success" role="status">Applied {{ applyResult?.applied.length ?? operations.length }} page change(s). The builder reloads; page history has the previous versions.</p>
        <p v-else-if="applyJob?.error" class="notice notice--error" role="alert">{{ applyJob.error.message }}</p>

        <div class="button-row">
          <button v-if="operations.length" class="compact-button compact-button--primary" type="button" :disabled="!canApply" data-testid="request-apply" @click="emit('apply')">
            {{ applyRunning ? 'Applying…' : `Apply ${operations.length} page change${operations.length === 1 ? '' : 's'}` }}
          </button>
          <button v-if="operations.length && !applyJob" class="compact-button" type="button" :disabled="running" @click="emit('discard')">Discard</button>
          <button v-if="result.changedFiles.length" class="compact-button" type="button" @click="emit('commit')">Commit files…</button>
        </div>
      </template>

      <p v-if="result?.usage" class="dev-drawer__usage">
        Tokens: {{ tokens(result.usage.inputTokens) }} in<template v-if="result.usage.cachedInputTokens"> ({{ tokens(result.usage.cachedInputTokens) }} cached)</template> · {{ tokens(result.usage.outputTokens) }} out<template v-if="result.model"> · {{ result.model }}</template>
      </p>
    </section>
  </aside>
</template>
