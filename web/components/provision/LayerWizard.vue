<script setup lang="ts">
import { reactive, ref } from 'vue'
defineProps<{ open: boolean; busy?: boolean }>()
const emit = defineEmits<{ close: []; submit: [value: { name: string; branch: string; force: boolean }] }>()
const review = ref(false)
const form = reactive({ name: '', branch: 'main', force: false })
function submit() { if (!review.value) review.value = true; else emit('submit', { ...form }) }
</script>

<template><div v-if="open" class="dialog-backdrop" @click.self="emit('close')"><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="layer-title"><button class="dialog__close" type="button" aria-label="Close" @click="emit('close')">×</button><p class="eyebrow">Shared Nuxt layer</p><h2 id="layer-title">{{ review ? 'Review layer' : 'Create a layer' }}</h2><form @submit.prevent="submit"><template v-if="!review"><label>Layer name<input v-model="form.name" required pattern="[a-z0-9-]+"></label><label>Starter branch<input v-model="form.branch"></label></template><dl v-else class="review-list"><div><dt>Name</dt><dd>{{ form.name }}</dd></div><div><dt>Branch</dt><dd>{{ form.branch }}</dd></div></dl><div class="button-row"><button v-if="review" class="action-button" type="button" @click="review = false">Back</button><button class="action-button action-button--primary" type="submit" :disabled="busy">{{ review ? 'Create layer' : 'Review' }}</button></div></form></section></div></template>
