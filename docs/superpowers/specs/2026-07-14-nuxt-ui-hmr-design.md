# Nuxt UI HMR Development Mode

**Date:** 2026-07-14

**Status:** Approved

## Goal

Provide a one-command frontend development workflow for the CouchFusion workbench. `go run . ui --dev` must serve the authenticated workbench through the Go server while Nuxt supplies live Vue and CSS updates through Vite HMR. The production `couchfusion ui` path must remain unchanged.

## Scope

This feature reloads only the Nuxt frontend. The Go server remains running while frontend modules recompile, so its launch token, session manager, API state, jobs, and managed site processes remain stable.

Automatic Go recompilation and restart are out of scope. Missing frontend dependencies are reported explicitly; development mode does not run `bun install` automatically.

## Architecture

Development mode keeps the Go server as the only browser-facing origin. It starts `bun run dev` as a managed child process on a separate random loopback port, then reverse-proxies authenticated non-API traffic to Nuxt. `/api/*` continues to use the existing Go handlers.

The reverse proxy forwards normal HTTP traffic and WebSocket upgrades. This is the supported Vite model for HMR behind a reverse proxy: <https://vite.dev/config/server-options#server-ws>.

```text
Browser -> Go server
  |-- /?token=... -> existing one-time token exchange
  |-- /api/*      -> existing authenticated Go API
  `-- other paths -> authenticated reverse proxy
                       |-- Nuxt pages and assets
                       `-- Vite HMR WebSocket
```

Normal UI mode continues to serve `internal/web/dist` from `go:embed`. Development mode does not generate or mutate embedded assets.

## Command-Line Interface

The existing `ui` command gains a boolean `--dev` flag:

```bash
go run . ui --dev
go run . ui --dev --no-open
go run . ui --dev --port 4400
```

Development mode expects a CouchFusion source checkout and resolves the frontend at `<current working directory>/web`. It validates `web/package.json` and `web/node_modules` before starting. If either is absent, the command exits with a specific instruction. Missing dependencies produce:

```text
Frontend dependencies are missing. Run: cd web && bun install
```

No browser-controlled input may select the frontend directory or proxy target.

## Components

### Development UI process

A focused process component owns the Nuxt child lifecycle. It:

- verifies the source directory, Bun executable, and installed dependencies;
- reserves a random `127.0.0.1` port and starts Nuxt with an explicit host, port, and strict-port behavior;
- forwards stdout and stderr with a `[nuxt]` prefix;
- polls the private Nuxt address until it becomes ready or the startup deadline expires;
- reports unexpected child termination to the UI command; and
- interrupts the child during shutdown, waits for a bounded grace period, then force-terminates it if required.

The component exposes readiness, completion, and shutdown behavior without depending on CLI presentation code.

### Development UI proxy

A proxy handler targets only the internally selected Nuxt loopback address. It forwards:

- page requests;
- Nuxt and Vite asset requests;
- development-tool requests; and
- WebSocket upgrade requests used by HMR.

The existing router continues to own token exchange, session authentication, API routing, host validation, origin enforcement, panic recovery, and outer security headers. Only authenticated non-API requests reach the development proxy.

### CLI integration

CLI wiring parses `--dev`, starts the development UI process before opening the browser, supplies the proxy handler to the web server, and coordinates shutdown. The public Go listener and tokenized launch URL work exactly as they do in normal mode.

If Nuxt exits unexpectedly, the command reports the failure and shuts down the Go server rather than leaving a nonfunctional proxy running.

## Security

Both listeners bind exclusively to `127.0.0.1`. The browser opens only the Go URL; the private Nuxt port is an implementation detail.

The production session and origin model remains intact:

- launch tokens remain random and single-use;
- session cookies remain HTTP-only and same-site strict;
- frontend requests require an authenticated session;
- `/api/*` mutations retain same-origin and JSON content-type validation; and
- production embedded responses retain their existing CSP behavior.

Development HTML requires special handling because it is generated dynamically by Nuxt. The proxy buffers only HTML responses, adds a fresh nonce to inline script tags, updates the response length, and emits a development CSP that permits the same-origin HMR WebSocket. Other response bodies and WebSocket traffic remain streamed.

The development relaxation is selected only by the explicit CLI flag and is never enabled by an HTTP request or production build setting.

## Lifecycle and errors

Startup fails before opening the browser when:

- the `web` source directory is invalid;
- frontend dependencies are missing;
- Bun is unavailable;
- Nuxt cannot bind the reserved loopback port;
- the Nuxt process exits during startup; or
- Nuxt does not become ready before the deadline.

Each failure includes an actionable message and cleans up any listener or child process already created.

On Ctrl-C, shutdown stops the Go HTTP server, the application services, and Nuxt. Shutdown remains bounded and must not leave an orphaned frontend process.

## Testing

Automated coverage will verify:

- parsing and propagation of `ui --dev`;
- source-directory, Bun, and dependency preflight errors;
- child startup, readiness, unexpected exit, graceful shutdown, and forced shutdown;
- authenticated HTTP proxying;
- rejection of unauthenticated frontend requests;
- continued direct routing of `/api/*` to Go;
- WebSocket upgrade forwarding;
- nonce injection and the development CSP for HTML;
- unchanged embedded-asset behavior in normal mode; and
- cleanup when startup or runtime components fail.

A browser smoke test will start development mode against an isolated frontend fixture, exchange the Go launch token, modify a temporary CSS fixture, and confirm that the page receives an HMR update without changing the Go address or losing its session.

## Acceptance criteria

- `go run . ui --dev` starts an authenticated, functional CouchFusion workbench from the source checkout.
- Editing a Vue component or `web/assets/css/main.css` updates the browser without regenerating `internal/web/dist`.
- The Go address, session, application state, API requests, SSE subscriptions, and managed processes survive frontend recompilation.
- The Nuxt listener is private and its lifecycle is owned by the UI command.
- Ctrl-C terminates Go and Nuxt without orphaned processes.
- `couchfusion ui` continues to serve the embedded production UI with no development behavior enabled.
