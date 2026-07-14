# Nuxt UI HMR Development Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `couchfusion ui --dev`, which keeps the authenticated Go control plane stable while proxying the Nuxt development server and its HMR WebSocket through the same loopback origin.

**Architecture:** The web server accepts an optional authenticated frontend handler; normal mode uses embedded assets, while development mode supplies a nonce-aware reverse proxy to a managed Nuxt child on a private loopback port. A dedicated `internal/devui` package owns preflight, readiness, logging, unexpected-exit reporting, and bounded shutdown. CLI wiring selects the mode without weakening production behavior.

**Tech Stack:** Go 1.24 standard library (`net/http/httputil`, `os/exec`), Nuxt 4.4, Vite 7.3, Bun, Vitest, Playwright.

## Global Constraints

- `couchfusion ui` must retain the existing embedded production UI and security behavior.
- Development mode is enabled only by the explicit `--dev` CLI flag.
- Go remains the only browser-facing origin; both Go and Nuxt bind only to `127.0.0.1`.
- `/api/*`, SSE, token exchange, session cookies, host checks, and mutation-origin checks remain owned by Go.
- Missing `web/node_modules` must fail with `Frontend dependencies are missing. Run: cd web && bun install`; never install automatically.
- Development mode reloads Nuxt/CSS only. Automatic Go recompilation is out of scope.
- Nuxt child processes must be stopped during every success, failure, and signal-driven shutdown path.

---

## File map

- Create `internal/web/dev_proxy.go`: authenticated frontend reverse proxy, HTML nonce rewriting, development CSP, HTTP streaming, and WebSocket upgrades.
- Create `internal/web/dev_proxy_test.go`: HTTP, API-boundary, nonce, and upgrade coverage.
- Modify `internal/web/server.go`: accept an optional `FrontendHandler` in `Options`.
- Modify `internal/web/routes.go`: select the injected frontend handler after authentication.
- Create `internal/devui/process.go`: source preflight, Bun/Nuxt lifecycle, readiness, output prefixing, completion, and shutdown.
- Create `internal/devui/process_test.go`: helper-process tests for validation and lifecycle behavior.
- Modify `internal/cli/ui.go`: parse `--dev`, pass it into the factory, and observe optional runtime completion.
- Modify `internal/cli/ui_test.go`: CLI flag and unexpected-runtime-exit tests.
- Create `ui_runtime.go`: compose the UI application service with an optional development process.
- Create `ui_runtime_test.go`: composition and shutdown-order tests.
- Modify `main.go`: start the development child, install the proxy, and update usage.
- Modify `web/nuxt.config.ts`: enforce strict Vite port selection in development.
- Modify `internal/web/testserver/main.go`: optionally inject a test proxy from a process environment variable.
- Create `internal/web/testdata/hmr/index.html` and `internal/web/testdata/hmr/style.css`: minimal isolated Vite HMR fixture.
- Create `web/e2e/hmr.spec.ts`: browser-level same-origin HMR and session test.
- Create `web/playwright.hmr.config.ts`: start the isolated Vite fixture and authenticated Go proxy.
- Modify `web/package.json`: add `e2e:hmr`.
- Modify `README.md`: document the development command and explicit install prerequisite.

---

### Task 1: Inject and proxy the authenticated frontend

**Files:**
- Create: `internal/web/dev_proxy.go`
- Create: `internal/web/dev_proxy_test.go`
- Modify: `internal/web/server.go`
- Modify: `internal/web/routes.go`
- Test: `internal/web/server_test.go`

**Interfaces:**
- Consumes: `randomToken(io.Reader)`, the existing authenticated route boundary, and a trusted internal `*url.URL`.
- Produces: `func NewDevelopmentProxy(target *url.URL, random io.Reader) (http.Handler, error)` and `web.Options.FrontendHandler http.Handler`.

- [ ] **Step 1: Write failing frontend-injection and HTML-security tests**

Add tests that construct an upstream `httptest.Server`, inject the proxy, authenticate through the existing launch-token helper, and verify that API traffic never reaches the frontend:

