<script setup lang="ts">
import { computed } from 'vue'
import type { ComponentRegistryResponse, ContentComponent, TrashEntry } from '../../types/api'
import { canRestore, rebuildRecommended, registryStatusLabels, restoreBlockedReason } from '../../composables/useComponentsPanel'

/**
 * Components drawer (phase 2 contract §4): the site's content-component registry with
 * source and delete actions, a registry rebuild, and the server trash with restore.
 */
const props = defineProps<{
  tab: 'registry' | 'trash'
  registry?: ComponentRegistryResponse | undefined
  components: ContentComponent[]
  trash: TrashEntry[]
  loadingRegistry: boolean
  loadingTrash: boolean
  busy?: string | undefined
  error: string
  canRebuild: boolean
  canDelete: boolean
  canViewSource: boolean
  canUseTrash: boolean
}>()

const emit = defineEmits<{
  'update:tab': [value: 'registry' | 'trash']
  rebuild: []
  viewSource: [componentId: string]
  delete: [componentId: string]
  restore: [entry: TrashEntry]
  refresh: []
  close: []
}>()

const status = computed(() => props.registry?.status)
const restorableCount = computed(() => props.trash.filter((entry) => entry.restorable).length)
const gitLabels: Record<TrashEntry['git'], string> = {
  tracked_clean: 'tracked, unchanged',
  tracked_modified: 'tracked, had uncommitted changes',
  untracked: 'not in git',
  unknown: 'git state unknown',
}

function when(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>

<template>
  <aside class="dev-drawer dev-drawer--wide" aria-labelledby="dev-components-title">
    <header class="dev-drawer__header">
      <div><p class="eyebrow">Content components</p><h2 id="dev-components-title">Components</h2></div>
      <button class="dialog__close" type="button" aria-label="Close components drawer" @click="emit('close')">×</button>
    </header>

    <div class="button-row dev-tabs" role="tablist">
      <button class="compact-button" :class="{ 'compact-button--primary': tab === 'registry' }" role="tab" type="button" :aria-selected="tab === 'registry'" @click="emit('update:tab', 'registry')">Registry ({{ components.length }})</button>
      <button v-if="canUseTrash" class="compact-button" :class="{ 'compact-button--primary': tab === 'trash' }" role="tab" type="button" :aria-selected="tab === 'trash'" @click="emit('update:tab', 'trash')">Trash ({{ trash.length }})</button>
      <button class="compact-button dev-tabs__end" type="button" @click="emit('refresh')">Refresh</button>
    </div>

    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>

    <section v-if="tab === 'registry'" class="dev-components" aria-label="Component registry">
      <div class="dev-components__status">
        <span v-if="status" class="dev-pill" :data-status="status === 'ready' ? 'ready' : 'failed'" data-testid="registry-status">{{ registryStatusLabels[status] }}</span>
        <small v-if="registry?.problem">{{ registry.problem }}</small>
        <button
          v-if="canRebuild"
          class="compact-button dev-tabs__end"
          :class="{ 'compact-button--primary': rebuildRecommended(status) }"
          type="button"
          data-testid="rebuild"
          :disabled="!!busy"
          @click="emit('rebuild')"
        >
          {{ busy === 'rebuild' ? 'Rebuilding…' : 'Rebuild registry' }}
        </button>
      </div>

      <p v-if="loadingRegistry && !components.length" class="empty-state">Loading the registry…</p>
      <p v-else-if="!components.length" class="empty-state">No content components in the registry.</p>
      <ul v-else class="dev-components__list">
        <li v-for="component in components" :key="component.id" class="dev-components__row">
          <div class="dev-components__name">
            <strong>{{ component.label }}</strong>
            <code>{{ component.id }}</code>
            <small><code>{{ component.source }}</code> · {{ component.propCount }} props<template v-if="component.allowChildren"> · children</template></small>
          </div>
          <div class="dev-components__actions">
            <button v-if="canViewSource" class="compact-button" type="button" @click="emit('viewSource', component.id)">Source</button>
            <button v-if="canDelete" class="compact-button compact-button--danger" type="button" :disabled="!!busy" @click="emit('delete', component.id)">Delete</button>
          </div>
        </li>
      </ul>
    </section>

    <section v-else class="dev-components" aria-label="Trash">
      <p class="dialog__note">Deleted component files stay on the server. Restoring puts the file back where it was; the builder reloads with it highlighted.</p>
      <p v-if="loadingTrash && !trash.length" class="empty-state">Loading the trash…</p>
      <p v-else-if="!trash.length" class="empty-state">The trash is empty.</p>
      <ul v-else class="dev-components__list" :data-restorable="restorableCount">
        <li v-for="entry in trash" :key="entry.trashId" class="dev-components__row">
          <div class="dev-components__name">
            <strong>{{ entry.componentId }}</strong>
            <small><code>{{ entry.source }}</code></small>
            <small>Deleted {{ when(entry.deletedAt) }} · {{ gitLabels[entry.git] }}</small>
            <small v-if="!entry.restorable" class="dev-components__problem">{{ restoreBlockedReason(entry) }}</small>
          </div>
          <div class="dev-components__actions">
            <button
              class="compact-button compact-button--primary"
              type="button"
              :disabled="!canRestore(entry, !!busy)"
              :data-testid="`restore-${entry.trashId}`"
              @click="emit('restore', entry)"
            >
              {{ busy === entry.trashId ? 'Restoring…' : 'Restore' }}
            </button>
          </div>
        </li>
      </ul>
    </section>
  </aside>
</template>
