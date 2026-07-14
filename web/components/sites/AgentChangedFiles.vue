<script setup lang="ts">
import type { AgentChangedFile } from '../../types/api'

defineProps<{
  baseline: AgentChangedFile[]
  changed: AgentChangedFile[]
}>()

function changeLabel(kind: AgentChangedFile['kind']) {
  return { added: 'Added', modified: 'Modified', deleted: 'Deleted' }[kind]
}
</script>

<template>
  <section v-if="baseline.length || changed.length" class="agent-files" aria-label="Changed files">
    <div v-if="baseline.length" class="agent-files__group" data-file-group="baseline">
      <h4>Already modified before generation</h4>
      <ul>
        <li v-for="file in baseline" :key="file.path">
          <span>{{ changeLabel(file.kind) }}</span>
          <code>{{ file.path }}</code>
        </li>
      </ul>
    </div>
    <div v-if="changed.length" class="agent-files__group" data-file-group="conversation">
      <h4>Changed by this conversation</h4>
      <ul>
        <li v-for="file in changed" :key="file.path">
          <span>{{ changeLabel(file.kind) }}</span>
          <code>{{ file.path }}</code>
        </li>
      </ul>
    </div>
  </section>
</template>
