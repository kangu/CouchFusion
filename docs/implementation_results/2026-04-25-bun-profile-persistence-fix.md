# Implementation Documentation - Bun Profile Persistence Fix

Request: Revert the broader Bun detection change because the PATH check was correctly reporting that Bun was unavailable in the environment. Fix the real issue: Bun installation works in the installer process, but shell availability differs between the installing session and new sessions.

## Summary

Restored the stricter Bun PATH probe and fixed the installation persistence path. The CLI now writes Bun startup configuration to both interactive and login shell startup files where applicable, and it creates a same-session shim in a directory that is already on the current `PATH` when possible.

## Changes

- Reverted the fallback Bun detection paths and returned `CommandProber` to `exec.LookPath("bun")`.
- Removed the environment refresh behavior for already-detected Bun installs outside the original PATH.
- Added `ShellProfilePaths` and `EnsureProfileBlocks` so install writes all relevant startup files.
- For bash, the installer now writes `.bashrc` and `.profile`, plus existing `.bash_profile` or `.bash_login` files when present.
- For zsh, the installer now writes `.zshrc` and `.zprofile`.
- Kept fish behavior scoped to `.config/fish/config.fish`.
- Added a same-session shim path: after installation, the CLI attempts to create `bun` in the first writable directory already present in the original terminal `PATH`.
- The same-session shim does not overwrite an unrelated existing `bun` in general PATH directories.
- Added tests covering bash login/interactive profile coverage, existing bash login files, zsh coverage, and writing multiple profile files.
- Added tests covering current-PATH shim creation, missing target handling, and existing-binary safety.
- Updated README behavior notes.

## Verification

- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./internal/bun`
- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./...`
- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go build ./...`
