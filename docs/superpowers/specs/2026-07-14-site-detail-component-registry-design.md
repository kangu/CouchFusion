# Site Detail Component Registry Design

**Date:** 2026-07-14  
**Status:** Approved

## Goal

Make the CouchFusion site-detail page denser and more useful by compacting curated tasks into a narrow control rail and adding a dedicated inventory for the site's build-time content component definitions.

The inventory represents the definitions generated from the site's content components, not the runtime CouchDB-backed global component aliases. It supports rebuilding the registry and opening a registered component's Vue source file in the operating system's configured editor.

## Scope

This feature spans two repositories:

- `/Users/radu/Projects/nuxt-apps/cli-content` owns registry generation and adds a safe JSON companion manifest.
- `/Users/radu/Projects/nuxt-apps/cli-init` owns manifest inspection, API operations, editor launching, and the site-detail UI.

The existing TypeScript registry remains the runtime source consumed by the content layer. The JSON file is an inspection and tooling contract; it does not replace `component-definitions.ts`.

## Selected Layout

The approved layout is the registry-first split, option B from the visual design review.

On desktop, the page uses two columns:

- A narrow left rail contains compact Tasks above Project fingerprint.
- A wider main column contains Local content components.

The existing Current activity panel remains full-width below the split. On narrow screens, Local content components appears first, followed by Tasks and Project fingerprint.

Task rows become single-line controls containing the task name, restrained type/status text, and one Run or Start button. The task panel retains the current distinction between finite jobs and managed processes while removing the current large heading and generous vertical padding.

## Manifest Contract

`generate-component-registry.mjs` continues writing the existing TypeScript file and also writes `component-definitions.json` in the same content-builder directory.

Nuxt 4 example:

```text
app/content-builder/component-definitions.ts
app/content-builder/component-definitions.json
```

Nuxt 3 uses the generator's existing content-builder location without `app/`.

The JSON shape is:

```json
{
  "version": 1,
  "typescriptSha256": "sha256-of-component-definitions.ts",
  "components": [
    {
      "id": "parallax",
      "label": "Parallax",
      "description": "Auto-generated registry entry for Parallax.",
      "source": "app/components/content/Parallax.vue",
      "propCount": 4,
      "allowChildren": true
    }
  ]
}
```

Rules:

- `version` is exactly `1`.
- `typescriptSha256` is the lowercase SHA-256 digest of the exact generated TypeScript bytes.
- Components are ordered deterministically by ID.
- IDs are unique and non-empty.
- `source` is slash-normalized and relative to the site root.
- `source` is the exact Vue file used to generate the definition, including nested content-component directories.
- `propCount` matches the number of generated property definitions.
- `allowChildren` reflects default-slot support and is always emitted as a boolean.
- Description may be empty but is always emitted as a string.
- No executable JavaScript, prop default values, secrets, or absolute filesystem paths are included.

The generator constructs the TypeScript and JSON from the same in-memory component records so the outputs cannot drift within a successful generation run. It finishes all component processing first, writes both outputs to sibling temporary files, syncs and closes them, then replaces the TypeScript file before the JSON manifest. The manifest is the commit marker: its `typescriptSha256` must match the installed TypeScript file. A failure or interruption between replacements can therefore leave an old manifest beside new TypeScript, but the inspector detects the hash mismatch and never reports that pair as ready.

## Inspection Model

The Go project package adds a focused content-registry inspector. It resolves the selected site through the existing contained-site boundary and locates the Nuxt 4 or Nuxt 3 manifest path.

The project inspector produces:

```text
status: ready | stale | missing | invalid
components: []
problem: optional user-facing explanation
```

The application response adds `canRebuild`, derived from the curated task catalog rather than from manifest contents.

Each component contains only the manifest metadata needed by the UI. The backend may include the source path relative to the site root, but never exposes a newly accepted absolute path from the browser.

Status rules:

- `ready`: the manifest is valid and no registered source file is newer than it.
- `stale`: the manifest is valid and at least one registered source file has a modification time newer than the manifest.
- `missing`: neither supported manifest path exists.
- `invalid`: the file exists but has unsupported version, malformed JSON, duplicate IDs, invalid metadata, an unsafe source, or a TypeScript hash mismatch.

An empty valid manifest is `ready` with zero components. Inspection never invokes Bun, imports TypeScript, executes project code, or modifies files.

## Path Security

Every manifest source must satisfy all of the following:

- It is non-empty and relative.
- It has a `.vue` extension.
- Its lexical path remains beneath the site root.
- Its canonical path after symlink resolution remains beneath the site root.
- It is an existing regular file.

One unsafe entry makes the manifest `invalid`; the API does not return a partially trusted inventory. Open-source resolution starts from a registered component ID and repeats canonical containment immediately before launching the editor, protecting against filesystem changes between inspection and action.

## Application and HTTP API

The application service exposes three site-scoped operations:

