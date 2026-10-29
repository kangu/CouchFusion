<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { RepoDiff, RepoStatus, RepoSummary } from '../../types/api'

const props = defineProps<{
  repos: RepoSummary[]
  status?: RepoStatus | undefined
  diff?: RepoDiff | undefined
  selectedRepo?: string | undefined
  error: string
  committing: boolean
}>()

const emit = defineEmits<{
  selectRepo: [repoId: string]
  viewDiff: [path: string]
  commit: [request: { message: string; paths: string[]; expectedHead: string }]
  refresh: []
  close: []
}>()

const message = ref('')
const selected = ref<Set<string>>(new Set())

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

watch(() => props.committing, (now, before) => {
  if (before && !now && !props.error) message.value = ''
})
</script>

<template>
  <aside class="dev-drawer dev-drawer--wide" aria-labelledby="dev-git-title">
    <header class="dev-drawer__header">
      <div><p class="eyebrow">Local commits · push comes later</p><h2 id="dev-git-title">Changes</h2></div>
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
        <textarea id="dev-git-message" v-model="message" rows="3" maxlength="4000" />
        <button class="compact-button compact-button--primary" type="submit" :disabled="!canCommit">
          {{ committing ? 'Committing…' : `Commit ${selected.size} file${selected.size === 1 ? '' : 's'}` }}
        </button>
      </form>
    </section>
    <p v-else-if="selectedRepo" class="empty-state">Reading status…</p>
  </aside>
</template>
