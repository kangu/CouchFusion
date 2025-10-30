# Implementation Documentation – Update Nuxt Extends

## Initial Prompt
Implement the specs in cli-init/docs/specs/update_nuxt_config_with_modules.md. Proceed step by step with each section and mark that in the spec document as it's done to be used as reference when resuming work at a future time. 
Ask me for anything that after evalulation, you are not so sure what decision to make. Strive for minimal impact on other areas of the applicaiton. Any time your confidence for taking an actions is < 80%, ask for clarification. Present implementation plan before proceeding on my instructions.

## Implementation Summary
Implementation Summary: Automatically rewrites the `extends` array in each new app’s `nuxt.config.ts` so it only contains the selected layers from the couchfusion CLI selection.

## Documentation Overview
- Added a workspace helper that strips any pre-existing `extends` entries and replaces them with `../../layers/<module>` values derived from the selected modules.
- Hooked the helper into the app creation flow after layer-specific setup so Nuxt configuration reflects the finalized module choices.
- Updated the spec with completion checkboxes to capture the delivered behaviour for future reference.

## Implementation Examples
- `cli-init/internal/workspace/workspace.go:124` calls `updateNuxtExtends` once the repository is cloned and layer parameters are applied.
- `cli-init/internal/workspace/workspace.go:308` rebuilds the `extends` array dynamically, replacing old entries or inserting a new block when missing.
