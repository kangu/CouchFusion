<script setup lang="ts">
import { computed, reactive, ref } from 'vue'

export interface SiteFormValue {
  name: string
  modules: string[]
  dbLoginPrefix: string
  branch: string
  force: boolean
}

const props = withDefaults(defineProps<{ modules?: string[]; requiredModules?: string[]; busy?: boolean }>(), {
  modules: () => ['content', 'auth', 'commerce'],
  requiredModules: () => [],
})
const emit = defineEmits<{ submit: [value: SiteFormValue] }>()
const step = ref(1)
const form = reactive<SiteFormValue>({ name: '', modules: [...props.requiredModules], dbLoginPrefix: '', branch: 'main', force: false })
const stepOneValid = computed(() => form.name.length > 0 && /^[a-z]{2}$/.test(form.dbLoginPrefix))
const optionalModules = computed(() => props.modules.filter(module => !props.requiredModules.includes(module)))

function toggleModule(module: string) {
  form.modules = form.modules.includes(module)
    ? form.modules.filter(item => item !== module)
    : [...form.modules, module]
}

function advanceOrSubmit() {
  if (props.busy) return
  if (step.value === 1) {
    if (!stepOneValid.value) return
    step.value++
    return
  }
  if (step.value < 3) {
    step.value++
    return
  }
  emit('submit', { ...form, modules: [...new Set([...props.requiredModules, ...form.modules])] })
}
</script>

<template>
  <p class="eyebrow">Site provisioning · {{ step }}/3</p>
  <h2>{{ step === 3 ? 'Review site' : 'Create a site' }}</h2>
  <form @submit.prevent="advanceOrSubmit">
    <template v-if="step === 1">
      <label>Site name<input v-model="form.name" name="name" pattern="[a-z0-9-]+" required placeholder="storefront"></label>
      <label>Database login prefix<input v-model="form.dbLoginPrefix" name="dbLoginPrefix" pattern="[a-z]{2}" minlength="2" maxlength="2" required placeholder="ac" aria-describedby="db-login-prefix-help"></label>
      <p id="db-login-prefix-help" class="dialog__note">Exactly two lowercase letters. CouchFusion stores this as {{ form.dbLoginPrefix || '••' }}-.</p>
      <label>Starter branch<input v-model="form.branch" name="branch"></label>
    </template>
    <fieldset v-else-if="step === 2">
      <legend>{{ requiredModules.length ? 'Modules' : 'Optional modules' }}</legend>
      <label v-for="item in requiredModules" :key="item" class="check-row"><input type="checkbox" checked disabled> {{ item }} (required)</label>
      <label v-for="item in optionalModules" :key="item" class="check-row"><input type="checkbox" :checked="form.modules.includes(item)" @change="toggleModule(item)"> {{ item }}</label>
    </fieldset>
    <dl v-else class="review-list">
      <div><dt>Site</dt><dd>{{ form.name }}</dd></div><div><dt>Database prefix</dt><dd>{{ form.dbLoginPrefix }}-</dd></div><div><dt>Branch</dt><dd>{{ form.branch }}</dd></div><div><dt>Modules</dt><dd>{{ [...new Set([...requiredModules, ...form.modules])].join(', ') || 'None' }}</dd></div>
    </dl>
    <div class="button-row"><button v-if="step > 1" class="action-button" type="button" @click="step--">Back</button><button class="action-button action-button--primary" type="submit" :disabled="busy || (step === 1 && !stepOneValid)">{{ step === 3 ? 'Create site' : 'Continue' }}</button></div>
  </form>
</template>
