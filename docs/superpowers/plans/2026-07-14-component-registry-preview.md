# Component Registry On-Demand Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicitly started, loopback-only Nuxt preview host and a fullscreen desktop/mobile component preview to the CouchFusion site registry.

**Architecture:** The Content layer supplies guarded preview-shell and render routes using extracted component-picker preview primitives. CouchFusion starts a distinct curated Nuxt dev process through a focused preview-host coordinator, exposes lifecycle APIs, and embeds the host shell in an accessible fullscreen overlay. The native render iframe retains the site's runtime and HMR behavior.

**Tech Stack:** Go 1.25, `net/http`, managed OS processes, Nuxt 4/Vue 3, TypeScript, Vitest/Vue Test Utils, Playwright, Bun.

## Global Constraints

- Preview starts only through an explicit **Start preview host** control and stops independently from the normal dev process.
- Bind the host to `127.0.0.1` on an automatically allocated port; allow one preview host per workspace/site.
- The browser may not supply an executable, arguments, directory, port, environment variable, token, or filesystem path.
- Register both Content preview routes only when `COUCHFUSION_PREVIEW=1` and require the per-process token on both routes.
- Render declared component defaults only; do not synthesize values and do not merge `previewProps` in version 1.
- Desktop fills available width; mobile is `375px`, shrinking only when the available canvas is narrower.
- Preserve native Nuxt JavaScript and CSS HMR inside the render iframe.
- Never expose the token as a standalone API field or in events, snapshots, logs, or user-facing errors.
- A host that does not report a verified URL within 60 seconds is stopped and marked failed.
- Do not modify or delete the pre-existing untracked `/Users/radu/Projects/nuxt-apps/cli-content/docs/integrate_inside_layer.md` file.

## File Structure

### Content layer repository: `/Users/radu/Projects/nuxt-apps/layers/content`

- `app/utils/component-preview.ts`: declared defaults and shared desktop/mobile model.
- `app/components/builder/PreviewDeviceToggle.vue`: accessible switcher used by both picker and preview shell.
- `app/pages/internal/ComponentPreviewShell.vue`: host toolbar and responsive render iframe.
- `app/pages/internal/ComponentPreviewRender.vue`: registry lookup and component-only rendering.
- `server/middleware/couchfusion-preview-guard.ts`: token gate for both preview paths.
- `utils/register-component-preview-pages.ts`: environment-gated page and middleware registration.
- `app/components/builder/ComponentPickerDialog.vue`: replace embedded device controls/default resolver with shared units.
- `nuxt.config.ts`: register the conditional preview module.
- `tests/builder/component-preview-primitives.spec.ts`: shared-default and device behavior tests.
- `tests/builder/component-preview-pages.spec.ts`: shell/render state tests.
- `tests/server/component-preview-registration.spec.ts`: flag and token-guard tests.

### CouchFusion repository: `/Users/radu/Projects/nuxt-apps/cli-init`

- `internal/project/tasks.go`: trusted environment overrides on resolved commands.
- `internal/processes/manager.go`: environment inheritance/copying and per-process value redaction.
- `internal/processes/manager_test.go`: environment and secret regression coverage.
- `internal/preview/manager.go`: preview lifecycle, token/port allocation, readiness, URL validation, and safe view.
- `internal/preview/manager_test.go`: deterministic coordinator tests.
- `internal/app/types.go`, `internal/app/service.go`, `internal/app/service_test.go`: application boundary and curated dev resolution.
- `internal/web/workspaces.go`, `internal/web/api_test.go`: status/start/stop endpoints.
- `internal/web/routes.go`, `internal/web/assets.go`, `internal/web/dev_proxy.go` and tests: narrow loopback frame CSP.
- `web/types/api.ts`: preview host API types.
- `web/components/sites/ComponentRegistryPanel.vue`: host controls and per-row Preview action.
- `web/components/sites/ComponentPreviewOverlay.vue`: accessible fullscreen wrapper.
- `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`: lifecycle loading/polling and overlay state.
- `web/assets/css/main.css`: fullscreen shell and compact control styling.
- `web/tests/site-detail.test.ts`: registry/host/overlay component behavior.
- `web/tests/e2e/component-preview.spec.ts`: browser acceptance and HMR proof.
- `docs/ui-server.md`: concise manual preview workflow.

---

### Task 1: Extract and Reuse Content Preview Primitives

