export default defineNuxtConfig({
  compatibilityDate: '2026-07-14',
  ssr: false,
  devtools: { enabled: true },
  modules: ['@vueuse/nuxt'],
  components: [{ path: '~/components', pathPrefix: false }],
  css: ['~/assets/css/main.css'],
  typescript: {
    strict: true,
    typeCheck: true,
  },
  vite: {
    server: {
      strictPort: true,
    },
  },
  app: {
    head: {
      title: 'CouchFusion Workbench',
      meta: [
        { name: 'description', content: 'Local control plane for CouchFusion workspaces and sites.' },
        { name: 'theme-color', content: '#101514' },
      ],
    },
  },
})
