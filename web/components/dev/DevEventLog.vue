<script setup lang="ts">
import { ref, toRef, watch } from 'vue'
import { useStickToBottom, useTypedLog, type LogEntry } from '../../composables/useTypedLog'

/** A run log whose new rows are typed in quickly and which follows the newest row (see useTypedLog). */
const props = defineProps<{ entries: LogEntry[] }>()

const list = ref<HTMLOListElement | null>(null)
const { shown } = useTypedLog(toRef(props, 'entries'))
const { following, onScroll, follow, jumpToLatest } = useStickToBottom(list)

watch(shown, follow, { flush: 'post' })

/** The button floats over the log; wheel turns over it still scroll the log. */
function scrollLog(event: WheelEvent) {
  const el = list.value
  if (!el) return
  const lineHeight = 16
  el.scrollTop += event.deltaMode === 1 ? event.deltaY * lineHeight : event.deltaMode === 2 ? event.deltaY * el.clientHeight : event.deltaY
}
</script>

<template>
  <div class="dev-events-wrap">
    <ol ref="list" class="dev-events" data-testid="event-log" @scroll.passive="onScroll">
      <li v-for="entry in shown" :key="entry.key" :class="entry.className">
        <span>{{ entry.code }}</span>{{ entry.shown }}<i v-if="entry.typing" class="dev-events__caret" aria-hidden="true" />
      </li>
    </ol>
    <button v-if="!following" class="dev-events__latest" type="button" data-testid="event-log-latest" @click="jumpToLatest" @wheel.prevent="scrollLog">↓ Latest</button>
  </div>
</template>
