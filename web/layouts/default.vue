<script setup lang="ts">
const route = useRoute()
const { activeWorkspaceId } = useWorkspace()
const menuOpen = ref(false)

watch(() => route.fullPath, () => {
  menuOpen.value = false
})

const primaryNavigation = [
  { label: 'Workspaces', to: '/', index: '01' },
  { label: 'Activity', to: '/activity', index: '02' },
  { label: 'System', to: '/system', index: '03' },
]

const workspaceNavigation = computed(() => activeWorkspaceId.value ? [
  { label: 'Overview', to: `/workspaces/${activeWorkspaceId.value}` },
  { label: 'Sites', to: `/workspaces/${activeWorkspaceId.value}/sites` },
  { label: 'Layers', to: `/workspaces/${activeWorkspaceId.value}/layers` },
  { label: 'Configuration', to: `/workspaces/${activeWorkspaceId.value}/config` },
] : [])
</script>

<template>
  <div class="app-frame">
    <a class="skip-link" href="#main-content">Skip to main content</a>

    <header class="mobile-bar">
      <NuxtLink class="brand brand--mobile" to="/" aria-label="CouchFusion home">
        <span class="brand__mark" aria-hidden="true"><i /><i /><i /></span>
        <span>CouchFusion</span>
      </NuxtLink>
      <button class="menu-toggle" type="button" :aria-expanded="menuOpen" aria-controls="primary-sidebar" @click="menuOpen = !menuOpen">
        <span>{{ menuOpen ? 'Close' : 'Menu' }}</span>
        <b aria-hidden="true" />
      </button>
    </header>

    <aside id="primary-sidebar" class="sidebar" :class="{ 'sidebar--open': menuOpen }">
      <div class="sidebar__grain" aria-hidden="true" />
      <NuxtLink class="brand" to="/" aria-label="CouchFusion home">
        <span class="brand__mark" aria-hidden="true"><i /><i /><i /></span>
        <span class="brand__copy"><strong>CouchFusion</strong><small>Local workbench</small></span>
      </NuxtLink>

      <nav class="primary-nav" aria-label="Primary navigation">
        <NuxtLink v-for="item in primaryNavigation" :key="item.to" :to="item.to" class="primary-nav__link">
          <span>{{ item.index }}</span>
          <strong>{{ item.label }}</strong>
        </NuxtLink>
      </nav>

      <section v-if="workspaceNavigation.length" class="workspace-nav" aria-labelledby="workspace-nav-title">
        <p id="workspace-nav-title" class="eyebrow">Active workspace</p>
        <nav aria-label="Active workspace">
          <NuxtLink v-for="item in workspaceNavigation" :key="item.to" :to="item.to">{{ item.label }}</NuxtLink>
        </nav>
      </section>

      <div class="sidebar__status">
        <span class="signal" aria-hidden="true" />
        <div><strong>Local channel</strong><small>Loopback secured</small></div>
      </div>
    </aside>

    <button v-if="menuOpen" class="sidebar-scrim" type="button" aria-label="Close navigation" @click="menuOpen = false" />

    <main id="main-content" class="main-canvas" tabindex="-1">
      <div class="main-canvas__coordinate" aria-hidden="true">CF / 127.0.0.1</div>
      <slot />
    </main>
  </div>
</template>