```go
type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) {
	clear(value)
	return len(value), nil
}

func TestDevelopmentFrontendRequiresSessionAndLeavesAPIWithGo(t *testing.T) {
	frontendCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frontendCalls++
		_, _ = io.WriteString(w, "<!doctype html><script>window.dev=true</script>")
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	proxy, err := NewDevelopmentProxy(target, zeroReader{})
	if err != nil { t.Fatal(err) }
	server, err := NewServer(Options{FrontendHandler: proxy})
	if err != nil { t.Fatal(err) }

	unauthenticated := performRequest(server, http.MethodGet, "/", nil, nil)
	if unauthenticated.Code != http.StatusUnauthorized || frontendCalls != 0 {
		t.Fatalf("unauthenticated = %d, calls = %d", unauthenticated.Code, frontendCalls)
	}
	cookie := authenticatedCookie(t, server)
	frontend := performRequest(server, http.MethodGet, "/workspace", nil, cookie)
	if frontend.Code != http.StatusOK || frontendCalls != 1 {
		t.Fatalf("frontend = %d, calls = %d", frontend.Code, frontendCalls)
	}
	api := performRequest(server, http.MethodGet, "/api/v1/bootstrap", nil, cookie)
	if api.Code != http.StatusOK || frontendCalls != 1 {
		t.Fatalf("api = %d, calls = %d", api.Code, frontendCalls)
	}
}

func TestDevelopmentProxyAddsNonceWithoutBufferingCSS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/style.css" {
			w.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(w, "body{color:red}")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><script type=\"module\">dev()</script>")
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	proxy, _ := NewDevelopmentProxy(target, zeroReader{})
	server, _ := NewServer(Options{FrontendHandler: proxy, Random: zeroReader{}})
	cookie := authenticatedCookie(t, server)
	html := performRequest(server, http.MethodGet, "/", nil, cookie)
	match := regexp.MustCompile(`script-src 'self' 'nonce-([^']+)'`).FindStringSubmatch(html.Header().Get("Content-Security-Policy"))
	if len(match) != 2 || !strings.Contains(html.Body.String(), `nonce="`+match[1]+`"`) {
		t.Fatalf("CSP = %q, body = %s", html.Header().Get("Content-Security-Policy"), html.Body.String())
	}
	if !strings.Contains(html.Header().Get("Content-Security-Policy"), "connect-src 'self' ws:") {
		t.Fatalf("CSP does not allow same-origin HMR: %q", html.Header().Get("Content-Security-Policy"))
	}
	css := performRequest(server, http.MethodGet, "/style.css", nil, cookie)
	if css.Body.String() != "body{color:red}" { t.Fatalf("CSS = %q", css.Body.String()) }
}
```

- [ ] **Step 2: Run the web tests and verify the red state**

Run: `go test ./internal/web -run 'TestDevelopment' -count=1`

Expected: FAIL because `Options.FrontendHandler` and `NewDevelopmentProxy` do not exist.

- [ ] **Step 3: Add the injectable frontend boundary**

Add to `internal/web/server.go`:

```go
type Options struct {
	Port            int
	MaxBodyBytes    int64
	APIHandler      http.Handler
	FrontendHandler http.Handler
	Application     Application
	Listener        net.Listener
	Random          io.Reader
}
```

In `routes()`, retain `staticHandler()` as the default and select the trusted injected handler only when non-nil:

```go
frontend := staticHandler()
if s.options.FrontendHandler != nil {
	frontend = s.options.FrontendHandler
}
```

After authentication and API dispatch, call `frontend.ServeHTTP(writer, request)`.

- [ ] **Step 4: Implement the nonce-aware reverse proxy**

Create `NewDevelopmentProxy` with `httputil.ReverseProxy`. Reject nil or non-loopback targets. Use `Rewrite` to set the target URL, preserve forwarding headers, and remove `Accept-Encoding` so HTML can be safely rewritten. In `ModifyResponse`, return immediately for non-HTML content; for HTML, read and close the body, generate a nonce, apply it to every `<script` tag, replace the body and content length, delete stale `ETag` and `Content-Encoding`, and set:

```go
const developmentCSP = "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'; connect-src 'self' ws:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'nonce-%s'"
```

Return proxy failures as `502 Bad Gateway` without exposing the target address.

- [ ] **Step 5: Add a real WebSocket-upgrade forwarding test**

Use two real loopback `httptest.Server` instances. The upstream handler must hijack an upgrade request, return `101 Switching Protocols`, read `ping`, and write `pong`. Dial the authenticated Go server with `net.Dial`, include the session cookie plus upgrade headers, then assert the `101` response and `pong` payload. This proves the standard-library proxy preserves the HMR transport rather than testing an HTTP approximation.

- [ ] **Step 6: Run and commit Task 1**

Run:

```bash
gofmt -w internal/web/dev_proxy.go internal/web/dev_proxy_test.go internal/web/server.go internal/web/routes.go
go test ./internal/web -count=1
git diff --check
```

Expected: all `internal/web` tests PASS.

Commit:

```bash
git add internal/web/dev_proxy.go internal/web/dev_proxy_test.go internal/web/server.go internal/web/routes.go internal/web/server_test.go
git commit -m "feat: proxy authenticated Nuxt development UI"
```

---

### Task 2: Manage the Nuxt development process

**Files:**
- Create: `internal/devui/process.go`
- Create: `internal/devui/process_test.go`
- Modify: `web/nuxt.config.ts`

**Interfaces:**
- Consumes: a source checkout root, Bun on `PATH`, `web/package.json`, and `web/node_modules`.
- Produces:

```go
type Options struct {
	Root           string
	Out            io.Writer
	Err            io.Writer
	StartupTimeout time.Duration
	StopGrace      time.Duration
}

