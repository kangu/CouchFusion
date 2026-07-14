<script setup lang="ts">
import { computed } from 'vue'
import type { ComponentPreviewHost, ComponentRegistryResponse } from '../../types/api'

const props = withDefaults(defineProps<{
  registry: ComponentRegistryResponse
  rebuilding: boolean
  openingId: string
  error: string
  previewHost?: ComponentPreviewHost
  previewBusy?: boolean
  previewingId?: string
}>(), {
  previewHost: () => ({ status: 'stopped' }),
  previewBusy: false,
  previewingId: '',
})

const emit = defineEmits<{
  rebuild: []
  open: [componentId: string]
  'start-preview-host': []
  'stop-preview-host': []
  preview: [componentId: string, trigger: HTMLButtonElement]
}>()

const countLabel = computed(() => {
  const count = props.registry.components.length
  return `${count} ${count === 1 ? 'component' : 'components'}`
})

const statusLabel = computed(() => ({
  ready: 'Ready',
  stale: 'Stale',
  missing: 'Missing',
  invalid: 'Invalid',
})[props.registry.status])

const guidance = computed(() => props.registry.problem || ({
  stale: 'Definitions may not match current Vue sources.',
  missing: 'The component registry has not been generated.',
  invalid: 'The component registry could not be trusted.',
  ready: '',
})[props.registry.status])

const previewStatusLabel = computed(() => ({
  stopped: 'Stopped',
  starting: 'Starting',
  ready: 'Ready',
  failed: 'Failed',
})[props.previewHost.status])

function propertyLabel(count: number) {
  return `${count} ${count === 1 ? 'property' : 'properties'}`
}

function preview(componentId: string, event: MouseEvent) {
  const trigger = event.currentTarget
  if (trigger instanceof HTMLButtonElement) emit('preview', componentId, trigger)
}
</script>

<template>
  <section class="panel component-registry" aria-label="Local content components">
    <header class="component-registry__header">
      <div>
        <p class="eyebrow">Build-time registry</p>
        <h2>Local content components</h2>
      </div>
      <div class="component-registry__tools">
        <span class="component-registry__count">{{ countLabel }}</span>
        <span class="status-lozenge" :class="`status-lozenge--${registry.status}`" data-registry-status>{{ statusLabel }}</span>
        <span class="preview-host-status" :class="`preview-host-status--${previewHost.status}`" data-preview-host-status>
          Preview {{ previewStatusLabel }}
        </span>
        <button
          v-if="previewHost.status === 'stopped' || previewHost.status === 'failed'"
          class="compact-button compact-button--primary"
          type="button"
          data-action="start-preview-host"
          :disabled="previewBusy"
          @click="emit('start-preview-host')"
        >{{ previewBusy ? 'Starting…' : 'Start preview host' }}</button>
        <button
          v-else
          class="compact-button compact-button--danger"
          type="button"
          data-action="stop-preview-host"
          :disabled="previewBusy"
          @click="emit('stop-preview-host')"
        >{{ previewHost.status === 'starting' ? 'Stop starting host' : 'Stop preview host' }}</button>
        <button
          v-if="registry.canRebuild"
          class="compact-button"
          type="button"
          data-action="rebuild-registry"
          :disabled="rebuilding"
          @click="emit('rebuild')"
        >{{ rebuilding ? 'Rebuilding…' : 'Rebuild registry' }}</button>
      </div>
    </header>

    <p v-if="error" class="notice notice--error component-registry__error" role="alert">{{ error }}</p>
    <p v-if="previewHost.status === 'failed' && previewHost.problem" class="notice notice--error component-registry__error" role="alert">
      {{ previewHost.problem }}
    </p>

    <div v-if="registry.status !== 'ready'" class="component-registry__guidance" :class="`component-registry__guidance--${registry.status}`">
      <strong>{{ statusLabel }} registry</strong>
      <p>{{ guidance }}</p>
    </div>

    <p v-else-if="registry.components.length === 0" class="component-registry__empty">No local content components registered.</p>

    <div v-else class="component-ledger">
      <article v-for="component in registry.components" :key="component.id" class="component-row">
        <div class="component-row__identity">
          <strong>{{ component.label }}</strong>
          <code>{{ component.id }}</code>
          <p v-if="component.description">{{ component.description }}</p>
        </div>
        <div class="component-row__metadata">
          <span>{{ propertyLabel(component.propCount) }}</span>
          <span v-if="component.allowChildren">Accepts children</span>
          <code>{{ component.source }}</code>
        </div>
        <div class="component-row__actions">
          <button
            class="compact-button compact-button--preview"
            type="button"
            :data-preview-component="component.id"
            :aria-label="`Preview ${component.label}`"
            :disabled="previewHost.status !== 'ready' || previewBusy || previewingId === component.id"
            @click="preview(component.id, $event)"
          >{{ previewingId === component.id ? 'Opening preview…' : 'Preview' }}</button>
          <button
            class="compact-button"
            type="button"
            :data-component="component.id"
            :disabled="openingId === component.id"
            @click="emit('open', component.id)"
          >{{ openingId === component.id ? 'Opening…' : 'Open source' }}</button>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.component-registry {
  min-width: 0;
}

