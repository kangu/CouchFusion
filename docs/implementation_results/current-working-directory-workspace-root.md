# Current Working Directory Workspace Root

## Initial Prompt
When doing root, err := resolvePath(cfg.WorkspaceRoot()) in workspace.go, the returned path seems to be the one from when the project was built. I want the path to always be the current path from which the user runs the cli, where it would look for "apps" and "layers"

## Implementation Summary
Implementation Summary: Updated the create_app and create_layer workflows to derive their workspace root from the caller's current working directory, ensuring apps and layers are discovered relative to where the CLI is executed.

## Documentation Overview
- Adjusted workspace resolution so operational commands guard against stale paths baked into configuration or build-time contexts, preferring the runtime working directory for resolving `apps` and `layers`.
- Retained downstream cloning, metadata, and documentation generation logic to operate against the newly derived root without altering existing outputs.

## Implementation Examples
- `cli-init/internal/workspace/workspace.go:101` (couchfusion module) resolves the workspace base using `os.Getwd()` before verifying the `apps` directory exists.
- `cli-init/internal/workspace/workspace.go:139` mirrors the current-directory resolution for layer scaffolding to ensure parity between app and layer workflows.
