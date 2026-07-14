<script setup lang="ts">
import { computed } from 'vue'
import type { AgentReadiness } from '../../types/api'

const props = defineProps<{ readiness: AgentReadiness }>()

const guidance = computed(() => {
  switch (props.readiness.status) {
    case 'missing':
    case 'launch_failed':
      return 'Verify the local installation with codex --version, then restart CouchFusion.'
    case 'unsupported':
      return 'Install a supported Codex version, then restart CouchFusion.'
    case 'unauthenticated':
      return 'Authenticate the local Codex session with codex login, then rerun checks.'
    case 'restarting':
      return 'The Codex app server is restarting. Wait briefly, then rerun checks.'
    default:
      return ''
  }
})

const controlState = computed(() => props.readiness.problem?.toLowerCase().includes('control') ? 'Unavailable' : props.readiness.available ? 'Connected' : 'Not ready')
</script>

<template>
  <article class="health-card agent-health-card" :class="{ 'health-card--down': !readiness.available }">
    <header>
      <span class="signal" :class="{ 'signal--danger': !readiness.available, 'signal--waiting': readiness.status === 'restarting' }" />
      <p class="eyebrow">Local agent</p>
    </header>
    <h2>Codex generation</h2>
    <p class="health-card__state">{{ readiness.problem || (readiness.available ? 'Available for component generation' : readiness.status) }}</p>
    <dl class="review-list">
      <div><dt>Codex binary</dt><dd>{{ readiness.version || 'Not verified' }}</dd></div>
      <div><dt>App server</dt><dd>{{ readiness.available ? 'Ready' : readiness.status.replaceAll('_', ' ') }}</dd></div>
      <div><dt>Local account</dt><dd>{{ readiness.accountType || (readiness.status === 'unauthenticated' ? 'Authentication required' : 'Not reported') }}</dd></div>
      <div><dt>couchfusion-control</dt><dd>{{ controlState }}</dd></div>
      <div><dt>Component skill</dt><dd>{{ readiness.available ? 'Registered' : 'Pending agent readiness' }}</dd></div>
    </dl>
    <p v-if="guidance" class="notice" :class="{ 'notice--error': readiness.status !== 'restarting' }">{{ guidance }}</p>
  </article>
</template>
