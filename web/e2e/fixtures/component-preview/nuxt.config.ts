import { fileURLToPath } from 'node:url'

export default defineNuxtConfig({
  compatibilityDate: '2026-07-14',
  extends: ['../../../../../layers/content'],
  modulesDir: ['../../../node_modules'],
  alias: {
    '#auth': fileURLToPath(new URL('./stubs/auth', import.meta.url)),
  },
  devtools: { enabled: false },
  runtimeConfig: {
    dbLoginPrefix: 'zz-',
  },
})
