<script setup lang="ts">
import { computed } from 'vue'
import type { ComponentUsage } from '../../types/api'
import { usageRequiresAcknowledgement } from '../../composables/useComponentDelete'

const props = defineProps<{
  componentId: string
  usage?: ComponentUsage | undefined
  loading: boolean
  submitting: boolean
  error: string
  acknowledged: boolean
}>()

const emit = defineEmits<{
  'update:acknowledged': [value: boolean]
  confirm: []
  cancel: []
}>()

const gitLabels: Record<ComponentUsage['git'], string> = {
  tracked_clean: 'Tracked in git, unchanged — recoverable from git and trash',
  tracked_modified: 'Tracked in git with uncommitted changes — only trash keeps those changes',
  untracked: 'Not in git — trash is the only copy',
  unknown: 'Git state unknown',
}

const requiresAck = computed(() => props.usage ? usageRequiresAcknowledgement(props.usage) : false)
const canConfirm = computed(() => !!props.usage && !props.submitting && (!requiresAck.value || props.acknowledged))
</script>

<template>
  <div class="dialog-backdrop" @click.self="emit('cancel')">
    <section class="dialog dev-delete" role="alertdialog" aria-modal="true" aria-labelledby="dev-delete-title">
      <button class="dialog__close" type="button" aria-label="Cancel delete" @click="emit('cancel')">×</button>
      <p class="eyebrow">Delete component file</p>
      <h2 id="dev-delete-title">{{ componentId }}</h2>

      <p v-if="loading" class="empty-state">Checking file, git state and page usage…</p>
      <template v-else-if="usage">
        <dl class="dev-delete__facts">
          <div><dt>File</dt><dd><code>{{ usage.source }}</code></dd></div>
          <div><dt>SHA-256</dt><dd><code>{{ usage.sha256.slice(0, 16) }}…</code></dd></div>
          <div><dt>Git</dt><dd>{{ gitLabels[usage.git] }}</dd></div>
        </dl>

        <section class="dev-delete__usage" aria-label="Usage">
          <p v-if="usage.pageScan === 'unavailable'" class="notice notice--error" role="alert">
            Page usage could not be checked (the dev server is not answering). Pages may still use this component.
          </p>
          <template v-if="usage.pages.length">
            <p class="eyebrow">Used on {{ usage.pages.length }} page{{ usage.pages.length === 1 ? '' : 's' }}</p>
            <ul data-testid="usage-pages"><li v-for="page in usage.pages" :key="page.id"><strong>{{ page.title || page.path }}</strong> <code>{{ page.path }}</code></li></ul>
          </template>
          <template v-if="usage.codeReferences.length">
            <p class="eyebrow">Referenced in code</p>
            <ul data-testid="usage-code"><li v-for="ref in usage.codeReferences" :key="`${ref.path}:${ref.line}`"><code>{{ ref.path }}:{{ ref.line }}</code></li></ul>
          </template>
          <p v-if="!requiresAck" class="notice notice--success">No pages or code reference this component.</p>
        </section>

        <p class="dialog__note">The file moves to the server's trash (nothing is erased). Pages that still use it render the raw tag until you edit them.</p>

        <label v-if="requiresAck" class="dev-delete__ack">
          <input type="checkbox" :checked="acknowledged" data-testid="ack" @change="emit('update:acknowledged', ($event.target as HTMLInputElement).checked)">
          <span>Delete anyway — I understand what still uses it</span>
        </label>
      </template>

      <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

      <div class="button-row dev-delete__actions">
        <button class="compact-button" type="button" @click="emit('cancel')">Keep component</button>
        <button class="compact-button compact-button--danger" type="button" data-testid="confirm-delete" :disabled="!canConfirm" @click="emit('confirm')">
          {{ submitting ? 'Deleting…' : 'Move to trash' }}
        </button>
      </div>
    </section>
  </div>
</template>
