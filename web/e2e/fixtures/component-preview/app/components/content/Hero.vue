<script setup lang="ts">
defineProps<{
  title?: string
}>()
</script>

<template>
  <section class="fixture-hero">
    <p data-default-probe>{{ title }}</p>
    <p data-style-probe>Component stylesheet HMR probe</p>
  </section>
</template>

<style scoped>
.fixture-hero {
  min-height: 100vh;
  padding: 2rem;
  background: #fffdf5;
}

[data-default-probe] {
  font: 700 1rem/1.4 ui-monospace, monospace;
}

[data-style-probe] {
  color: rgb(17, 34, 51);
}
</style>