func Start(context.Context, Options) (*Process, error)
func (p *Process) URL() *url.URL
func (p *Process) Done() <-chan error
func (p *Process) Shutdown(context.Context) error
```

- [ ] **Step 1: Write failing preflight tests**

Add table-driven tests for the exact source and dependency errors, using a temporary root and an internal `lookPath` dependency overridden from the same package test:

```go
func TestStartValidatesFrontendCheckout(t *testing.T) {
	tests := []struct {
		name string
		prepare func(string)
		want string
	}{
		{name: "missing web package", prepare: func(string) {}, want: "development mode must run from the CouchFusion source checkout"},
		{name: "missing dependencies", prepare: func(root string) {
			if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil { t.Fatal(err) }
			if err := os.WriteFile(filepath.Join(root, "web", "package.json"), []byte(`{"scripts":{"dev":"nuxt dev"}}`), 0o644); err != nil { t.Fatal(err) }
		}, want: "Frontend dependencies are missing. Run: cd web && bun install"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.prepare(root)
			_, err := Start(context.Background(), Options{Root: root})
			if err == nil || !strings.Contains(err.Error(), test.want) { t.Fatalf("error = %v", err) }
		})
	}
}
```

- [ ] **Step 2: Run the package test and verify failure**

Run: `go test ./internal/devui -count=1`

Expected: FAIL because `internal/devui` and `Start` do not exist.

- [ ] **Step 3: Implement preflight, port reservation, and startup**

In `Start`:

1. Default `Root` to `os.Getwd()` and output writers to `io.Discard`.
2. Verify `web/package.json` is a regular file and `web/node_modules` is a directory.
3. Resolve `bun` with `exec.LookPath` and return `Bun is required for UI development mode` when absent.
4. Reserve an IPv4 loopback port with `net.Listen("tcp4", "127.0.0.1:0")`, capture it, and close the reservation immediately before starting the child.
5. Start `bun run dev -- --host 127.0.0.1 --port <port>` with `Dir` set to `web`.
6. Prefix stdout and stderr line-by-line with `[nuxt] `.
7. Poll `http://127.0.0.1:<port>/` with a short-timeout client until any HTTP response arrives, the child exits, the parent context is cancelled, or the startup timeout expires.

Keep injectable package variables for `lookPath`, `listen`, `commandContext`, and `httpClient` so unit tests can use the Go test binary as a controlled child without shell scripts.

- [ ] **Step 4: Write lifecycle tests with a helper subprocess**

Use the standard Go helper-process pattern. The helper reads the requested port from `COUCHFUSION_DEVUI_HELPER_PORT`, serves loopback HTTP, prints `ready`, and either waits for interruption or exits immediately based on `COUCHFUSION_DEVUI_HELPER_MODE`.

Verify:

```go
func TestProcessBecomesReadyAndShutsDown(t *testing.T) {
	process := startHelperProcess(t, "serve")
	if process.URL().Hostname() != "127.0.0.1" { t.Fatalf("URL = %s", process.URL()) }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := process.Shutdown(ctx); err != nil { t.Fatal(err) }
	select {
	case err := <-process.Done():
		if err != nil { t.Fatalf("done = %v", err) }
	case <-ctx.Done():
		t.Fatal("process did not finish")
	}
}

func TestProcessReportsUnexpectedExit(t *testing.T) {
	process := startHelperProcess(t, "exit-after-ready")
	err := <-process.Done()
	if err == nil || !strings.Contains(err.Error(), "Nuxt development server exited unexpectedly") {
		t.Fatalf("done = %v", err)
	}
}
```

