<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { GitRemoteOp, RepoDiff, RepoStatus, RepoSummary } from '../../types/api'
import { commitDraft, remoteActions } from '../../composables/useGitRepos'

const props = defineProps<{
  repos: RepoSummary[]
  status?: RepoStatus | undefined
  diff?: RepoDiff | undefined
  selectedRepo?: string | undefined
  error: string
  committing: boolean
  /** Remote job in progress, if any. */
  remoteBusy?: GitRemoteOp | undefined
}>()

const emit = defineEmits<{
  selectRepo: [repoId: string]
  viewDiff: [path: string]
  commit: [request: { message: string; paths: string[]; expectedHead: string }]
  remote: [op: GitRemoteOp, body: { branch?: string; confirm?: string }]
  refresh: []
  close: []
}>()

const message = ref('')
const selected = ref<Set<string>>(new Set())
const confirmingPush = ref(false)
const pushConfirm = ref('')
const actions = computed(() => remoteActions(props.status))
const pushReady = computed(() => !!props.status && pushConfirm.value === props.status.branch && !props.remoteBusy)

function startPush() {
  confirmingPush.value = true
  pushConfirm.value = ''
}

function push() {
  if (!pushReady.value || !props.status) return
  emit('remote', 'push', { branch: props.status.branch, confirm: pushConfirm.value })
  confirmingPush.value = false
}

watch(() => props.status, (status) => {
  // Default to every changed file; the user unticks what should stay out.
  selected.value = new Set(status?.files.map((file) => file.path) ?? [])
}, { immediate: true })

const canCommit = computed(() =>
  !!props.status && selected.value.size > 0 && message.value.trim().length > 0 && !props.committing)

function toggle(path: string, checked: boolean) {
  const next = new Set(selected.value)
  if (checked) next.add(path)
  else next.delete(path)
  selected.value = next
}

function fileState(file: RepoStatus['files'][number]) {
  if (file.untracked) return 'new'
  const code = `${file.index}${file.worktree}`.replace(/\./g, '')
  if (code.includes('D')) return 'deleted'
  if (code.includes('A')) return 'added'
  return 'modified'
}

function submit() {
  if (!canCommit.value || !props.status) return
  emit('commit', { message: message.value.trim(), paths: [...selected.value], expectedHead: props.status.head?.sha ?? '' })
}

/** Built from the workbench tasks whose files are still uncommitted; follows new tasks. */
const suggested = computed(() => commitDraft(props.status?.workbenchNotes))
const noteCount = computed(() => props.status?.workbenchNotes?.length ?? 0)
let lastSuggested = ''
watch(suggested, (next) => {
  // Replace the message only while it is empty or still the previous suggestion (never an edit).
  if (!message.value.trim() || message.value === lastSuggested) message.value = next
  lastSuggested = next
}, { immediate: true })
const edited = computed(() => !!suggested.value && message.value !== suggested.value)
const messageRows = computed(() => Math.min(12, Math.max(3, message.value.split('\n').length + 1)))

function useSuggested() {
  message.value = suggested.value
}

watch(() => props.committing, (now, before) => {
  if (before && !now && !props.error) message.value = suggested.value
})
</script>

