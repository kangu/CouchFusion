<script setup lang="ts">
import { useEventListener, useScrollLock } from '@vueuse/core'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { ComponentPreviewHost, ContentComponent } from '../../types/api'

const props = defineProps<{
  open: boolean
  component: ContentComponent
  previewHost: ComponentPreviewHost
  returnFocus?: HTMLElement | undefined
}>()

const emit = defineEmits<{
  close: []
  restartHost: []
}>()

const closeButton = ref<HTMLButtonElement>()
const body = ref<HTMLElement | null>(null)
const documentTarget = () => typeof document === 'undefined' ? null : document
const scrollLocked = useScrollLock(body)

const frameURL = computed(() => {
  if (props.previewHost.status !== 'ready' || !props.previewHost.url) return ''
  const base = new URL(props.previewHost.url)
  const target = new URL(
    `/__couchfusion/components/preview/${encodeURIComponent(props.component.id)}`,
    base.origin,
  )
  target.search = base.search
  return target.toString()
})

function close() {
  emit('close')
}

function restorePage() {
  scrollLocked.value = false
  document.querySelector('#__nuxt')?.removeAttribute('inert')
  props.returnFocus?.focus()
}

useEventListener(documentTarget, 'keydown', (event: KeyboardEvent) => {
  if (props.open && event.key === 'Escape') {
    event.preventDefault()
    close()
  }
})

watch(() => props.open, async (open) => {
  if (!body.value) return
  scrollLocked.value = open
  const app = document.querySelector('#__nuxt')
  if (open) {
    app?.setAttribute('inert', '')
    await nextTick()
    closeButton.value?.focus()
  } else {
    restorePage()
  }
}, { immediate: true })

onMounted(async () => {
  body.value = document.body
  if (props.open) {
    scrollLocked.value = true
    document.querySelector('#__nuxt')?.setAttribute('inert', '')
    await nextTick()
    closeButton.value?.focus()
  }
})

onBeforeUnmount(restorePage)
</script>

<template>
  <Teleport to="body">
    <Transition name="component-preview">
      <section
        v-if="open"
        class="component-preview-overlay"
        role="dialog"
        aria-modal="true"
        :aria-label="`Preview ${component.label}`"
      >
        <div class="component-preview-overlay__rail">
          <span>Live component host</span>
          <strong>{{ component.label }}</strong>
          <code>{{ component.id }}</code>
          <button
            ref="closeButton"
            type="button"
            class="component-preview-overlay__close"
            data-action="close-component-preview"
            aria-label="Close component preview"
            @click="close"
          >Close ×</button>
        </div>

        <iframe
          v-if="frameURL"
          class="component-preview-overlay__frame"
          data-component-preview-frame
          :src="frameURL"
          :title="`Preview ${component.label}`"
        />
        <div v-else class="component-preview-overlay__disconnected" role="alert">
          <p class="eyebrow">Preview disconnected</p>
          <h2>Host unavailable</h2>
          <p>{{ previewHost.problem || 'The component preview host is not running.' }}</p>
          <div class="button-row">
            <button class="compact-button compact-button--primary" type="button" data-action="restart-preview-host" @click="emit('restartHost')">
              Start preview host
            </button>
            <button class="compact-button" type="button" @click="close">Close preview</button>
          </div>
        </div>
      </section>
    </Transition>
  </Teleport>
</template>

<style scoped>
.component-preview-overlay {
  position: fixed;
  z-index: 120;
  inset: 0;
  display: grid;
  grid-template-rows: 3.25rem minmax(0, 1fr);
  background: #080c0b;
  color: var(--paper);
}

.component-preview-overlay__rail {
  position: relative;
  display: grid;
  grid-template-columns: auto auto minmax(0, 1fr) auto;
  align-items: center;
  gap: .8rem;
  padding: 0 .8rem 0 1rem;
  border-bottom: 1px solid rgba(111, 228, 224, .28);
  background:
    linear-gradient(90deg, rgba(111, 228, 224, .08), transparent 34%),
    var(--ink-raised);
}

.component-preview-overlay__rail span,
.component-preview-overlay__rail code {
  color: rgba(241, 239, 230, .52);
  font: .57rem/1 var(--mono);
  letter-spacing: .08em;
  text-transform: uppercase;
}

.component-preview-overlay__rail strong {
  font: 700 .78rem/1 var(--mono);
}

.component-preview-overlay__rail code {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.component-preview-overlay__close {
  min-height: 2.25rem;
  padding: 0 .75rem;
  border: 1px solid var(--cyan);
  background: transparent;
  color: var(--cyan);
  cursor: pointer;
  font: 700 .6rem/1 var(--mono);
  letter-spacing: .06em;
  text-transform: uppercase;
}

.component-preview-overlay__frame {
  width: 100%;
  height: 100%;
  border: 0;
  background: var(--paper);
}

.component-preview-overlay__disconnected {
  align-self: center;
  justify-self: center;
  width: min(34rem, calc(100% - 2rem));
  padding: 2rem;
  border: 1px solid rgba(111, 228, 224, .4);
  background: var(--ink-raised);
  box-shadow: 1rem 1rem 0 rgba(0, 0, 0, .32);
}

.component-preview-overlay__disconnected h2 {
  margin: 0;
  font: 700 1.5rem/1 var(--display);
  text-transform: uppercase;
}

.component-preview-overlay__disconnected > p:not(.eyebrow) {
  color: rgba(241, 239, 230, .68);
  line-height: 1.5;
}

.component-preview-overlay__disconnected .compact-button:not(.compact-button--primary) {
  border-color: var(--paper);
  color: var(--paper);
}

.component-preview-enter-active,
.component-preview-leave-active { transition: opacity 150ms ease; }
.component-preview-enter-from,
.component-preview-leave-to { opacity: 0; }

@media (max-width: 620px) {
  .component-preview-overlay__rail {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .component-preview-overlay__rail span,
  .component-preview-overlay__rail code { display: none; }
}
</style>
