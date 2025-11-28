# Implementation Documentation – Module Selection TUI

## Initial Prompt
Evaluate using Charmbracelet’s Bubble Tea (and add Lip Gloss for styling) to enhance the TUI experience. I want to be able to move with ↑/↓ and press Space to toggle multiple selections on the chosen layers.

## Implementation Summary
Implementation Summary: Built a Bubble Tea-powered module selector component that now anchors the new flow, enabling arrow navigation and multi-select toggles with Lip Gloss styling.

## Documentation Overview
- Introduced a Bubble Tea + Lip Gloss component that renders module rows, highlights the active cursor, and tracks selections with keyboard shortcuts.
- Embedded the selector inside the new new TUI pipeline while retaining non-interactive fallbacks for scripted environments.
- Added `go-isatty` checks so the CLI can gracefully downgrade when stdout/stderr are not attached to a TTY.

## Implementation Examples
- `cli-init/internal/workspace/tui.go:12` defines the module selector component used across the Bubble Tea experiences.
- `cli-init/internal/workspace/new_tui.go:64` integrates the selector into the multi-step new wizard, pairing it with name, auth, and summary panels.
- `cli-init/go.mod` records the Bubble Tea, Lip Gloss, Bubbles, and go-isatty dependencies needed for the styled terminal interface.
