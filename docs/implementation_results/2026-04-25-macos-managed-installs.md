# Implementation Documentation - macOS Managed Install Improvements

Request: Implement the same managed-install functionality for Bun and CouchDB on macOS systems.

## Summary

Confirmed Bun already uses the official installer on macOS and now has regression coverage for the macOS install path. Extended CouchDB's macOS Homebrew install scripts so Homebrew shellenv setup is persisted into future zsh/bash sessions when Homebrew is bootstrapped or used by the CLI.

## Changes

- Added Bun macOS installer coverage to ensure Darwin uses the official Bun installer, keeps `BUN_INSTALL`, verifies through `bun --version`, and does not run Linux apt prerequisites.
- Added a shared CouchDB macOS Homebrew prelude used by both install and configure scripts.
- CouchDB macOS setup now:
  - evaluates `/opt/homebrew/bin/brew shellenv` or `/usr/local/bin/brew shellenv` during the install process,
  - bootstraps Homebrew when `brew` is missing,
  - persists Homebrew shellenv into `.zprofile`, `.zshrc`, `.bash_profile`, `.bashrc`, and `.profile`,
  - avoids reinstalling CouchDB when `brew list couchdb` already succeeds.
- Updated README and PRD docs to reflect macOS Homebrew persistence and Bun same-session/profile behavior.

## Verification

- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./internal/bun ./internal/couchdb`
- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test -count=1 ./...`
- `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go build ./...`
