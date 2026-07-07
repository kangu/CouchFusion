# Implementation Documentation - Managed Git Installation

Request: During `couchfusion init`, Git was missing from `PATH` and the clone failed with `exec: "git": executable file not found in $PATH`. Add Git installation to the init setup so the CLI handles this dependency before cloning.

## Summary

Implemented a managed Git preflight that runs before Bun, CouchDB, and workspace clone/reinitialize logic. The CLI now detects Git, offers installation on macOS or Debian/Ubuntu when missing, and fails early with a clear `git preflight failed` message instead of reaching `git clone` with no executable available.

## Changes

- Added `internal/gitsetup` for Git probing, installation orchestration, preflight decisions, and testable command execution.
- Wired `ensureGit` into `init`, `new`, and `create_layer` before any command can invoke Git.
- Reused `--yes` / `-y` for non-interactive Git installation approval.
- Added Debian/Ubuntu installation through `sudo apt update -y` and `sudo apt install -y git`.
- Added macOS installation through Homebrew, including Homebrew bootstrap when `brew` is missing.
- Updated README and PRD docs to document managed Git setup and the remaining user responsibility for private repository authentication.

## Verification

- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./internal/gitsetup`
- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./...`
