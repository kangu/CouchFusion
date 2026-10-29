<script setup lang="ts">
import WorkspaceForm from './WorkspaceForm.vue'
import type { WorkspaceFormValue } from './WorkspaceForm.vue'
const props = defineProps<{ open: boolean; mode: 'register' | 'initialize'; busy?: boolean }>()
const emit = defineEmits<{ close: []; submit: [payload: { root: string; name: string; layerBranch?: string; force?: boolean }] }>()
function submit(value: WorkspaceFormValue) { emit('submit', value) }
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="emit('close')">
    <section class="dialog" role="dialog" aria-modal="true" :aria-labelledby="`${mode}-title`">
      <button class="dialog__close" type="button" aria-label="Close" @click="emit('close')">×</button>
      <h2 :id="`${mode}-title`" class="sr-only">{{ mode === 'register' ? 'Register workspace' : 'Initialize workspace' }}</h2>
      <WorkspaceForm :mode="mode" :busy="busy" @submit="submit" />
    </section>
  </div>
</template>
