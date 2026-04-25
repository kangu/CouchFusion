# Implementation Documentation - Managed CouchDB Installation

Request: Integrate CouchDB installation into the CouchFusion CLI when CouchDB is not running, show detected version when it is running, and handle installation in the background following the existing README/script direction.

## Summary

Implemented a managed CouchDB preflight that runs before workspace commands. The CLI now probes local CouchDB, records first-time acknowledgement for running instances, installs CouchDB on macOS or Debian/Ubuntu when missing and approved, stores reusable admin auth in an env-style file, and lets auth-layer setup reuse that auth before prompting.

## Updated Areas

- Added `internal/couchdb` for CouchDB probing, env credential storage, preflight decisions, generated admin auth, system installer command orchestration, post-install verification, system database setup, and Photon installation.
- Added `--yes` / `-y` to `init`, `new`, and `create_layer` so automation can approve CouchDB acknowledgement or installation.
- Removed the old warning-only CouchDB check from `internal/checks`; CouchDB is now handled by the managed preflight.
- Updated auth-layer parameter setup to reuse `COUCHDB_ADMIN_AUTH` from the process environment or `~/.couchfusion/couchdb.env`.
- Updated README and specs to document managed install behavior, supported platforms, local-only bind address, and the credential handoff file.
- Updated the install success flow to print the generated admin password and wait for Enter before continuing.
- Added the Photon UI URL to the install success output: `http://localhost:5984/photon/_design/photon/index.html`.
- Added managed Bun setup before CouchDB preflight: missing Bun can be installed on macOS/Linux, the current process environment is refreshed, and the shell profile is updated so `bun -v` is available after setup.

## Verification

- Run `GOCACHE=/Users/radu/Projects/nuxt-apps/cli-init/.gocache go test ./...`.
