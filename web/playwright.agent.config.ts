import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: 'agent-component-generation.spec.ts',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  outputDir: 'output/playwright-agent',
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4178',
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: [
    {
      command: 'bun run dev -- --host 127.0.0.1 --port 4179',
      url: 'http://127.0.0.1:4179',
      reuseExistingServer: false,
      timeout: 45_000,
    },
    {
      command: 'cd .. && go build -o /tmp/couchfusion-agent-e2e-fake ./internal/agent/appserver/testdata/fake-server.go && COUCHFUSION_AGENT_E2E=1 COUCHFUSION_AGENT_FAKE=/tmp/couchfusion-agent-e2e-fake COUCHFUSION_TEST_UI_PROXY=http://127.0.0.1:4179 go run ./internal/web/testserver',
      url: 'http://127.0.0.1:4178',
      reuseExistingServer: false,
      timeout: 45_000,
    },
  ],
})
