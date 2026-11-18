<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { JobSnapshot, LlmApplyResult, LlmProposalState, LlmRequestResult, LlmRequestSummary, SitePageList } from '../../types/api'
import { isTerminalJob } from '../../composables/useJobTracker'
import { formatWhen, historyOutcome, historyStatus } from '../../composables/useLlmRequest'
import type { LogEntry } from '../../composables/useTypedLog'

/**
 * LLM Request: a prompt that may change app files and proposes page changes. Page changes are
 * shown for review and written only when the user clicks Apply (contract 2026-11-02). Finished
 * requests are listed under History; New task clears the finished one for the next prompt.
 */
const props = defineProps<{
  followUp?: boolean
  prompt: string
  currentPath: string
  pages?: SitePageList | undefined
  pagesError: string
  model: string
  view: 'task' | 'history'
  job?: JobSnapshot | undefined
  applyJob?: JobSnapshot | undefined
  result?: LlmRequestResult | undefined
  applyResult?: LlmApplyResult | undefined
  submittedPrompt: string
  proposal: LlmProposalState
  history?: LlmRequestSummary[] | undefined
  historyError: string
  canSubmit: boolean
  canApply: boolean
  canStartNew: boolean
  running: boolean
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  'update:prompt': [value: string]
  'update:currentPath': [value: string]
  'update:view': [value: 'task' | 'history']
  followUp: []
  submit: [screenshot?: { data: string; key: string }]
  apply: []
  newTask: [reuse: boolean]
  open: [id: string]
  cancelJob: []
  commit: []
  close: []
}>()

const events = computed<LogEntry[]>(() => [
  ...(props.job?.events ?? []).map((event) => ({ ...event, key: `${props.job!.id}:${event.id}` })),
  ...(props.applyJob?.events ?? []).map((event) => ({ ...event, key: `${props.applyJob!.id}:${event.id}` })),
].slice(-200).map((event) => ({ key: event.key, code: event.code, text: event.text, className: eventClass(event.code, event.level) })))
const operations = computed(() => props.result?.operations ?? [])
const problems = computed(() => props.result?.check?.problems ?? [])
const previewByIndex = computed(() => new Map((props.result?.check?.preview ?? []).map((p) => [p.index, p])))
const requestRunning = computed(() => !!props.job && !isTerminalJob(props.job))
const applyRunning = computed(() => !!props.applyJob && !isTerminalJob(props.applyJob))
const pageOptions = computed(() => props.pages?.pages ?? [])
const knownPath = computed(() => !props.currentPath || pageOptions.value.some((p) => p.path === props.currentPath))
const taskPath = computed(() => props.result?.currentPath ?? props.history?.find((item) => item.id === props.job?.id)?.currentPath ?? '')
const finished = computed(() => !!props.job && !props.running)

const reading = ref(false)
const imageNotice = ref('')
let generation = 0
const screenshot = ref<{ data: string; key: string; name: string }>()
function resetImage() {
  screenshot.value = undefined
  generation++
  reading.value = false
  imageNotice.value = ''
}
watch(() => [props.job?.id, props.view, props.followUp], resetImage)
onBeforeUnmount(resetImage)

async function readScreenshot(files?: FileList | null) {
  if (reading.value || props.busy || !files?.length) return
  const file = files[0]!
  if (files.length !== 1 || !['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 10 * 1024 * 1024) {
    imageNotice.value = 'Choose one PNG, JPEG or WebP image under 10 MiB.'
    return
  }
  const token = ++generation
  reading.value = true
  imageNotice.value = 'Reading screenshot…'
  try {
    const data = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result))
      reader.onerror = () => reject(reader.error)
      reader.readAsDataURL(file)
    })
    if (token !== generation) return
    screenshot.value = { data, key: crypto.randomUUID(), name: file.name }
    imageNotice.value = ''
  } catch {
    if (token === generation) imageNotice.value = 'Could not read the screenshot. Try another image.'
  } finally {
    if (token === generation) reading.value = false
  }
}

