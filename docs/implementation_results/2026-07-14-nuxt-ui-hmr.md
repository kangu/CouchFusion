# Nuxt UI HMR development mode

## Outcome

CouchFusion now supports a one-command frontend development workflow:

```bash
go run . ui --dev
```

The browser continues to use the tokenized, authenticated Go URL. Go starts Nuxt on a private random `127.0.0.1` port and proxies authenticated page, asset, development-tool, and Vite WebSocket requests to it. API and SSE requests remain handled directly by Go.

## Behavior

- Vue and CSS edits under `web/` update through Vite HMR without regenerating `internal/web/dist`.
- The Go listener, launch token, HTTP-only session, application state, jobs, and managed processes remain stable during frontend recompilation.
- `couchfusion ui` still serves the embedded production SPA and enables no development behavior.
- `--dev` requires the CouchFusion source checkout, Bun, `web/package.json`, and existing `web/node_modules`.
- Missing dependencies fail explicitly with `Frontend dependencies are missing. Run: cd web && bun install`.
- Ctrl-C stops both the Go server and its managed Nuxt child.

## Security

Both servers bind only to IPv4 loopback. The Nuxt target is selected internally and cannot be supplied through an HTTP request. Existing token exchange, session authentication, host validation, same-origin mutation validation, and API routing remain in the Go server.

Development HTML receives per-response CSP nonces. The development policy permits the same-origin HMR WebSocket, while non-HTML responses and WebSocket traffic remain streamed. Production CSP and embedded asset handling are unchanged.

## Coverage

- Authenticated frontend injection and direct Go API routing
- Loopback-only proxy targets
- Development HTML nonce rewriting and CSP
- Real WebSocket upgrade forwarding
- Source checkout, dependency, and Bun preflight errors
- Nuxt readiness, prefixed output, unexpected exit, timeout cleanup, and idempotent shutdown
- CLI `--dev` propagation and runtime failure reporting
- Application/development lifecycle composition
- Browser CSS HMR while preserving the Go URL and session
- Existing production-like control-plane browser journey

## Verification

The final gate completed successfully on 2026-07-14:

```text
go test -race ./... -count=1     PASS
go vet ./...                     PASS
bun run test:run                 7 files, 14 tests passed
bun run typecheck                PASS
bun run generate                 PASS
bun run e2e                      1 browser journey passed
bun run e2e:hmr                  1 HMR browser test passed
git diff --check                 PASS
```

A real `go run . ui --dev --no-open` smoke test started Go on port `51511` and Nuxt on private port `51512`. Changing `--display-page` from `4rem` to `3.99rem` updated the open browser through HMR, the browser stayed on port `51511`, and `/api/v1/bootstrap` returned `200`. The source edit was restored, Ctrl-C stopped the command, and no Nuxt child remained.
