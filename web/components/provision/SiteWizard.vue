<script setup lang="ts">
import SiteProvisionForm from './SiteProvisionForm.vue'
import type { SiteFormValue } from './SiteProvisionForm.vue'
const props = withDefaults(defineProps<{ open: boolean; modules?: string[]; busy?: boolean; requiredModules?: string[] }>(), {
  modules: () => ['content', 'auth', 'commerce'],
  requiredModules: () => [],
})
const emit = defineEmits<{ close: []; submit: [value: SiteFormValue] }>()
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="emit('close')">
    <section class="dialog" role="dialog" aria-modal="true" aria-labelledby="site-wizard-title">
      <button class="dialog__close" type="button" aria-label="Close" @click="emit('close')">×</button>
      <h2 id="site-wizard-title" class="sr-only">Create a site</h2>
      <SiteProvisionForm :modules="modules" :required-modules="requiredModules" :busy="busy" @submit="emit('submit', $event)" />
    </section>
  </div>
</template>
