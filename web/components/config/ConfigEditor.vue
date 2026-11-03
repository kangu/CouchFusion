<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import type { EffectiveConfig } from '../../types/api'
const props = defineProps<{ effective: EffectiveConfig; saving?: boolean }>()
const emit = defineEmits<{ save: [value: { workspace: { defaultRoot: string } }] }>()
const form = reactive({ defaultRoot: props.effective.config.workspace.defaultRoot })
const reviewing = ref(false)
watch(() => props.effective.config.workspace.defaultRoot, (value) => { form.defaultRoot = value })
function submit() {
  if (!form.defaultRoot.startsWith('/')) return
  if (!reviewing.value) { reviewing.value = true; return }
  emit('save', { workspace: { defaultRoot: form.defaultRoot } })
}
</script>

<template>
  <form class="config-editor" @submit.prevent="submit">
    <template v-if="!reviewing">
      <label>Default project root <span class="source-badge">{{ effective.sources['workspace.defaultRoot'] || 'embedded' }}</span><input name="defaultRoot" v-model="form.defaultRoot"></label>
      <p v-if="!form.defaultRoot.startsWith('/')" class="field-error">Use an absolute path.</p>
      <button class="action-button action-button--primary" type="submit">Review changes</button>
    </template>
    <section v-else class="review-panel">
      <p class="eyebrow">Review configuration</p><h2>One workspace override</h2>
      <dl class="review-list"><div><dt>Default root</dt><dd>{{ form.defaultRoot }}</dd></div><div><dt>Source after save</dt><dd><span class="source-badge">workspace</span></dd></div></dl>
      <div class="button-row"><button class="action-button" type="button" @click="reviewing = false">Back</button><button data-test="confirm-config" class="action-button action-button--primary" type="button" :disabled="saving" @click="submit">Save override</button></div>
    </section>
  </form>
</template>
