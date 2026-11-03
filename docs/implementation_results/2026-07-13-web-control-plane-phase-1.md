# Web Control Plane Phase 1 — Implementation Result

**Completed:** 2026-07-14
**Release:** 0.6.0

## Delivered

CouchFusion now ships a local web control plane without replacing the existing CLI/TUI. `couchfusion ui` starts a loopback-only Go server, opens a tokenized browser URL, serves an embedded Nuxt 4 SPA, and coordinates graceful shutdown of HTTP traffic, finite jobs, and managed site processes.

The Go application layer is shared by CLI and HTTP adapters. It owns the explicit workspace registry, project inspection, layered configuration, provisioning, health checks, curated task resolution, bounded job history, managed process lifecycle, redaction, and shutdown. The browser consumes typed `/api/v1` JSON resources and reconnectable SSE streams.

User-visible phase-1 flows include:

- Explicitly register, initialize, select, inspect, relink through the API, rename through the API, and forget workspace references.
- Inspect sites and layers; forgetting a workspace never deletes files.
- Create sites and layers through reviewed jobs.
- View effective layered configuration and write reviewed workspace overrides.
- Check Git, Bun, Node.js, and CouchDB independently; managed repair requires confirmation.
- Run exact catalogued package tasks as finite jobs.
- Start, stop, and restart catalogued development processes with bounded logs and advisory loopback URLs.
- Review session jobs/processes, reconnect event streams, and cancel active jobs.

## Security and lifecycle controls

- The listener binds only to `127.0.0.1`; foreign `Host` and mutation `Origin` values are rejected.
- A random 256-bit one-time launch token is exchanged for an HTTP-only, same-site session cookie and removed from the URL.
- API mutation bodies require JSON, reject unknown fields, and are size bounded.
- Embedded HTML receives a per-response CSP nonce; executable inline JavaScript is not enabled with `unsafe-inline`.
- API DTOs omit commands and credentials. Logs/errors redact credential URLs, environment-style secrets, authorization headers, cookies, and URL query secrets.
- Site IDs are resolved beneath registered roots; traversal and symlink escapes are rejected.
- The browser cannot submit arbitrary executables, arguments, working directories, shell fragments, delete operations, or force-overwrite controls.
- Closing a tab unsubscribes from activity only; jobs continue. Server shutdown cancels jobs and gracefully stops all managed child processes.

## Build and release

`build.sh` now installs the locked frontend dependencies, runs frontend tests and typechecking, generates the SPA, replaces `internal/web/dist`, asserts the embedded marker, runs Go tests, and cross-compiles five single-binary targets. `release.sh` rejects missing or non-embedded binaries and verifies every expected archive is non-empty.

Targets are Linux amd64/arm64, macOS amd64/arm64, and Windows amd64.

## Verification

The implementation was verified with:

```bash
go test -race ./... -count=1
go vet ./...
cd web && bun run test:run
cd web && bun run typecheck
cd web && bun run generate
cd web && bun run e2e
./build.sh
./release.sh v0.6.0
```

The Chromium acceptance test exercises launch-token exchange, explicit registration, workspace inspection, reviewed site provisioning, a finite task, managed process start/URL/stop, configuration review/save, and non-destructive forget against the real embedded SPA and HTTP security stack.

## Deferred

Phase 2 remains CouchDB database, user, permission, configuration, health-maintenance, and document administration. Phase 3 remains CouchFusion-aware content/page editing, preview, validation, and publishing.

Remote/LAN hosting, multiple users or roles, filesystem crawling, destructive site/layer/workspace deletion, arbitrary browser shell execution, persistent daemons, persisted job history, and a third-party plugin runtime remain intentionally out of scope.
