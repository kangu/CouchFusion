import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: 'hmr.spec.ts',
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  outputDir: 'output/playwright-hmr',
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4178',
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: [
    {
      command: 'rm -rf output/hmr-fixture && cp -R ../internal/web/testdata/hmr output/hmr-fixture && bunx vite output/hmr-fixture --host 127.0.0.1 --port 4179 --strictPort',
      url: 'http://127.0.0.1:4179',
      reuseExistingServer: false,
      timeout: 30_000,
    },
    {
      command: 'cd .. && COUCHFUSION_TEST_UI_PROXY=http://127.0.0.1:4179 go run ./internal/web/testserver',
      url: 'http://127.0.0.1:4178',
      reuseExistingServer: false,
      timeout: 30_000,
    },
  ],
})
