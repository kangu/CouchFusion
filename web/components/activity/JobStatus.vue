<script setup lang="ts">
import { computed } from 'vue'
import type { JobSnapshot } from '../../types/api'
const props = defineProps<{ job: JobSnapshot }>()
const emit = defineEmits<{ cancel: [jobId: string] }>()
const active = computed(() => ['queued', 'running', 'stopping'].includes(props.job.status))
</script>

<template>
  <article class="activity-card">
    <div><p class="eyebrow">{{ job.type }}</p><h3>{{ job.status }}</h3><small>{{ job.id }}</small></div>
    <button v-if="active" class="compact-button compact-button--danger" type="button" @click="emit('cancel', job.id)">Cancel job</button>
  </article>
</template>
