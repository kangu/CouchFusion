# Codex-Assisted Component Generation: Implementation Results

Date: 2026-07-14
Branch: `ui`
Status: Complete

## Outcome

CouchFusion can now start and retain separate Codex-assisted component-generation conversations from a site's component registry. A conversation runs against the selected site's working directory through the locally installed Codex session, asks clarifying questions in the CouchFusion UI, requires explicit approval for file changes and protected operations, preserves the site's pre-existing dirty state, rebuilds the component registry after successful turns, and leaves review and Git operations to the user.

Conversation metadata survives CouchFusion restarts in the dedicated `couchfusion-control` CouchDB database. Preview remains a separate, manually started action.

## Delivered Tasks

All 16 tasks in the approved plan were implemented:

1. Shared workspace mutation leases.
2. Versioned app-server protocol contract and JSONL JSON-RPC client.
3. Codex app-server supervision and readiness reporting.
4. Minimal persistent conversation metadata.
5. Dirty-work baseline capture and comparison.
6. Embedded content-component skill and prompt policy.
7. Conversation state machine and event stream.
8. Questions, one-generation approvals, and protected consents.
9. Changed-file reporting and automatic registry rebuild.
10. Application-service integration.
11. REST and SSE conversation APIs with strict validation.
12. Typed web state, idempotent commands, and SSE reconnection.
13. Accessible integrated conversation UI.
14. Component-registry and site-page integration.
15. Deterministic fake-server browser acceptance coverage and operator documentation.
16. Real local Codex compatibility, containment, and security verification.

## Codex Compatibility Audit

The implementation was audited against locally generated experimental app-server schemas from `codex-cli 0.144.4`:

```sh
codex app-server generate-json-schema --experimental --out "$schema_dir"
```

The command generated 337 schema files. Every corresponding checked-in generated schema was byte-identical. The only additional checked-in protocol files are repository metadata (`README.md` and `VERSION`). The supervisor intentionally accepts the compatible `0.144.x` Codex CLI series.

A disposable temporary Git repository was also exercised with the real locally authenticated Codex CLI in `workspace-write` mode. Codex created a Vue content component, the ordinary registry build discovered it, and the repository's original branch, commit history, tracked baseline, and pre-existing untracked file remained unchanged. No dependency or Git operation was requested or performed.

## Verification

The completed implementation passed:

```sh
go test -race ./... -count=1
go vet ./...
cd web
bun run test:run
bun run typecheck
bun run build
bun run e2e:agent
```

The final focused security regression rerun also passed:

```sh
go test -race ./internal/agent/... ./internal/app ./internal/web -count=1
cd web
bun run test:run
bun run e2e:agent
```

The frontend suite contains 62 passing tests. The browser acceptance scenario covers clarification, approval for the current generation, protected dependency consent, automatic registry rebuild and highlighting, manual preview behavior, non-inherited follow-up approval, SSE recovery, and retained conversation state after reload.

## Deliberately Deferred

- User-selectable Codex models.
- Browser-side privileged process controls.
- Automatic preview startup.
- Automated staging, commits, pushes, or pull requests.
- Dependency installation or network access without explicit consent.
- Making a real authenticated Codex invocation mandatory in CI; deterministic protocol and browser fixtures remain the required CI path.
