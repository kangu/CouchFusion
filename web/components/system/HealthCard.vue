<script setup lang="ts">
import { ref } from 'vue'
import type { DependencyHealth } from '../../types/api'
const props = defineProps<{ name: string; component: string; health: DependencyHealth; repairable?: boolean; busy?: boolean }>()
const emit = defineEmits<{ repair: [component: string] }>()
const confirming = ref(false)
</script>

<template>
  <article class="health-card" :class="{ 'health-card--down': !health.available }">
    <header><span class="signal" :class="{ 'signal--danger': !health.available }" /><p class="eyebrow">{{ component }}</p></header><h2>{{ name }}</h2>
    <p class="health-card__state">{{ health.available ? 'Available' : health.problem || 'Unavailable' }}</p><code v-if="health.version">{{ health.version }}</code><small v-if="health.path">{{ health.path }}</small>
    <template v-if="repairable && !health.available"><button v-if="!confirming" class="compact-button" type="button" @click="confirming = true">Prepare repair</button><div v-else class="repair-confirm"><p>This installs {{ name }} using the configured local package workflow.</p><div class="button-row"><button type="button" class="compact-button" @click="confirming = false">Back</button><button type="button" class="compact-button compact-button--danger" :disabled="busy" @click="emit('repair', props.component)">Confirm install</button></div></div></template>
  </article>
</template>
