# Canonical Content Registry Generator Design

**Date:** 2026-07-14  
**Status:** Approved

## Problem

The repository currently contains two independent component-registry generators. The legacy `cli-content/generate-component-registry.mjs` writes the runtime TypeScript definitions and CouchFusion's integrity-checked JSON manifest. The Content layer's `content/scripts/generate-component-registry.mjs`, used by the Nuxt development watcher, writes only the TypeScript file. A normal external `bun run dev` therefore replaces `component-definitions.ts` without replacing its paired manifest, and CouchFusion reports the registry as invalid until **Rebuild Registry** runs the legacy generator.

The two copies have also accumulated behavioral drift. In particular, the current local `cli-content` generator contains uncommitted `previewSections` metadata handling that must be preserved during consolidation.

## Considered Approaches

1. **Canonical Content-layer generator with compatibility delegates — selected.** Move the paired-output module, tests, manifest record construction, and all current generator behavior into `layers/content`. Replace legacy `cli-content` generator and watcher entry points with thin delegates to the layer scripts. Existing app package scripts continue to work while every path executes one implementation.
2. **Keep two generators in sync.** Copy the manifest writer into the layer and add parallel tests. This fixes today's symptom but preserves the duplication that caused it.
3. **Make the Content watcher call `cli-content`.** This avoids copying code but makes the reusable Content layer depend on a sibling repository that may not exist in installed or independently consumed workspaces.

## Ownership and Compatibility

`layers/content/scripts/generate-component-registry.mjs` becomes the only implementation. It owns component discovery, definition extraction, `previewSections`, deterministic ordering, TypeScript rendering, JSON manifest rendering, hashing, and paired atomic installation.

`layers/content/scripts/registry-output.mjs` owns the output contract and paired write. Its tests move with it into the Content layer.

The following existing entry points remain supported:

- the Content layer's dev-only registry watcher;
- `content-registry` from the Content layer package;
- direct execution of `layers/content/scripts/generate-component-registry.mjs`;
- existing app scripts that execute `../../cli-content/generate-component-registry.mjs`;
- the legacy `cli-content` watch command.

The two `cli-content` scripts become minimal process delegates. They resolve the sibling Content-layer script, forward all arguments and signals, inherit stdio, and exit with the delegated process's exit code. They contain no registry parsing or output logic.

## Generation Contract

Every successful generation writes this pair in the same content-builder directory:

```text
component-definitions.ts
component-definitions.json
```

Both files are rendered from the same in-memory records. Definitions and manifest components are sorted by component ID. The JSON manifest remains version `1`, includes the SHA-256 digest of the exact TypeScript bytes, and contains only safe UI metadata: ID, label, description, relative Vue source, prop count, and child-slot support.

The writer creates and syncs sibling temporary files, installs TypeScript first, and installs JSON last as the commit marker. On failure it removes remaining temporary files. CouchFusion's existing inspector continues rejecting incomplete or hash-mismatched pairs.

An empty registry still produces a valid TypeScript file and a ready JSON manifest containing an empty component array.

## Runtime Flow

On `bun run dev`, the Content layer starts its watcher. The initial watcher run and every subsequent Vue change call the canonical layer generator. When generation finishes, both files have matching content and the CouchFusion UI can read the updated list without invoking **Rebuild Registry**.

The UI rebuild remains useful as an explicit manual trigger, but it is no longer a special metadata-producing path. If an app still uses the legacy `cli-content` package script, that script delegates to the same canonical generator.

## Error Handling

- A missing sibling Content layer causes the legacy delegate to fail with a concise path-specific error and a non-zero exit code.
- Generator parsing or dependency-resolution errors remain visible through inherited stderr.
- A failed paired write never installs JSON before TypeScript and cleans temporary files.
- Watchers serialize runs as they do today; a change arriving during generation queues one additional run.

## Testing

1. Move/adapt the existing output-contract tests into `layers/content/tests/scripts` and verify deterministic metadata, digest correctness, ordered installation, cleanup, and empty registries.
2. Add an integration test that executes the canonical layer generator against a temporary Nuxt 4 fixture and asserts that both files are generated and the digest matches.
3. Add compatibility tests for the legacy delegates, verifying argument and exit-code forwarding to a controlled target.
4. Run the focused Content tests and the full `cli-content` test command.
5. Run the canonical generator against a real local app, then inspect it through CouchFusion's existing Go `ComponentInspector` tests or an equivalent manifest validation.

## Documentation and Migration

Update the Content-layer implementation results to name the layer command as canonical and describe the legacy delegate. Existing app `registry:build` scripts do not require immediate edits; they remain compatible. New documentation and examples should prefer the Content-layer command so `cli-content` can eventually be removed in a separately approved cleanup.
