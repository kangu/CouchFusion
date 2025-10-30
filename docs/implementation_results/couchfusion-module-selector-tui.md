# Implementation Documentation – Module Selection TUI

## Initial Prompt
Evaluate using Charmbracelet’s Bubble Tea (and add Lip Gloss for styling) to enhance the TUI experience. I want to be able to move with ↑/↓ and press Space to toggle multiple selections on the chosen layers.

## Implementation Summary
Implementation Summary: Replaced the plain-text module prompt with a Bubble Tea-powered multi-select so developers can navigate layers with arrow keys and toggle selections via space/enter.

## Documentation Overview
- Introduced a Bubble Tea + Lip Gloss model that renders the available modules, tracks cursor state, and supports multi-selection with keyboard shortcuts.
- Wired the TUI into `ResolveAppCreationInputs`, falling back to the default layer selection when no choices are made and to the legacy prompt when the terminal is non-interactive.
- Added `go-isatty` checks to detect interactive terminals, keeping behaviour predictable in scripted environments.

## Implementation Examples
- `cli-init/internal/workspace/tui.go:12` defines the Bubble Tea model, rendering styles, and selection behaviour using lipgloss for highlights.
- `cli-init/internal/workspace/workspace.go:69` now invokes `runModuleSelector` when interactive selection is available, reverting to defaults if the user exits without choosing modules.
- `cli-init/go.mod` records new dependencies (`bubbletea`, `lipgloss`, and `go-isatty`) required by the TUI.
