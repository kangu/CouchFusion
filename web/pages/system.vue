<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AgentHealthCard from '../components/system/AgentHealthCard.vue'
import HealthCard from '../components/system/HealthCard.vue'
import type { JobSnapshot, SystemHealth } from '../types/api'
const api = useApi(); const selection = useWorkspace(); const activity = useActivityStore(); const health = ref<SystemHealth>(); const busy = ref(''); const error = ref('')
onMounted(load)
async function load() { if (!selection.ready.value) await selection.validate(); if (!selection.activeWorkspaceId.value) return; try { health.value = await api.get(`/workspaces/${encodeURIComponent(selection.activeWorkspaceId.value)}/health`) } catch (cause) { error.value = cause instanceof Error ? cause.message : 'Health inspection failed.' } }
async function repair(component: string) { busy.value = component; try { const job = await api.post<JobSnapshot>(`/system/dependencies/${encodeURIComponent(component)}/install`); activity.trackJob(job); await navigateTo('/activity') } finally { busy.value = '' } }
const bootstrap = useBootstrap()
// The Rust control plane checks the VPS toolchain read-only; installs go through the packaging scripts.
const canRepair = computed(() => !bootstrap.isRust.value)
const cards = computed(() => health.value ? [
  { name: 'Git', component: 'git', health: health.value.git, repairable: false },
  { name: 'Bun', component: 'bun', health: health.value.bun, repairable: canRepair.value },
  { name: 'Node.js', component: 'node', health: health.value.node, repairable: false },
  { name: 'CouchDB', component: 'couchdb', health: health.value.couchdb, repairable: canRepair.value },
] : [])
</script>
<template><section class="page-sheet"><header class="page-header"><div><p class="eyebrow">Local dependencies</p><h1>System</h1><p>Independent checks for the tools CouchFusion uses. Repairs always require confirmation.</p></div><button class="compact-button" type="button" :disabled="!selection.activeWorkspaceId.value" @click="load">Run checks</button></header><p v-if="error" class="notice notice--error">{{ error }}</p><div v-if="cards.length" class="health-grid"><HealthCard v-for="card in cards" :key="card.component" v-bind="card" :busy="busy === card.component" @repair="repair" /><AgentHealthCard v-if="health?.agent" :readiness="health.agent" /></div><div v-else class="empty-state"><h2>{{ selection.activeWorkspaceId.value ? 'Run a system check.' : 'Select a workspace first.' }}</h2><p>Dependency health is evaluated in the active workspace context.</p></div></section></template>
