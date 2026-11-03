import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: 'component-preview.spec.ts',
  timeout: 45_000,
  fullyParallel: false,
  workers: 1,
  outputDir: 'output/playwright-preview',
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4178',
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: [
    {
      command: 'cd e2e/fixtures/component-preview && COUCHFUSION_PREVIEW=1 COUCHFUSION_PREVIEW_TOKEN=fixture NUXT_HOST=127.0.0.1 NUXT_PORT=4311 bun run dev',
      url: 'http://127.0.0.1:4311/__couchfusion/components/preview/hero?token=fixture',
      reuseExistingServer: false,
      timeout: 45_000,
    },
    {
      command: 'cd .. && go run ./internal/web/testserver',
      url: 'http://127.0.0.1:4178',
      reuseExistingServer: false,
      timeout: 45_000,
    },
  ],
})
