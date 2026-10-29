<script setup lang="ts">
import { computed, reactive, ref } from 'vue'

export interface WorkspaceFormValue {
  root: string
  name: string
  layerBranch?: string
  force?: boolean
}

const props = defineProps<{ mode: 'register' | 'initialize'; busy?: boolean }>()
const emit = defineEmits<{ submit: [payload: WorkspaceFormValue] }>()
const form = reactive({ root: '', name: '', layerBranch: 'main', force: false })
const reviewing = ref(false)
const title = computed(() => props.mode === 'register' ? 'Register workspace' : 'Initialize workspace')
const error = computed(() => !form.root.startsWith('/') ? 'Enter an absolute filesystem path.' : '')

function submit() {
  if (error.value || props.busy) return
  if (props.mode === 'initialize' && !reviewing.value) {
    reviewing.value = true
    return
  }
  emit('submit', props.mode === 'register'
    ? { root: form.root.trim(), name: form.name.trim() }
    : { root: form.root.trim(), name: form.name.trim(), layerBranch: form.layerBranch.trim(), force: form.force })
}

defineExpose({ resetReview: () => { reviewing.value = false } })
</script>

<template>
  <p class="eyebrow">Explicit filesystem action</p>
  <h2>{{ reviewing ? 'Review initialization' : title }}</h2>
  <form @submit.prevent="submit">
    <template v-if="!reviewing">
      <label>Absolute path<input name="root" v-model="form.root" autocomplete="off" placeholder="/Users/you/Projects/workspace"></label>
      <small v-if="error" class="field-error">{{ error }}</small>
      <label>Display name<input name="name" v-model="form.name" autocomplete="off" placeholder="Storefront"></label>
      <label v-if="mode === 'initialize'">Layer branch<input name="layerBranch" v-model="form.layerBranch" autocomplete="off"></label>
      <label v-if="mode === 'initialize'" class="check-row"><input v-model="form.force" type="checkbox"> Allow an existing empty target</label>
      <p class="dialog__note">CouchFusion never scans for projects. Only this exact path will be inspected.</p>
      <button class="action-button action-button--primary" type="submit" :disabled="busy || Boolean(error)">{{ mode === 'register' ? 'Register exact path' : 'Review' }}</button>
    </template>
    <template v-else>
      <dl class="review-list"><div><dt>Path</dt><dd>{{ form.root }}</dd></div><div><dt>Name</dt><dd>{{ form.name || 'Derived from folder' }}</dd></div><div><dt>Branch</dt><dd>{{ form.layerBranch }}</dd></div></dl>
      <p class="dialog__note">This creates CouchFusion project files at the exact path above.</p>
      <div class="button-row"><button type="button" class="action-button" @click="reviewing = false">Back</button><button data-test="confirm-initialize" class="action-button action-button--primary" type="button" :disabled="busy" @click="submit">Initialize now</button></div>
    </template>
  </form>
</template>