.component-registry__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1.25rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--line-paper);
}

.component-registry__header .eyebrow {
  margin-bottom: .55rem;
}

.component-registry__header h2 {
  margin: 0;
  font: 700 clamp(1.25rem, 2vw, 1.65rem)/1 var(--display);
  letter-spacing: -.01em;
  text-transform: uppercase;
}

.component-registry__tools {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: .45rem;
}

.component-registry__count {
  color: #59635e;
  font: 700 .58rem/1 var(--mono);
  letter-spacing: .05em;
  text-transform: uppercase;
}

.preview-host-status {
  color: #59635e;
  font: 700 .56rem/1 var(--mono);
  letter-spacing: .05em;
  text-transform: uppercase;
}

.preview-host-status--ready { color: #176b68; }
.preview-host-status--starting { color: #755410; }
.preview-host-status--failed { color: #9d3828; }

.component-registry__error {
  margin: 1rem 0 0;
}

.component-registry__guidance,
.component-registry__empty {
  margin: 1rem 0 0;
  padding: 1rem;
  border: 1px dashed var(--line-paper);
  background: rgba(255, 253, 245, .42);
}

.component-registry__guidance strong {
  font: 700 .68rem/1 var(--mono);
  letter-spacing: .05em;
  text-transform: uppercase;
}

.component-registry__guidance p,
.component-registry__empty {
  color: #56605b;
  font-size: .88rem;
  line-height: 1.5;
}

.component-registry__guidance p {
  margin: .55rem 0 0;
}

.component-ledger {
  border-top: 1px solid var(--line-paper);
}

.component-row {
  display: grid;
  grid-template-columns: minmax(9rem, .8fr) minmax(12rem, 1.2fr) auto;
  align-items: center;
  gap: 1rem;
  padding: .85rem 0;
  border-bottom: 1px solid var(--line-paper);
}

.component-row__identity,
.component-row__metadata {
  display: grid;
  min-width: 0;
  gap: .25rem;
}

.component-row__identity strong {
  font: 700 .82rem/1.2 var(--mono);
}

.component-row__identity code,
.component-row__metadata code {
  overflow-wrap: anywhere;
  color: #5f6964;
  font: .6rem/1.4 var(--mono);
}

.component-row__identity p {
  margin: .15rem 0 0;
  color: #56605b;
  font-size: .78rem;
  line-height: 1.35;
}

.component-row__metadata span {
  width: fit-content;
  color: #176b68;
  font: 700 .56rem/1.25 var(--mono);
  letter-spacing: .03em;
  text-transform: uppercase;
}

.component-row__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: .4rem;
}

.compact-button--preview {
  border-color: var(--cyan-deep);
  color: var(--cyan-deep);
}

.status-lozenge--stale {
  border-color: rgba(213, 159, 59, .65);
  background: rgba(213, 159, 59, .12);
  color: #755410;
}

.status-lozenge--missing,
.status-lozenge--invalid {
  border-color: rgba(225, 93, 71, .55);
  background: rgba(225, 93, 71, .08);
  color: #9d3828;
}

@media (max-width: 700px) {
  .component-registry__header {
    align-items: stretch;
    flex-direction: column;
  }

  .component-registry__tools {
    justify-content: flex-start;
  }

  .component-row {
    grid-template-columns: 1fr;
    gap: .65rem;
  }

  .component-row .compact-button {
    width: fit-content;
  }

  .component-row__actions { justify-content: flex-start; }
}
</style>