<template>
  <aside class="dev-drawer dev-drawer--wide" aria-labelledby="dev-git-title">
    <header class="dev-drawer__header">
      <div><p class="eyebrow">Commit · fetch · pull · push</p><h2 id="dev-git-title">Changes</h2></div>
      <button class="dialog__close" type="button" aria-label="Close git drawer" @click="emit('close')">×</button>
    </header>

    <nav class="dev-git__repos" aria-label="Repositories">
      <button
        v-for="repo in repos"
        :key="repo.id"
        type="button"
        class="dev-git__repo"
        :class="{ 'dev-git__repo--active': repo.id === selectedRepo }"
        @click="emit('selectRepo', repo.id)"
      >
        <strong>{{ repo.path }}</strong>
        <small>{{ repo.branch }} · {{ repo.dirty }} changed{{ repo.hasRemote ? '' : ' · no remote' }}</small>
      </button>
      <button class="compact-button" type="button" @click="emit('refresh')">Refresh</button>
    </nav>

    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

    <section v-if="status" class="dev-git__status">
      <p class="eyebrow">{{ status.branch }}<template v-if="status.head"> · {{ status.head.sha.slice(0, 8) }} {{ status.head.subject }}</template></p>
      <div v-if="status.hasRemote" class="dev-git__remote">
        <span class="dev-git__tracking">{{ actions.summary }}</span>
        <button class="compact-button" type="button" :disabled="!actions.canFetch || !!remoteBusy" @click="emit('remote', 'fetch', {})">
          {{ remoteBusy === 'fetch' ? 'Fetching…' : 'Fetch' }}
        </button>
        <button class="compact-button" type="button" :disabled="!actions.canPull || !!remoteBusy" title="Fast-forward only" @click="emit('remote', 'pull', {})">
          {{ remoteBusy === 'pull' ? 'Pulling…' : 'Pull' }}
        </button>
        <button class="compact-button compact-button--primary" type="button" :disabled="!actions.canPush || !!remoteBusy" :title="actions.pushBlocked ?? 'Push (never forced)'" @click="startPush">
          {{ remoteBusy === 'push' ? 'Pushing…' : 'Push…' }}
        </button>
        <small v-if="actions.pushBlocked && !remoteBusy" class="dev-git__hint">{{ actions.pushBlocked }}</small>
      </div>
      <form v-if="confirmingPush" class="dev-git__confirm" @submit.prevent="push">
        <label for="dev-git-push-confirm">Type <code>{{ status.branch }}</code> to push it to {{ status.upstream ?? 'the remote' }}</label>
        <input id="dev-git-push-confirm" v-model="pushConfirm" autocomplete="off" spellcheck="false">
        <button class="compact-button compact-button--primary" type="submit" :disabled="!pushReady">Push {{ status.branch }}</button>
        <button class="compact-button" type="button" @click="confirmingPush = false">Cancel</button>
      </form>
      <p v-if="!status.files.length" class="empty-state">Working tree clean.</p>
      <ul v-else class="dev-git__files">
        <li v-for="file in status.files" :key="file.path">
          <label>
            <input type="checkbox" :checked="selected.has(file.path)" @change="toggle(file.path, ($event.target as HTMLInputElement).checked)">
            <span class="dev-git__state" :data-state="fileState(file)">{{ fileState(file) }}</span>
          </label>
          <button type="button" class="dev-git__path" @click="emit('viewDiff', file.path)">{{ file.path }}</button>
        </li>
      </ul>

      <section v-if="diff" class="log-viewer dev-git__diff">
        <header><span>{{ diff.path }}</span><small>{{ diff.binary ? 'binary' : diff.truncated ? 'truncated at 256 KiB' : 'diff' }}</small></header>
        <pre v-if="!diff.binary">{{ diff.diff }}</pre>
      </section>

      <form v-if="status.files.length" class="dev-drawer__form" @submit.prevent="submit">
        <label for="dev-git-message">Commit message</label>
        <textarea id="dev-git-message" v-model="message" :rows="messageRows" maxlength="4000" data-testid="commit-message" />
        <p v-if="suggested" class="dev-drawer__hint dev-git__suggested" data-testid="commit-suggested">
          <template v-if="edited">Your edit is kept as new tasks finish.
            <button type="button" class="dev-git__link" @click="useSuggested">Use the suggested message</button>
          </template>
          <template v-else>Pre-filled from {{ noteCount === 1 ? 'the workbench task' : `${noteCount} workbench tasks` }} since these files were last committed. New tasks are added as timestamped notes.</template>
        </p>
        <button class="compact-button compact-button--primary" type="submit" :disabled="!canCommit">
          {{ committing ? 'Committing…' : `Commit ${selected.size} file${selected.size === 1 ? '' : 's'}` }}
        </button>
      </form>
    </section>
    <p v-else-if="selectedRepo" class="empty-state">Reading status…</p>
  </aside>
</template>
