# Implementation Documentation - CLI Current-State Docs Alignment

Request: Update the README and documentation so the most recent changes and direction are reflected correctly.

## Summary

Aligned the main CouchFusion CLI documentation with the current implementation and recent direction. The docs now describe the Bubble Tea TUI flows, embedded default config fallback, automatic app wiring during `couchfusion new`, auth-layer parameter handling, fresh git initialization for cloned starters, and the current backlog direction.

## Updated Areas

- `README.md` now documents the real `new` workflow: positional app names, TUI behavior, `package.json` dependency injection, `nuxt.config.ts` rewrites, auth `.env` setup, generated metadata, and final `bun install` / `bun run dev` steps.
- `docs/specs/cli_bootstrap_prd.md` now distinguishes the original MVP scope from the current implementation direction and records updated `new` / `create_layer` flows.
- `docs/specs/end_of_create_app.md` now captures the setup work that happens before the final user instructions.
- `docs/specs/layer_parameters.md` now records that layer parameter logic should be reused by the future `add_layer` workflow and aligned with flat CouchDB `_config` section conventions where applicable.
- `internal/workspace/workspace.go` now emits current `docs/module_setup.json` next-step text instead of stale manual Nuxt wiring instructions.

## Verification

- Ran `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test ./...` after the documentation and user-facing text update.
