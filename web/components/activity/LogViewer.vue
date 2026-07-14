<script setup lang="ts">
import { computed } from 'vue'
import type { ActivityEvent } from '../../types/api'
const props = withDefaults(defineProps<{ events: ActivityEvent[]; connected?: boolean; limit?: number }>(), { connected: true, limit: 500 })
const visibleEvents = computed(() => props.events.slice(-props.limit))
</script>

<template>
  <section class="log-viewer" aria-label="Activity log">
    <header><span class="signal" :class="{ 'signal--waiting': !connected }" /><strong>{{ connected ? 'Live output' : 'Reconnecting' }}</strong><small>{{ visibleEvents.length }} lines</small></header>
    <pre aria-live="polite"><code v-for="event in visibleEvents" :key="event.id"><span>{{ String(event.id).padStart(3, '0') }}</span> {{ event.text }}
</code></pre>
  </section>
</template>
