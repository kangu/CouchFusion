import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  outputDir: 'output/playwright',
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4178',
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'cd .. && go run ./internal/web/testserver',
    url: 'http://127.0.0.1:4178',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
