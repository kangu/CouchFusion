<script setup lang="ts">
import type { Task } from '../../types/api'
defineProps<{ tasks: Task[]; busyTask?: string }>()
const emit = defineEmits<{ run: [taskId: string]; start: [taskId: string] }>()
function execute(task: Task) {
  if (task.longRunning) emit('start', task.id)
  else emit('run', task.id)
}
</script>

<template>
  <div class="task-list">
    <article v-for="task in tasks" :key="task.id" class="task-row" :aria-busy="busyTask === task.id">
      <div><strong>{{ task.name || task.id }}</strong><small>{{ task.longRunning ? 'Managed process' : 'Finite job' }} · {{ task.kind }}</small></div>
      <button class="compact-button" type="button" :data-task="task.id" :disabled="busyTask === task.id" @click="execute(task)">{{ task.longRunning ? 'Start' : 'Run' }}</button>
    </article>
  </div>
</template>

<style scoped>
.task-list {
  border-top: 1px solid var(--line-paper);
}

.task-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: .75rem;
  padding: .55rem 0;
  border-bottom: 1px solid var(--line-paper);
}

.task-row > div {
  display: grid;
  min-width: 0;
  gap: .18rem;
}

.task-row strong {
  overflow: hidden;
  font: 700 .75rem/1.2 var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-row small {
  color: #67716c;
  font: .56rem/1.2 var(--mono);
  letter-spacing: .02em;
}

.task-row .compact-button {
  min-height: 2.25rem;
  padding: .55rem .75rem;
}
</style>
