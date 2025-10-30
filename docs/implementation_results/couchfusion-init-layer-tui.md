# Implementation Documentation – Init & Layer TUIs

## Initial Prompt
Include the nice Bubble Tea interface all around the cli app, on all the commands and throughout all steps

## Implementation Summary
Implementation Summary: Wrapped the `init` and `create_layer` commands in Bubble Tea flows with Lip Gloss styling, covering input collection, confirmation, and progress feedback.

## Documentation Overview
- Added `RunInitTUI` and `RunCreateLayerTUI` to orchestrate multi-step wizards (path/branch/force for init, name/branch/force for layer) with context-aware hints.
- Updated `gitutil.Clone` and workspace functions to accept optional clone options so progress logs can stay inside the TUI.
- Extended the Bubble Tea infrastructure (`ui` package) with log buffers, root layout, and shared styling used across all commands.

## Implementation Examples
- `cli-init/internal/workspace/init_tui.go:1` renders the init wizard, including branch overrides and force toggles with live logs.
- `cli-init/internal/workspace/create_layer_tui.go:1` manages layer scaffolding with name sanitisation, branch overrides, and progress spinner.
- `cli-init/main.go:60`/`:141` delegate to the TUIs when a TTY is available, falling back to the traditional prompts otherwise.