**Files:**
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/app/utils/component-preview.ts`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/app/components/builder/PreviewDeviceToggle.vue`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/tests/builder/component-preview-primitives.spec.ts`
- Modify: `/Users/radu/Projects/nuxt-apps/layers/content/app/components/builder/ComponentPickerDialog.vue`

**Interfaces:**
- Produces: `PreviewDevice = 'desktop' | 'mobile'`, `MOBILE_PREVIEW_WIDTH = 375`, `declaredPreviewDefaults(definition): Record<string, unknown>`.
- Produces: `PreviewDeviceToggle` with `modelValue: PreviewDevice` and `update:modelValue`.
- Preserves: picker synthetic fallback values and `previewProps` merge.

- [ ] **Step 1: Write failing primitive and picker-regression tests**

```ts
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PreviewDeviceToggle from '../../app/components/builder/PreviewDeviceToggle.vue'
import { MOBILE_PREVIEW_WIDTH, declaredPreviewDefaults } from '../../app/utils/component-preview'

describe('component preview primitives', () => {
  it('returns only explicitly declared defaults, including falsy values', () => {
    expect(declaredPreviewDefaults({ id: 'hero', label: 'Hero', props: [
      { key: 'enabled', label: 'Enabled', type: 'boolean', default: false },
      { key: 'count', label: 'Count', type: 'number', default: 0 },
      { key: 'title', label: 'Title', type: 'text' },
    ] })).toEqual({ enabled: false, count: 0 })
  })

  it('shares the 375px mobile model and emits accessible changes', async () => {
    expect(MOBILE_PREVIEW_WIDTH).toBe(375)
    const wrapper = mount(PreviewDeviceToggle, { props: { modelValue: 'desktop' } })
    await wrapper.get('button[aria-label="Mobile preview"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['mobile'])
  })
})
```

Add a source-level regression assertion that `ComponentPickerDialog.vue` imports `declaredPreviewDefaults`, retains `previewProps`, and still supplies text/boolean/number/select/array/object fallbacks.

- [ ] **Step 2: Run the test and verify the missing imports fail**

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx vitest run tests/builder/component-preview-primitives.spec.ts`

Expected: FAIL because the utility and toggle do not exist.

- [ ] **Step 3: Implement the shared utility and switcher**

```ts
import type { ComponentDefinition } from '#content/app/types/builder'

export type PreviewDevice = 'desktop' | 'mobile'
export const MOBILE_PREVIEW_WIDTH = 375

export const declaredPreviewDefaults = (definition: ComponentDefinition) =>
  (definition.props || []).reduce<Record<string, unknown>>((values, prop) => {
    if (prop.default !== undefined) values[prop.key] = prop.default
    return values
  }, {})
```

Build `PreviewDeviceToggle.vue` from the existing desktop/mobile SVG buttons and styles. Use a button group labeled `Preview device`, `aria-pressed`, and emit only `desktop` or `mobile`. Replace both picker toggle blocks with this component. Rewrite picker `getDefaultProps` to begin with `declaredPreviewDefaults(def)` and fill only missing keys with its existing synthetic values before merging `def.previewProps || {}`.

- [ ] **Step 4: Run focused and existing picker tests**

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx vitest run tests/builder/component-preview-primitives.spec.ts tests/builder/component-picker-dialog-categories.spec.ts tests/builder/global-alias-defaults.spec.ts`

Expected: PASS.

- [ ] **Step 5: Commit the primitive extraction**

```bash
cd /Users/radu/Projects/nuxt-apps/layers/content
git add app/utils/component-preview.ts app/components/builder/PreviewDeviceToggle.vue app/components/builder/ComponentPickerDialog.vue tests/builder/component-preview-primitives.spec.ts
git commit -m "refactor: share component preview primitives"
```

### Task 2: Add Guarded Content Preview Pages

**Files:**
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/app/pages/internal/ComponentPreviewShell.vue`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/app/pages/internal/ComponentPreviewRender.vue`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/server/middleware/couchfusion-preview-guard.ts`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/utils/register-component-preview-pages.ts`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/tests/builder/component-preview-pages.spec.ts`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/tests/server/component-preview-registration.spec.ts`
- Modify: `/Users/radu/Projects/nuxt-apps/layers/content/nuxt.config.ts`

**Interfaces:**
- Consumes: `PreviewDeviceToggle`, `MOBILE_PREVIEW_WIDTH`, `declaredPreviewDefaults`, and `useComponentRegistry()`.
- Produces guarded routes `/__couchfusion/components/preview/:componentId` and `/__couchfusion/components/render/:componentId`.

- [ ] **Step 1: Write failing page and registration tests**

Mount the shell with route `{ params: { componentId: 'hero' }, query: { token: 'secret' } }`. Assert its iframe initially uses `width: 100%`, the mobile button changes it to `375px`, Refresh changes the iframe `key`, and the iframe URL forwards the encoded ID/token. Mount the render page with a stubbed registry and assert the selected component receives `{ enabled: false }`, not missing props or `previewProps`.

For registration, export pure helpers and assert:

```ts
expect(previewEnabled({ COUCHFUSION_PREVIEW: '1' })).toBe(true)
expect(previewEnabled({})).toBe(false)
expect(validPreviewRequest('/__couchfusion/components/render/hero', 'good', 'good')).toBe(true)
expect(validPreviewRequest('/__couchfusion/components/render/hero', 'bad', 'good')).toBe(false)
```

- [ ] **Step 2: Run and verify the tests fail**

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx vitest run tests/builder/component-preview-pages.spec.ts tests/server/component-preview-registration.spec.ts`

Expected: FAIL because pages, registration, and guard are missing.

- [ ] **Step 3: Implement conditional registration and token guard**

`register-component-preview-pages.ts` must return immediately unless `process.env.COUCHFUSION_PREVIEW === '1'`, then add exactly two page records in `pages:extend` and add the Nitro middleware handler. The guard applies only under `/__couchfusion/components/`, compares `getQuery(event).token` with `process.env.COUCHFUSION_PREVIEW_TOKEN` using `timingSafeEqual` on equal-length buffers, and throws `createError({ statusCode: 404 })` on any mismatch.

Add the module path to `nuxt.config.ts`; the module itself keeps routes absent when disabled.

- [ ] **Step 4: Implement shell and render pages**

The shell uses `PreviewDeviceToggle`, a refresh counter, and:

```ts
const frameWidth = computed(() => device.value === 'mobile'
  ? `${Math.min(MOBILE_PREVIEW_WIDTH, availableWidth.value)}px`
  : '100%')
const renderURL = computed(() => `/__couchfusion/components/render/${encodeURIComponent(componentId.value)}?token=${encodeURIComponent(token.value)}&refresh=${refresh.value}`)
```

Use `ResizeObserver` on the canvas to maintain `availableWidth`. The render page resolves `registry.lookup[id]`, returns a 404 state when absent, uses `previewComponentId?.trim() || id`, binds only `declaredPreviewDefaults(definition)`, and wraps the dynamic component in `NuxtErrorBoundary` with concise fallback copy.

- [ ] **Step 5: Run Content-layer verification**

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx vitest run tests/builder/component-preview-primitives.spec.ts tests/builder/component-preview-pages.spec.ts tests/server/component-preview-registration.spec.ts`

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx nuxi typecheck`

Expected: all tests and typecheck PASS.

- [ ] **Step 6: Commit guarded preview pages**

```bash
cd /Users/radu/Projects/nuxt-apps/layers/content
git add app/pages/internal app/components/builder/PreviewDeviceToggle.vue server/middleware/couchfusion-preview-guard.ts utils/register-component-preview-pages.ts nuxt.config.ts tests/builder/component-preview-pages.spec.ts tests/server/component-preview-registration.spec.ts
git commit -m "feat: add guarded component preview pages"
```

### Task 3: Support Trusted Process Environments and Runtime Secrets

**Files:**
- Modify: `internal/project/tasks.go`
- Modify: `internal/processes/manager.go`
- Modify: `internal/processes/manager_test.go`

**Interfaces:**
- Produces: `project.Command.Environment map[string]string`.
- Produces: `processes.StartRequest.SecretValues []string`.
- Guarantees inherited environment plus trusted overrides and exact-value event redaction.

- [ ] **Step 1: Write failing process tests**

Add a helper mode that prints `CF_TEST_ENV` and a raw secret. Start it with:

```go
Command: project.Command{
    Executable: os.Args[0],
    Args: []string{"-test.run=TestProcessHelper", "--", "environment"},
    Dir: root,
    Environment: map[string]string{"CF_TEST_ENV": "preview"},
},
SecretValues: []string{"raw-preview-secret"},
```

Assert the child sees `preview`, still sees an inherited sentinel, snapshots never contain `raw-preview-secret`, and mutating the original map/slice after Start does not change Restart behavior.

- [ ] **Step 2: Run and verify compile failure**

Run: `go test ./internal/processes -run 'Environment|RuntimeSecret' -count=1`

Expected: FAIL because the fields do not exist.

- [ ] **Step 3: Implement environment merge, copying, and redaction**

Set `command.Env = mergeEnvironment(os.Environ(), request.Command.Environment)`, where overrides replace matching keys and output is deterministically sorted. Extend `cloneRequest` to deep-copy the map and `SecretValues`. Give each `managedProcess` a redactor built from global key rules plus `SecretValues`; exact values are quoted regular expressions and replaced with `[redacted]`. Use the process redactor in `addEventLocked`.

- [ ] **Step 4: Run process tests and race tests**

Run: `go test -race ./internal/processes -count=1`

Expected: PASS.

- [ ] **Step 5: Commit process support**

```bash
git add internal/project/tasks.go internal/processes/manager.go internal/processes/manager_test.go
git commit -m "feat: support trusted process environments"
```

### Task 4: Build the Preview Host Coordinator

**Files:**
- Create: `internal/preview/manager.go`
- Create: `internal/preview/manager_test.go`

**Interfaces:**
- Produces: `preview.Status` values `stopped`, `starting`, `ready`, `failed`.
- Produces: `preview.View { Status, ProcessID, URL, StartedAt, Problem }`.
- Produces: `Start(ctx, workspaceID, siteID string, command project.Command) (View, error)`, `Status(ctx, workspaceID, siteID string) (View, error)`, `Stop(ctx, workspaceID, siteID string) (View, error)`.
- Uses fixed internal task ID `component-preview`.

- [ ] **Step 1: Write deterministic failing coordinator tests**

Use a fake process manager plus injected `AllocatePort func() (int, error)`, `NewToken func() (string, error)`, `Now func() time.Time`, and `ReadyTimeout`. Assert:

- Start adds only the four trusted overrides, passes the token in `SecretValues`, and uses task ID `component-preview`.
- A normal `script:dev` owner does not conflict with `component-preview`.
- repeated Start while starting/ready returns the same process.
- only `http://127.0.0.1:<allocated>/` or `http://localhost:<allocated>/` makes status ready.
- the returned URL is `origin + /__couchfusion/components/preview?token=...` without a standalone token field; component IDs are appended by the UI while retaining this query.
- terminal exit becomes failed; Stop is idempotent.
- the 60-second timer stops a process that never reports a URL.

- [ ] **Step 2: Run and verify the package is missing**

Run: `go test ./internal/preview -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the coordinator**

Store records behind a mutex keyed by `workspaceID + "\x00" + siteID`. Allocate a port by listening on `tcp4`, `127.0.0.1:0`, reading the assigned port, and closing the listener immediately before process Start. Generate 32 random bytes with `crypto/rand` and encode using `base64.RawURLEncoding`.

Clone the curated command, add the four environment overrides, and call:

```go
Processes.Start(ctx, processes.StartRequest{
    WorkspaceID: workspaceID,
    SiteID: siteID,
    TaskID: "component-preview",
    Command: command,
    SecretValues: []string{token},
})
```

Launch one readiness goroutine per record. Poll its snapshot without holding the coordinator mutex, accept only the allocated loopback origin, update ready/failed atomically, and stop at the deadline. Stop cancels readiness, stops the process, and returns `stopped` while retaining no tokenized active record.

- [ ] **Step 4: Run package and race tests**

Run: `go test -race ./internal/preview ./internal/processes -count=1`

Expected: PASS.

- [ ] **Step 5: Commit coordinator**

```bash
git add internal/preview
git commit -m "feat: manage component preview hosts"
```

### Task 5: Expose the Curated Preview API and Frame Policy

**Files:**
- Modify: `internal/app/types.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/service_test.go`
- Modify: `internal/web/workspaces.go`
- Modify: `internal/web/api_test.go`
- Modify: `internal/web/routes.go`
- Modify: `internal/web/assets.go`
- Modify: `internal/web/dev_proxy.go`
- Modify: `internal/web/server_test.go`
- Modify: `internal/web/dev_proxy_test.go`

**Interfaces:**
- Adds application methods `ComponentPreviewHost`, `StartComponentPreviewHost`, `StopComponentPreviewHost`.
- Adds GET status, POST start, and POST stop endpoints from the approved spec.

- [ ] **Step 1: Write failing service and HTTP tests**

In service tests, expose a site with and without `script:dev`. Assert Start resolves exactly `script:dev`, rejects a missing/non-long-running dev task, and passes the resolved command to the preview coordinator. Assert status/stop use only workspace/site IDs.

In API tests, add the three paths and assert envelopes contain `status`, optional `processId/url/startedAt/problem`, never `token`, and start/stop return HTTP 200. Add a test proving malformed request bodies remain rejected by the existing mutation middleware.

Update CSP expectations to include exactly:

```text
frame-src http://127.0.0.1:* http://localhost:*
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./internal/app ./internal/web -run 'ComponentPreview|CSP|SecurityHeaders' -count=1`

Expected: FAIL because methods/routes/policy are absent.

- [ ] **Step 3: Wire the application boundary**

Add a `ComponentPreviewHosts` interface matching Task 4 and dependency. `StartComponentPreviewHost` resolves the site, scans `Tasks(site)` for exactly `{ ID: "script:dev", LongRunning: true }`, resolves only that task, then calls the coordinator. Status and Stop first resolve the registered contained site, ensuring unknown or escaped site IDs cannot address coordinator records.

- [ ] **Step 4: Add API handlers and CSP**

Extend `Application` and route registration in `workspaces.go`. Handlers read path IDs only, call the corresponding method, and return `{ "data": view }`. Add the loopback `frame-src` directive to static, embedded-HTML, and development-proxy policies without changing `frame-ancestors 'none'`, `object-src 'none'`, script nonces, or HMR `connect-src`.

- [ ] **Step 5: Run full Go verification**

Run: `go test -race ./...`

Run: `go vet ./...`

Expected: PASS.

- [ ] **Step 6: Commit API and policy**

```bash
git add internal/app internal/web
git commit -m "feat: expose component preview host API"
```

### Task 6: Add Registry Controls and Fullscreen Preview UI

**Files:**
- Create: `web/components/sites/ComponentPreviewOverlay.vue`
- Modify: `web/components/sites/ComponentRegistryPanel.vue`
- Modify: `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`
- Modify: `web/types/api.ts`
- Modify: `web/assets/css/main.css`
- Modify: `web/tests/site-detail.test.ts`

**Interfaces:**
- Adds `ComponentPreviewHostStatus` and `ComponentPreviewHost` API types.
- Registry panel accepts `previewHost`, `previewBusy`, and `previewingId`; emits `start-preview-host`, `stop-preview-host`, and `preview(componentId, trigger)`.
- Overlay accepts `open`, `component`, and `url`; emits `close` and `restart-host`.

- [ ] **Step 1: Write failing panel and overlay tests**

Assert stopped shows Start and disables each Preview; starting shows Starting; ready shows Stop and enables Preview; failed shows problem plus Start. Assert Preview emits the component ID and originating element.

Mount the overlay and assert `role="dialog"`, `aria-modal="true"`, full-screen class, tokenized component URL, Escape close, close button, body scroll lock, and focus restoration on unmount/close. Simulate iframe load failure/host status transition and assert Close plus Start host remain usable.

- [ ] **Step 2: Run and verify component failure**

Run: `cd web && bun run test:run -- tests/site-detail.test.ts`

Expected: FAIL because types, props, events, and overlay are absent.

- [ ] **Step 3: Implement types, registry controls, and overlay**

```ts
export type ComponentPreviewHostStatus = 'stopped' | 'starting' | 'ready' | 'failed'
export interface ComponentPreviewHost {
  status: ComponentPreviewHostStatus
  processId?: string
  url?: string
  startedAt?: string
  problem?: string
}
```

Build the overlay with a `<Teleport to="body">`, native dialog semantics, a persistent CouchFusion close button above the iframe, `useEventListener(document, 'keydown', ...)`, and body overflow restoration. Construct the iframe URL without losing the capability query:

```ts
const base = new URL(previewHost.url)
const target = new URL(`/__couchfusion/components/preview/${encodeURIComponent(component.id)}`, base.origin)
target.search = base.search
return target.toString()
```

- [ ] **Step 4: Wire page lifecycle and polling**

Load preview status in the initial `Promise.allSettled`. Start and Stop call the dedicated endpoints. While status is `starting`, poll GET every 500ms; stop polling on ready/failed/unmount. If ready becomes stopped/failed while the overlay is open, keep the close path visible and show restart guidance. Do not place preview processes into the existing generic `activeProcess` task panel.

- [ ] **Step 5: Run UI verification**

Run: `cd web && bun run test:run -- tests/site-detail.test.ts`

Run: `cd web && bun run typecheck`

Run: `cd web && bun run generate`

Expected: PASS.

- [ ] **Step 6: Commit fullscreen UI**

```bash
git add web/types/api.ts web/components/sites/ComponentRegistryPanel.vue web/components/sites/ComponentPreviewOverlay.vue 'web/pages/workspaces/[workspaceId]/sites/[siteId].vue' web/assets/css/main.css web/tests/site-detail.test.ts
git commit -m "feat: add fullscreen component previews"
```

### Task 7: Prove the Real Preview and Document Manual Use

**Files:**
- Create: `web/tests/e2e/component-preview.spec.ts`
- Modify: `web/playwright.config.ts`
- Modify: `internal/web/acceptance_test.go`
- Modify: `docs/ui-server.md`

**Interfaces:**
- Produces browser acceptance for explicit start, fullscreen render, desktop/mobile sizing, concurrent dev ownership, CSS HMR, close, and stop.

- [x] **Step 1: Add the failing browser scenario**

Use the existing fixture workspace, adding a minimal Content-layer component and `dev` script if absent. The test must:

```ts
await page.getByRole('button', { name: 'Start preview host' }).click()
await expect(page.getByRole('button', { name: 'Stop preview host' })).toBeVisible()
await page.getByRole('button', { name: 'Preview Hero' }).click()
await expect(page.getByRole('dialog', { name: /Preview Hero/ })).toBeVisible()
const preview = page.frameLocator('[data-component-preview-frame]')
await preview.getByRole('button', { name: 'Mobile preview' }).click()
await expect.poll(async () => preview.locator('[data-render-frame]').evaluate(el => el.getBoundingClientRect().width)).toBe(375)
```

Then edit only a fixture CSS declaration, wait for the render frame's computed style to change without iframe navigation or server restart, restore the fixture in `finally`, close, and stop the host.

- [x] **Step 2: Run and verify the scenario fails before fixture/harness completion**

Run: `cd web && bun run e2e -- component-preview.spec.ts`

Expected: FAIL at the first unavailable preview behavior or fixture requirement.

- [x] **Step 3: Complete acceptance wiring and manual guide**

Extend the Go acceptance fixture to expose the adjacent Content layer and wait for preview cleanup. In `docs/ui-server.md`, add:

1. Start CouchFusion UI with its existing command.
2. Open a registered workspace and site.
3. Ensure the build-time registry is ready.
4. Click **Start preview host**.
5. Click **Preview** for a component.
6. Use Desktop/Mobile at the top; edit the component and observe HMR.
7. Close fullscreen preview and click **Stop preview host** when finished.

Include troubleshooting for missing `dev`, host timeout, invalid/stale registry, and Nuxt compile errors.

- [x] **Step 4: Run complete verification**

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx vitest run tests/builder/component-preview-primitives.spec.ts tests/builder/component-preview-pages.spec.ts tests/server/component-preview-registration.spec.ts`

Run: `cd /Users/radu/Projects/nuxt-apps/layers/content && bunx nuxi typecheck`

Run: `cd /Users/radu/Projects/nuxt-apps/cli-init && go test -race ./... && go vet ./...`

Run: `cd /Users/radu/Projects/nuxt-apps/cli-init/web && bun run test:run && bun run typecheck && bun run generate && bun run e2e -- component-preview.spec.ts`

Expected: every command PASS; browser test observes the CSS change through HMR.

Verification note (2026-07-14): Go race tests/vet, UI unit tests/typecheck/generate, all three browser suites, and the focused Content preview tests pass. A consuming-app `nuxi typecheck` was also run and remains non-green because of the Content/Database layers' pre-existing strict-TypeScript backlog outside the preview files.

- [x] **Step 5: Commit acceptance and documentation**

```bash
git add web/tests/e2e/component-preview.spec.ts web/playwright.config.ts internal/web/acceptance_test.go docs/ui-server.md
git commit -m "test: cover component preview workflow"
```

## Final Review

- [x] Confirm `git status --short` in both repositories contains no unintended changes.
- [x] Confirm JSON exposes the token only inside the required ready URL, never as a standalone field, and no event, log, or failure message contains `COUCHFUSION_PREVIEW_TOKEN` or its value.
- [x] Confirm ordinary `script:dev` and `component-preview` process owners run concurrently.
- [x] Confirm preview routes return 404 when the preview flag is absent or token is wrong.
- [x] Confirm the iframe is `375px` in mobile mode and native CSS HMR updates without rotating the CouchFusion UI token.
- [x] Use `superpowers:verification-before-completion` before reporting completion.
- [x] Use `superpowers:requesting-code-review` before branch handoff.
