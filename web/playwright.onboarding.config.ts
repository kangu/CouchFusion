import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: 'onboarding.spec.ts',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  outputDir: 'output/playwright-onboarding',
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4178',
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'cd .. && go build -o /tmp/couchfusion-onboarding-e2e-fake ./internal/agent/appserver/testdata/fake-server.go && COUCHFUSION_ONBOARDING_E2E=1 COUCHFUSION_AGENT_E2E=1 COUCHFUSION_AGENT_FAKE=/tmp/couchfusion-onboarding-e2e-fake go run ./internal/web/testserver',
    url: 'http://127.0.0.1:4178',
    reuseExistingServer: false,
    timeout: 45_000,
  },
})
