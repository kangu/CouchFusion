<script setup lang="ts">
import { reactive, ref } from 'vue'
const props = withDefaults(defineProps<{ open: boolean; modules?: string[]; busy?: boolean }>(), { modules: () => ['content', 'auth', 'commerce'] })
const emit = defineEmits<{ close: []; submit: [value: { name: string; modules: string[]; branch: string; force: boolean }] }>()
const step = ref(1)
const form = reactive({ name: '', modules: [] as string[], branch: 'main', force: false })
function toggleModule(module: string) { form.modules = form.modules.includes(module) ? form.modules.filter((item) => item !== module) : [...form.modules, module] }
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="emit('close')"><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="site-wizard-title">
    <button class="dialog__close" type="button" aria-label="Close" @click="emit('close')">×</button><p class="eyebrow">Site provisioning · {{ step }}/3</p><h2 id="site-wizard-title">{{ step === 3 ? 'Review site' : 'Create a site' }}</h2>
    <form @submit.prevent="step < 3 ? step++ : emit('submit', { ...form })">
      <template v-if="step === 1"><label>Site name<input v-model="form.name" name="name" pattern="[a-z0-9-]+" required placeholder="storefront"></label><label>Starter branch<input v-model="form.branch" name="branch"></label></template>
      <fieldset v-else-if="step === 2"><legend>Optional modules</legend><label v-for="item in props.modules" :key="item" class="check-row"><input type="checkbox" :checked="form.modules.includes(item)" @change="toggleModule(item)"> {{ item }}</label></fieldset>
      <dl v-else class="review-list"><div><dt>Site</dt><dd>{{ form.name }}</dd></div><div><dt>Branch</dt><dd>{{ form.branch }}</dd></div><div><dt>Modules</dt><dd>{{ form.modules.join(', ') || 'None' }}</dd></div></dl>
      <div class="button-row"><button v-if="step > 1" class="action-button" type="button" @click="step--">Back</button><button class="action-button action-button--primary" type="submit" :disabled="busy || (step === 1 && !form.name)">{{ step === 3 ? 'Create site' : 'Continue' }}</button></div>
    </form>
  </section></div>
</template>
