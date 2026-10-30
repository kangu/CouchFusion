<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { ComponentSource } from '../../types/api'

/** Read-only viewer for a content component's `.vue` source (phase 2 contract §4). */
const props = defineProps<{
  componentId: string
  source?: ComponentSource | undefined
  loading: boolean
  error: string
}>()

const emit = defineEmits<{ close: [] }>()

const copied = ref(false)
const lines = computed(() => (props.source?.content ?? '').replace(/\n$/, '').split('\n'))
const gutterWidth = computed(() => `${String(lines.value.length).length + 1}ch`)
const size = computed(() => {
  const bytes = props.source?.size ?? 0
  return bytes >= 1024 ? `${(bytes / 1024).toFixed(1)} KiB` : `${bytes} B`
})

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

async function copyPath() {
  if (!props.source) return
  try {
    await navigator.clipboard.writeText(props.source.path)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <div class="dialog-backdrop" @click.self="emit('close')">
    <section class="dialog dev-source" role="dialog" aria-modal="true" aria-labelledby="dev-source-title">
      <button class="dialog__close" type="button" aria-label="Close source" @click="emit('close')">×</button>
      <p class="eyebrow">Component source · read only</p>
      <h2 id="dev-source-title">{{ componentId }}</h2>

      <p v-if="loading" class="empty-state">Reading the file…</p>
      <p v-else-if="error" class="notice notice--error" role="alert">{{ error }}</p>
      <template v-else-if="source">
        <dl class="dev-source__facts">
          <div><dt>File</dt><dd><code>{{ source.path }}</code> <button class="compact-button" type="button" @click="copyPath">{{ copied ? 'Copied' : 'Copy path' }}</button></dd></div>
          <div><dt>SHA-256</dt><dd><code>{{ source.sha256.slice(0, 16) }}…</code></dd></div>
          <div><dt>Size</dt><dd>{{ size }}</dd></div>
        </dl>
        <p v-if="source.truncated" class="notice" role="status">The file is larger than 256 KiB; only the beginning is shown.</p>
        <pre class="dev-source__code" :style="{ '--gutter': gutterWidth }" data-testid="source-code"><code><span v-for="(line, index) in lines" :key="index" class="dev-source__line"><span class="dev-source__no" aria-hidden="true">{{ index + 1 }}</span>{{ line }}
</span></code></pre>
      </template>
    </section>
  </div>
</template>