1. Inspect local content components.
2. Start a component-registry rebuild.
3. Open a registered component source.

HTTP routes:

```text
GET  /api/v1/workspaces/{workspaceId}/sites/{siteId}/components
POST /api/v1/workspaces/{workspaceId}/sites/{siteId}/components/registry/rebuild
POST /api/v1/workspaces/{workspaceId}/sites/{siteId}/components/{componentId}/open
```

The list response returns the inspection status and safe component DTOs.

Rebuild is permitted only when the inspected site exposes the exact `registry:build` package script through the curated task catalog. It starts the same finite-job workflow used by other package tasks and returns a job snapshot. The browser supplies no executable, working directory, arguments, or alternative task ID.

Open source accepts only the component ID in the route. The application resolves the current manifest, finds that ID, validates the source again, and delegates to an editor-opener interface. Success returns no content. Missing IDs return not found; unsafe or invalid manifests return validation errors; unavailable OS launchers return process/unavailable errors without leaking command details.

## System Editor Integration

The production opener uses the operating system's default file association:

- macOS: `open <file>`
- Linux: `xdg-open <file>`
- Windows: `rundll32 url.dll,FileProtocolHandler <file>` through direct process arguments

The command receives the validated absolute source path as one argument. It does not use a shell. The opener is injected behind an interface so unit and browser tests never launch a real editor.

Opening a source file is an explicit user action. Ordinary inspection and rebuild completion do not open files automatically.

## User Interface

The site-detail page loads the site, curated tasks, and component inventory together. Component inspection failure is isolated to its panel and does not hide tasks or fingerprint data.

### Compact Tasks

- Small eyebrow and natural-sized section title.
- One compact row per task.
- Task name is primary; finite-job or managed-process type and task kind are secondary.
- One Run or Start control remains visible.
- Busy state disables only the active task.

### Local Content Components

The panel header contains:

- `Local content components`
- Component count
- Registry state lozenge
- `Rebuild registry` button when `canRebuild` is true

Each component row contains:

- Label
- Registry ID
- Property count
- `Accepts children` indicator when applicable
- Relative source path
- `Open source` action

The ready state renders the component ledger. The other states render clear guidance:

- `stale`: definitions may not match current Vue sources; rebuild is emphasized.
- `missing`: no manifest has been generated; rebuild creates it when the script is available.
- `invalid`: explain that the manifest could not be trusted and offer rebuild when available.

A valid empty registry says `No local content components registered.`

Rebuild uses the existing job activity subscription. While active, the button is disabled and reports rebuilding. On success, the component endpoint is fetched again. Failures remain visible through the existing Current activity logs and a concise panel error.

Open-source errors remain scoped to the component panel. A successful request needs no toast because opening the editor is the visible outcome.

## Error Handling

- A missing manifest is a normal inspectable state, not an API error.
- A stale manifest is usable for display, but open-source revalidates its path.
- Malformed or unsafe manifests return `invalid` state for reads and block open-source actions.
- Rebuild is unavailable when `registry:build` is absent from the curated catalog.
- A rebuild job may fail without replacing the last valid registry files.
- Missing source files block opening and prompt a rebuild.
- Editor launcher failures return a clear local-process error without exposing command internals.

## Testing

### `cli-content`

- JSON schema version and deterministic ordering.
- Metadata parity between TypeScript and JSON records.
- Correct relative source paths for Nuxt 3, Nuxt 4, and nested component directories.
- Correct prop counts and child support.
- Empty registry generation.
- Temporary-file cleanup, replacement ordering, and hash mismatch detection for interrupted paired output updates.

### `cli-init` Go

- Ready, stale, missing, invalid, and valid-empty inspection.
- Unsupported versions, duplicate IDs, malformed entries, missing sources, lexical escapes, and symlink escapes.
- Nuxt 3 and Nuxt 4 manifest discovery.
- Component-ID lookup and repeated containment before opening.
- Mocked editor commands for macOS, Linux, and Windows behavior.
- Application authorization and error mapping.
- HTTP response DTOs and list, rebuild, and open routes.
- Rebuild uses only the exact curated `registry:build` task.
- Testserver fixtures provide component inventory and mock editor success without external side effects.

### `cli-init` Web

- Compact finite and managed task rows preserve Run and Start events.
- Registry-first split and responsive ordering.
- Ready ledger and all non-ready states.
- Count, status, prop count, child indicator, and relative source display.
- Rebuild job start, disabled state, activity tracking, success refresh, and failure display.
- Open-source request and scoped error behavior.
- Browser acceptance verifies the component inventory and both actions without launching a real editor.

## Out of Scope

- Runtime or CouchDB-backed global component aliases.
- Editing or previewing Vue source inside CouchFusion.
- Creating, renaming, moving, or deleting components.
- Arbitrary filesystem browsing or arbitrary task execution.
- Automatically rebuilding during ordinary site inspection.
- Weakening the existing site-root or symlink containment rules.
