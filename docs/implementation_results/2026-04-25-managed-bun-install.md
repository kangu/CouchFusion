# Implementation Documentation - Managed Bun Installation

Request: Provide the same installation option for Bun when it is not available in PATH and reload the environment so the user can immediately run `bun -v`.

## Summary

Implemented a managed Bun preflight that runs before CouchDB setup for workspace commands. The CLI now detects Bun, offers installation on macOS/Linux when missing, refreshes the current process environment with Bun paths, and updates the user's shell profile for future terminal sessions.

## Updated Areas

- Added `internal/bun` for Bun probing, installation orchestration, PATH refresh, shell profile updates, and testable command execution.
- Wired `ensureBun` into `init`, `new`, and `create_layer` before CouchDB setup.
- Removed warning-only Bun handling from `internal/checks`.
- Reused `--yes` / `-y` for non-interactive Bun installation approval.
- Created a `~/.couchfusion/bin/bun` shim to `~/.bun/bin/bun` when possible so shells that already have CouchFusion on PATH can run `bun -v` immediately after setup.
- Added Linux prerequisite handling for `unzip`, `ca-certificates`, and `curl`, which are required by Bun's official shell installer on minimal Debian/Ubuntu systems.
- Improved verification diagnostics by including Bun installer output when the binary cannot be found after installation.
- Updated README and PRD docs to reflect managed Bun setup and immediate `bun -v` availability after environment refresh.

## Verification

- Run `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./...`.
