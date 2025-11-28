# Implementation Documentation – Create App Layer Dependency Links

## Initial Prompt
For the new workflow, as part of the cli process which confirms the modules selected by the user, based on what is selected, it should update the package.json file (as soon as it's available) to include dependency for the selected layers, like "@my/content": "link:../../layers/content" for the content module, and so on, each after its own name. The successful end result of the new process is for the original package.json file to be brought in from the repository, then merged for dependencies for all the selected layers.

## Implementation Summary
Implementation Summary: Added layer dependency linking during new so selected modules are injected into package.json as @my/<module>: "link:../../layers/<module>" entries right after cloning the starter.

## Documentation Overview
- Introduced a helper that reads `package.json`, ensures a dependencies object, and injects `@my/<module>` entries pointing to `link:../../layers/<module>` for each selected module (deduped).
- Hooked the helper into `RunNew` immediately after cloning and reinitializing git so dependency links are merged as soon as the starter `package.json` is present.
- Left subsequent steps (layer parameter prompts, Nuxt extends rewrite, metadata/doc generation) unchanged to preserve existing workflow.

## Implementation Examples
- `cli-init/internal/workspace/workspace.go:110-159` now calls `updateLayerDependencies` during `new` before module-specific configuration.
- `cli-init/internal/workspace/workspace.go:402-454` defines the package.json merge logic mapping selected modules to `@my/<module>` dependencies with `link:../../layers/<module>` values.