Also verify startup timeout cleans up the helper and that two calls to `Shutdown` are safe.

- [ ] **Step 5: Configure strict Vite port behavior**

Add to `web/nuxt.config.ts`:

```ts
vite: {
  server: {
    strictPort: true,
  },
},
```

The reverse proxy forwards the default Vite WebSocket path, so no separate public HMR port or host is configured.

- [ ] **Step 6: Run and commit Task 2**

Run:

```bash
gofmt -w internal/devui/process.go internal/devui/process_test.go
go test ./internal/devui -count=1
cd web && bun run typecheck && cd ..
git diff --check
```

Expected: process tests and Nuxt typechecking PASS.

Commit:

```bash
git add internal/devui/process.go internal/devui/process_test.go web/nuxt.config.ts
git commit -m "feat: manage Nuxt UI development process"
```

---

### Task 3: Wire `ui --dev` through the CLI

**Files:**
- Modify: `internal/cli/ui.go`
- Modify: `internal/cli/ui_test.go`
- Create: `ui_runtime.go`
- Create: `ui_runtime_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: `devui.Start`, `devui.Process`, `web.NewDevelopmentProxy`, and `web.Options.FrontendHandler`.
- Produces: `UIOptions.NewServer func(net.Listener, string, bool) (UIServer, UIApplication, error)` and optional runtime completion through:

```go
type UICompletion interface {
	Done() <-chan error
}
```

- [ ] **Step 1: Write failing CLI tests for the development flag and runtime exit**

Update existing factories with the new boolean parameter, then add:

```go
func TestUIPropagatesDevelopmentMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := &uiServerFake{launch: webserver.LaunchInfo{URL: "http://127.0.0.1:4400/?token=x"}, started: cancel}
	development := false
	err := RunUI(ctx, []string{"--dev", "--no-open"}, IO{}, UIOptions{
		Listen: func(_, _ string) (net.Listener, error) { return loopbackListener(t), nil },
		NewServer: func(_ net.Listener, _ string, dev bool) (UIServer, UIApplication, error) {
			development = dev
			return server, &uiApplicationFake{}, nil
		},
	})
	if err != nil || !development { t.Fatalf("err = %v, development = %v", err, development) }
}

