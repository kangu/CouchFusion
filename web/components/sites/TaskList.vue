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
    <article v-for="task in tasks" :key="task.id" class="task-row">
      <div><strong>{{ task.name || task.id }}</strong><small>{{ task.longRunning ? 'Managed process' : 'Finite job' }} · {{ task.kind }}</small></div>
      <button class="compact-button" type="button" :data-task="task.id" :disabled="busyTask === task.id" @click="execute(task)">{{ task.longRunning ? 'Start' : 'Run' }}</button>
    </article>
  </div>
</template>
