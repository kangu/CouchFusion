# Implementation Documentation – Rename create_app to new

## Initial Prompt
Refactor the create_app command to be called "new". Search everywhere where it's mentioned and used and do the change. The final command for initializing a new app would be "couchfusion new"

## Implementation Summary
Implementation Summary: Renamed the app creation command to `new`, updating CLI parsing, defaults, docs, and TUI to scaffold apps via `couchfusion new` while keeping legacy configs compatible.

## Documentation Overview
- CLI entrypoint now routes the `new` subcommand (with the same flags) through the app scaffolding workflow; usage text and log messaging reflect the new command name.
- Default and parsed configuration expect a `repos.new` entry; legacy `create_app` keys are auto-normalized to `new` for backward compatibility.
- TUI scaffolding functions and docs now reference `couchfusion new`, with the starter repository link maintained under the renamed repo key.
- Supporting documentation across specs and implementation notes was updated to use the `new` command and adjusted file references (e.g., `new_tui.go`).

## Implementation Examples
- `main.go:21-77` switches the command dispatcher and usage strings to `new` and routes to `runNew`.
- `internal/config/config.go:35-95` validates `repos.new` and normalizes legacy `create_app` entries into the new key.
- `internal/workspace/workspace.go:110-159` and `internal/workspace/new_tui.go:1-552` align the app creation workflow and TUI entrypoints with `RunNew`/`RunNewTUI` and repo key `new`.
- `internal/config/default_config.yaml` sets the default starter repo under the `new` key with HTTPS transport.