func TestUIReturnsUnexpectedRuntimeFailure(t *testing.T) {
	runtimeDone := make(chan error, 1)
	runtimeDone <- errors.New("Nuxt development server exited unexpectedly")
	close(runtimeDone)
	application := &uiApplicationFake{done: runtimeDone}
	server := &uiServerFake{launch: webserver.LaunchInfo{URL: "http://127.0.0.1:4400/?token=x"}}
	err := RunUI(context.Background(), []string{"--dev", "--no-open"}, IO{}, UIOptions{
		Listen: func(_, _ string) (net.Listener, error) { return loopbackListener(t), nil },
		NewServer: func(net.Listener, string, bool) (UIServer, UIApplication, error) { return server, application, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "Nuxt development server exited unexpectedly") {
		t.Fatalf("error = %v", err)
	}
	if server.shutdownCalls != 1 || application.shutdownCalls != 1 {
		t.Fatalf("shutdown = server %d application %d", server.shutdownCalls, application.shutdownCalls)
	}
}
```

- [ ] **Step 2: Run the CLI tests and verify failure**

Run: `go test ./internal/cli -run 'TestUI' -count=1`

Expected: FAIL because `--dev`, the factory parameter, and completion selection are absent.

- [ ] **Step 3: Implement CLI propagation and completion selection**

Parse `dev := flags.Bool("dev", false, "Use the Nuxt development server with HMR")`, pass `*dev` to `NewServer`, and wait with:

```go
var runtimeDone <-chan error
if completion, ok := application.(UICompletion); ok {
	runtimeDone = completion.Done()
}
var runtimeErr error
select {
case <-ctx.Done():
case runtimeErr = <-runtimeDone:
}
shutdownErr := errors.Join(server.Shutdown(shutdownContext), application.Shutdown(shutdownContext))
return errors.Join(runtimeErr, shutdownErr)
```

A nil completion channel disables the runtime branch in normal mode.

- [ ] **Step 4: Write and implement the root runtime composition tests**

Create `uiRuntime` with `application cli.UIApplication` and `development *devui.Process`. Its `Done` method returns nil without a development child. Its `Shutdown` method stops the development child before the application and joins both errors.

Test a lightweight internal constructor that accepts interfaces rather than requiring a real process:

```go
type developmentRuntime interface {
	Done() <-chan error
	Shutdown(context.Context) error
}

type uiRuntime struct {
	application cli.UIApplication
	development developmentRuntime
}
```

Verify normal mode returns a nil `Done` channel and development mode invokes both shutdown methods once.

- [ ] **Step 5: Wire development startup in `main.go`**

Change the UI factory to receive `development bool`. Create the application service first. When development is true:

1. call `os.Getwd()`;
2. start `devui.Start(ctx, devui.Options{Root: root, Out: streams.Out, Err: streams.Err})`;
3. create `webserver.NewDevelopmentProxy(process.URL(), nil)`;
4. pass the proxy as `webserver.Options.FrontendHandler`; and
5. return `uiRuntime{application: uiApplication, development: process}`.

Every error after process startup must call its bounded shutdown before returning. In normal mode, return the same runtime wrapper without a child. Update usage to:

```text
couchfusion ui [--port 0] [--no-open] [--config path] [--dev]
```

- [ ] **Step 6: Run and commit Task 3**

Run:

```bash
gofmt -w internal/cli/ui.go internal/cli/ui_test.go ui_runtime.go ui_runtime_test.go main.go
go test ./internal/cli ./internal/devui ./internal/web . -count=1
go vet ./...
git diff --check
```

Expected: targeted Go tests and vet PASS.

Commit:

```bash
git add internal/cli/ui.go internal/cli/ui_test.go ui_runtime.go ui_runtime_test.go main.go
git commit -m "feat: add CouchFusion UI development mode"
```

---

### Task 4: Prove HMR through the authenticated Go origin

**Files:**
- Modify: `internal/web/testserver/main.go`
- Create: `internal/web/testdata/hmr/index.html`
- Create: `internal/web/testdata/hmr/style.css`
- Create: `web/e2e/hmr.spec.ts`
- Create: `web/playwright.hmr.config.ts`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: `web.NewDevelopmentProxy` and the deterministic testserver launch token `AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA`.
- Produces: `bun run e2e:hmr`, an isolated browser smoke test that never edits tracked source files.

- [ ] **Step 1: Create the isolated HMR fixture**

Add `internal/web/testdata/hmr/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <link rel="stylesheet" href="/style.css">
    <title>CouchFusion HMR Probe</title>
  </head>
  <body><main id="probe">HMR ready</main></body>
</html>
```

Add `style.css`:

```css
#probe { color: rgb(17, 34, 51); }
```

The Playwright startup command copies these files into ignored `web/output/hmr-fixture/`; the test modifies only that copy.

- [ ] **Step 2: Write the failing HMR browser test and config**

Create `web/e2e/hmr.spec.ts`:

```ts
import { expect, test } from '@playwright/test'
import { writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const launchToken = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'

test('keeps the Go session while Vite hot-reloads CSS', async ({ page }) => {
  await page.goto(`/?token=${launchToken}`)
  const probe = page.locator('#probe')
  await expect(probe).toHaveCSS('color', 'rgb(17, 34, 51)')

  const bootstrap = await page.request.get('/api/v1/bootstrap')
  expect(bootstrap.ok()).toBe(true)

  await writeFile(resolve('output/hmr-fixture/style.css'), '#probe { color: rgb(68, 85, 102); }\n')
  await expect(probe).toHaveCSS('color', 'rgb(68, 85, 102)')

  const afterUpdate = await page.request.get('/api/v1/bootstrap')
  expect(afterUpdate.ok()).toBe(true)
  expect(page.url()).toBe('http://127.0.0.1:4178/')
})
```

Create a dedicated Playwright config with one worker, the existing output directory, and two web servers:

```ts
export default defineConfig({
  testDir: './e2e',
  testMatch: 'hmr.spec.ts',
  timeout: 30_000,
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:4178', browserName: 'chromium', trace: 'retain-on-failure' },
  webServer: [
    {
      command: 'rm -rf output/hmr-fixture && cp -R ../internal/web/testdata/hmr output/hmr-fixture && bunx vite output/hmr-fixture --host 127.0.0.1 --port 4179 --strictPort',
      url: 'http://127.0.0.1:4179',
      timeout: 30_000,
    },
    {
      command: 'cd .. && COUCHFUSION_TEST_UI_PROXY=http://127.0.0.1:4179 go run ./internal/web/testserver',
      url: 'http://127.0.0.1:4178',
      timeout: 30_000,
    },
  ],
})
```

Add `"e2e:hmr": "playwright test --config playwright.hmr.config.ts"` to `web/package.json`.

- [ ] **Step 3: Run the browser test and verify the red state**

Run: `cd web && bun run e2e:hmr`

Expected before testserver proxy wiring is complete: FAIL because the authenticated Go origin serves the embedded workbench instead of `#probe`.

- [ ] **Step 4: Add opt-in development proxying to the fixture server**

Read `COUCHFUSION_TEST_UI_PROXY`. When non-empty, parse it, create `NewDevelopmentProxy`, and pass it as `Options.FrontendHandler`. Panic on an invalid test-only value. With the environment variable absent, preserve the existing embedded E2E server behavior.

- [ ] **Step 5: Verify the browser suites are green**

Run:

```bash
gofmt -w internal/web/testserver/main.go
cd web
bun run e2e:hmr
bun run e2e
```

Expected: both the HMR smoke test and existing full control-plane journey PASS.

- [ ] **Step 6: Commit Task 4**

```bash
git add internal/web/testserver/main.go internal/web/testdata/hmr web/e2e/hmr.spec.ts web/playwright.hmr.config.ts web/package.json
git commit -m "test: verify authenticated UI hot reload"
```

---

### Task 5: Document and release-gate development mode

**Files:**
- Modify: `README.md`
- Create: `docs/implementation_results/2026-07-14-nuxt-ui-hmr.md`
- Modify: `docs/superpowers/plans/2026-07-14-nuxt-ui-hmr.md`

**Interfaces:**
- Consumes: the completed `ui --dev` behavior and verification commands.
- Produces: a concise operator guide and recorded verification evidence.

- [ ] **Step 1: Add the development workflow to README**

Immediately after the normal UI command documentation, add:

```markdown
#### Develop the workbench UI with HMR

From the CouchFusion source checkout:

```bash
cd web
bun install
cd ..
go run . ui --dev
```

The browser still connects to the authenticated Go server. Go forwards page, asset, and HMR WebSocket traffic to a managed Nuxt development server, so edits under `web/` update without rebuilding `internal/web/dist`. This mode does not reload Go code. Stop it with Ctrl-C to terminate both servers.
```

Add troubleshooting entries for missing dependencies and an unexpectedly occupied Nuxt port.

- [ ] **Step 2: Run the complete verification gate**

Run:

```bash
go test -race ./... -count=1
go vet ./...
cd web
bun run test:run
bun run typecheck
bun run generate
bun run e2e
bun run e2e:hmr
cd ..
git diff --check
```

Expected: all Go tests pass under the race detector, vet is clean, 14 frontend unit tests pass, Nuxt typechecking and generation pass, and both Playwright suites pass.

- [ ] **Step 3: Manually smoke-test the real command**

Run `go run . ui --dev --no-open`, open the printed tokenized URL, edit one value in `web/assets/css/main.css`, and verify the browser updates without changing its Go port. Restore the CSS edit, press Ctrl-C, and confirm no matching Nuxt child remains with `pgrep -fal 'nuxt dev'`.

- [ ] **Step 4: Record implementation results**

Create `docs/implementation_results/2026-07-14-nuxt-ui-hmr.md` containing:

- the user-visible `ui --dev` command;
- the single-origin proxy architecture;
- explicit dependency and source-checkout requirements;
- security behavior retained from production;
- files and tests added; and
- the exact verification results from Step 2.

- [ ] **Step 5: Mark this plan complete and commit**

Mark every completed checkbox in this plan, then run:

```bash
git add README.md docs/implementation_results/2026-07-14-nuxt-ui-hmr.md docs/superpowers/plans/2026-07-14-nuxt-ui-hmr.md
git commit -m "docs: ship Nuxt UI development mode"
git status --short
```

Expected: the documentation commit succeeds and the working tree is clean.
