<script setup lang="ts">
import type { EffectiveConfig } from '../../../types/api'
definePageMeta({ middleware: 'workspace' })
const route = useRoute(); const api = useApi(); const effective = ref<EffectiveConfig>(); const saving = ref(false); const message = ref(''); const id = computed(() => String(route.params.workspaceId))
onMounted(load); async function load() { effective.value = await api.get(`/workspaces/${encodeURIComponent(id.value)}/config`) }
async function save(value: { workspace: { defaultRoot: string } }) { saving.value = true; try { effective.value = await api.put(`/workspaces/${encodeURIComponent(id.value)}/config`, value); message.value = 'Workspace configuration saved.' } finally { saving.value = false } }
</script>
<template><section class="page-sheet"><header class="page-header"><div><p class="eyebrow">Layered configuration</p><h1>Configuration</h1><p>Every effective value retains its embedded, global, or workspace source.</p></div></header><p v-if="message" class="notice notice--success" role="status">{{ message }}</p><ConfigEditor v-if="effective" :effective="effective" :saving="saving" @save="save" /><p v-else class="empty-state">Resolving configuration layers…</p></section></template>