function chooseScreenshot(event: Event) {
  const input = event.target as HTMLInputElement
  void readScreenshot(input.files)
  input.value = ''
}

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

    <div class="button-row dev-tabs" role="tablist">
      <button class="compact-button" :class="{ 'compact-button--primary': view === 'task' }" role="tab" type="button" :aria-selected="view === 'task'" data-testid="request-tab-task" @click="emit('update:view', 'task')">
        {{ job ? (running ? 'Running task' : 'Task') : 'New task' }}
      </button>
      <button class="compact-button" :class="{ 'compact-button--primary': view === 'history' }" role="tab" type="button" :aria-selected="view === 'history'" data-testid="request-tab-history" @click="emit('update:view', 'history')">
        History<template v-if="history?.length"> ({{ history.length }})</template>
      </button>
      <button v-if="view === 'task' && finished" class="compact-button dev-tabs__end" type="button" :disabled="!canStartNew" data-testid="request-new-task" @click="emit('newTask', false)">+ New task</button>
    </div>

    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

    <template v-if="view === 'history'">
      <p v-if="historyError" class="notice notice--error" role="alert">{{ historyError }}</p>
      <p v-else-if="history && !history.length" class="dev-drawer__hint">No requests yet. Each request is listed here once you send it.</p>
      <ol v-if="history?.length" class="dev-request__history" data-testid="request-history">
        <li v-for="item in history" :key="item.id">
          <button
            type="button"
            class="dev-request__history-item"
            :aria-current="item.id === job?.id ? 'true' : undefined"
            :disabled="running && item.id !== job?.id"
            @click="emit('open', item.id)"
          >
            <span class="dev-request__history-top">
              <span class="dev-request__status" :class="`dev-request__status--${item.status}`">{{ historyStatus(item.status) }}</span>
              <time :datetime="item.createdAt">{{ formatWhen(item.createdAt) }}</time>
              <code v-if="item.currentPath">{{ item.currentPath }}</code>
            </span>
            <span class="dev-request__history-prompt" :class="{ 'dev-request__history-prompt--missing': !item.prompt }">{{ item.prompt || 'Prompt not recorded' }}</span>
            <span v-if="historyOutcome(item)" class="dev-request__history-outcome">{{ historyOutcome(item) }}</span>
          </button>
        </li>
      </ol>
    </template>

    <form v-else-if="!job" class="dev-drawer__form" @dragover.prevent @drop.prevent="readScreenshot($event.dataTransfer?.files)" @submit.prevent="!reading && canSubmit && emit('submit', screenshot)">
      <label for="dev-request-prompt">{{ followUp ? 'Follow-up message' : 'What should change?' }}</label>
      <textarea
        id="dev-request-prompt"
        rows="6"
        maxlength="16384"
        :value="prompt"
        :disabled="running"
        placeholder="Make the hero on this page shorter and add a contact CTA · Create a /pricing page like /services · Replace /services with the content of /services-2026 · Set /old-offer to draft"
        @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
      />
      <label for="dev-request-screenshot">Attach a screenshot or drop it here</label>
      <input id="dev-request-screenshot" type="file" accept="image/png,image/jpeg,image/webp" :disabled="reading || busy" @change="chooseScreenshot">
      <div v-if="screenshot" class="button-row">
        <img :src="screenshot.data" alt="Attached screenshot" width="96" style="max-height: 96px; object-fit: contain">
        <span>{{ screenshot.name }}</span>
        <button type="button" class="compact-button" :disabled="busy" @click="resetImage">Remove</button>
      </div>
      <p v-if="imageNotice" class="dev-drawer__hint" role="status">{{ imageNotice }}</p>
      <label for="dev-request-page">Page in focus</label>
      <select id="dev-request-page" :value="currentPath" :disabled="running" @change="emit('update:currentPath', ($event.target as HTMLSelectElement).value)">
        <option value="">None</option>
        <option v-if="!knownPath" :value="currentPath">{{ currentPath }}</option>
        <option v-for="page in pageOptions" :key="page.path" :value="page.path">{{ page.path }}<template v-if="page.title"> · {{ page.title }}</template><template v-if="page.publicationState === 'draft'"> (draft)</template></option>
      </select>
      <p v-if="pagesError" class="notice">{{ pagesError }}</p>
      <p class="dev-drawer__hint">Codex may change files in this app right away (not dependencies). Page changes are only proposed: you review them below and nothing is written to the pages until you click Apply. Pages are shared with the live site.</p>
      <div class="button-row">
        <button class="compact-button compact-button--primary" type="submit" :disabled="reading || !canSubmit">{{ busy ? 'Starting…' : 'Send request' }}</button>
      </div>
    </form>

    <section v-else class="dev-drawer__job">
      <div class="dev-request__asked" data-testid="request-asked">
        <p class="eyebrow">
          Request {{ job.status }}<template v-if="applyJob"> · apply {{ applyJob.status }}</template>
          · {{ formatWhen(job.createdAt) }}<template v-if="taskPath"> · {{ taskPath }}</template>
        </p>
        <p class="dev-request__prompt" :class="{ 'dev-request__history-prompt--missing': !submittedPrompt }">{{ submittedPrompt || 'Prompt not recorded' }}</p>
      </div>
      <div v-if="requestRunning" class="button-row">
        <button class="compact-button compact-button--danger" type="button" @click="emit('cancelJob')">Cancel run</button>
      </div>
      <DevEventLog :key="job.id" :entries="events" />
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
        <p v-else-if="proposal === 'applied'" class="notice notice--success" role="status">These page changes were applied.</p>
        <p v-else-if="proposal === 'partial'" class="notice notice--error">These page changes were only partly applied and cannot be applied again.</p>

        <div class="button-row">
          <button v-if="operations.length && proposal === 'pending'" class="compact-button compact-button--primary" type="button" :disabled="!canApply" data-testid="request-apply" @click="emit('apply')">
            {{ applyRunning ? 'Applying…' : `Apply ${operations.length} page change${operations.length === 1 ? '' : 's'}` }}
          </button>
          <button v-if="result.changedFiles.length" class="compact-button" type="button" @click="emit('commit')">Commit files…</button>
        </div>
      </template>

      <p v-if="result?.usage" class="dev-drawer__usage">
        Tokens: {{ tokens(result.usage.inputTokens) }} in<template v-if="result.usage.cachedInputTokens"> ({{ tokens(result.usage.cachedInputTokens) }} cached)</template> · {{ tokens(result.usage.outputTokens) }} out<template v-if="result.model"> · {{ result.model }}</template>
      </p>

      <div v-if="finished" class="button-row dev-request__next">
        <button v-if="submittedPrompt" class="compact-button" type="button" :disabled="!canStartNew" @click="emit('followUp')">Send a follow-up</button>
        <button class="compact-button compact-button--primary" type="button" :disabled="!canStartNew" @click="emit('newTask', false)">Start a new task</button>
        <button v-if="submittedPrompt" class="compact-button" type="button" :disabled="!canStartNew" data-testid="request-reuse" @click="emit('newTask', true)">Reuse this prompt</button>
        <span v-if="operations.length && proposal === 'pending'" class="dev-drawer__hint">Unapplied page changes stay in History; you can apply them later.</span>
      </div>
    </section>
  </aside>
</template>
