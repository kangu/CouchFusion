# CLI Bootstrap Tool PRD

## 1. Purpose & Background
> Progress: [Done] Clarified CLI purpose, context, and expected outcome for ecosystem bootstrapping.
- **Objective:** Deliver a Go-based CLI that bootstraps the NuxtJS apps + shared layers ecosystem with consistent structure, validated external dependencies, and streamlined cloning of starter templates.
- **Context:** Existing projects rely on `/apps` for Nuxt applications and `/layers` for reusable functionality (analytics, auth, content, database, imagekit, lightning, orders). Creating a new environment or project currently involves multiple manual steps.
- **Primary Outcome:** New contributors can run a single binary to stand up the baseline repo layout, clone referenced starter repositories, and ensure local prerequisites are acknowledged.

## 2. Target Users & Personas
> Progress: [Done] Documented core user groups that benefit from the CLI.
- **Internal framework maintainers:** Need to spin up fresh playgrounds or maintain shared starters.
- **Feature developers:** Require consistent scaffolds when building new apps or custom layers.
- **Automation pipelines:** CI/CD or templates that programmatically hydrate repos for demos.

## 3. Goals & Non-Goals
> Progress: [Done] Captured scope alignment and explicit exclusions per stakeholder direction.
### Goals
- Detect whether the current working directory is already initialized (contains `/apps` and `/layers`).
- Provide an `init` command that prepares the workspace directory and clones the baseline layers repo from configured git sources.
- Provide a `new` command that scaffolds an app folder and clones the configured starter repo, optionally linking selectable modules mapped to layers with clear follow-up instructions.
- Provide a `create_layer` command that scaffolds new layer directories by cloning pre-defined starter templates.
- Manage configuration via a local config file that stores git source information and optional credentials.
- Validate presence of `bun` executable and CouchDB service (`localhost:5984`) at startup with actionable warnings.
- Ensure commands remain idempotent and report when directories already exist or repos are previously cloned.

### Non-Goals
- Managing dependency installation beyond prerequisite checks (no automatic install of bun or CouchDB).
- Modifying cloned repositories beyond initial clone (no automatic package updates or template customization).
- Handling starter repository version pinning beyond branches specified in config (no tag management workflow).
- Auto-editing Nuxt config or app code to wire modules; CLI only surfaces instructions for manual changes.
- Handling cloud provisioned infrastructure beyond local CouchDB reachability check.
- Providing GUI or interactive TUI beyond standard CLI prompts.

## 4. Success Metrics
> Progress: [Done] Defined measurable outcomes for rollout validation.
- Time to first environment setup reduced to under 5 minutes.
- Error rate for missing prerequisites drops due to visible warnings.
- Developers report consistent directory layout across new projects (qualitative feedback).
- Automated smoke script can execute `init` + `new` on a clean folder without manual intervention.

## 5. Assumptions & Constraints
> Progress: [Done] Listed operating expectations and environmental constraints.
- CLI is compiled and distributed as a Go binary targeting macOS/Linux.
- Git is available and authenticated per user environment (SSH keys or HTTPS credentials for private repos).
- Config file lives at `~/.couchfusion/config.yaml` (exact path configurable via flag) and is populated before first run.
- Network access allowed for git clone operations.
- The CLI runs within a workspace root where `/apps` and `/layers` folders should exist post-init.
- HTTPS operations rely on existing system credential helpers when possible; otherwise CLI prompts for username/password or tokens inline without storing them.

## 6. Dependencies
> Progress: [Done] Enumerated external binaries, services, and repositories.
- **System binaries:** `git`, `bun`.
- **Services:** CouchDB at `http://localhost:5984` (warning-only check, local endpoint only).
- **Repositories:**
  - `layers_base_repo` – starter kit for shared layers.
  - `app_starter_repo` – base Nuxt app template.
  - `layer_starter_repo` – base layer template.
- **Configuration loader:** Support YAML (primary) with possible JSON fallback.

## 7. Configuration File Requirements
> Progress: [Done] Defined config schema and validation rules incorporating SSH/HTTPS guidance.
- Default path `~/.couchfusion/config.yaml` (overridable via `--config` flag).
- Fields:
  ```yaml
  repos:
    init:
      url: git@github.com:org/layers-base.git
      branch: main
      protocol: ssh # ssh or https
      authPrompt: false
    new:
      url: https://github.com/org/app-starter.git
      branch: main
      protocol: https
      authPrompt: true
    create_layer:
      url: git@github.com:org/layer-starter.git
      branch: main
      protocol: ssh
  modules:
    analytics:
      description: Analytics tracking utilities
    auth:
      description: Authentication + session helpers
    # ... mirrors actual `/layers` inventory
  workspace:
    defaultRoot: .
  prompts:
    defaultLayerSelection: []
  ```
- Validation: ensure required repo URLs and branch fields exist; verify `protocol` is `ssh` or `https`; enforce boolean for `authPrompt`.
- CLI should surface meaningful errors when config missing or malformed, with instructions to initialize config manually.

## 8. Command Overview
> Progress: [Done] Summarized commands, flag surface area, and safety checks.
| Command | Description | Key Flags | Idempotency Handling |
|---------|-------------|-----------|----------------------|
| `couchfusion init` | Prepare workspace with `/apps` and `/layers`, clone base layers repo. | `--config`, `--force`, `--layers-branch`. | Warn if directories exist; allow `--force` to re-clone into empty dirs. |
| `couchfusion new` | Create new app folder, clone starter repo, capture module selection. | `--name`, `--modules`, `--branch`. | Prevent overwrite if app folder exists; provide `--force` to clear empty dir. |
| `couchfusion create_layer` | Create new layer folder and clone layer starter template. | `--name`, `--branch`. | Same as app: guard against existing directories. |

## 9. Detailed User Flows
> Progress: [Done] Documented end-to-end execution steps with manual follow-up guidance.
### Startup Check (global pre-run)
1. Load config file.
2. Detect current workspace initialization status (presence of `apps/` and `layers/`).
3. Check `bun` availability via `bun --version`.
4. Ping CouchDB at `http://localhost:5984/_up` or `_utils`.
5. Display aggregated warnings before executing command logic; continue unless fatal configuration missing.

### `init`
1. Confirm target directory (current working directory or `--path`).
2. If already initialized, prompt user to confirm continuing; skip clone if repos exist.
3. Create directories: `/apps`, `/layers`.
4. Clone `layers_base_repo` into `/layers` (support branch selection from config or CLI flag).
5. Output summary of created resources and next steps (e.g., run `new`).

### `new`
1. Ensure `init` already ran (validate directories, otherwise prompt to run `init`).
2. Prompt for app name if not provided via flag; sanitize to filesystem-safe slug.
3. Present module selection list sourced from config `modules` (multi-select). Default selections drawn from config preferences.
4. Create `/apps/<app-name>` directory.
5. Clone `app_starter_repo` into new directory; optionally checkout provided branch/tag (branch only per config scope).
6. Persist selection metadata in `/apps/<app-name>/couchfusion.json` (records modules picked, timestamp, CLI version).
7. Generate manual integration guidance: output to console and write `/apps/<app-name>/docs/module_setup.json` capturing Nuxt `extends` entries for chosen modules. No automated edits to `nuxt.config.ts`.
8. Summarize next steps, highlighting manual module wiring instructions.

### `create_layer`
1. Validate workspace init.
2. Prompt for new layer name; ensure uniqueness against existing `/layers` directories.
3. Clone `layer_starter_repo` into `/layers/<layer-name>`.
4. Register new layer within config modules section (optional interactive step or manual instructions) and provide reminder to update shared documentation.

## 10. UX, Prompts & Output
> Progress: [Done] Captured messaging style and automation flags.
- Use clear ANSI-neutral text with progress indicators (e.g., `Creating apps directory... done`).
- Provide `--yes` / `-y` option to skip confirmations for automation.
- Provide structured warnings in yellow text equivalents (e.g., `[WARN] bun not detected`).
- Display final success summary with next commands, module follow-up paths, and location paths.
- Support verbose mode (`--verbose`) for detailed git commands, and quiet mode (`--quiet`) for automation logs.
- When HTTPS protocol requires credentials, prompt inline and optionally cache via system credential helper if available.

## 11. Error Handling & Edge Cases
> Progress: [Done] Listed failure scenarios and mitigation.
- Missing config file: exit with instructions to create config; offer `couchfusion config init` in future (out of scope for MVP).
- Git clone failure: bubble up stderr, include hint to verify credentials or repo URL.
- Existing directories: abort unless `--force` set and directory empty; never delete non-empty folders automatically.
- Network timeouts: provide retry suggestion or offline fallback messaging.
- Invalid module selections: re-prompt or exit with list of valid options.
- HTTPS authentication failures: mask passwords in logs and re-prompt with option to abort.

## 12. Telemetry & Logging
> Progress: [Done] Detailed logging expectations and privacy stance.
- CLI logs to stdout; optional `--log-file` flag writes to file.
- No remote telemetry in MVP (privacy compliance). Future enhancement could include anonymized usage stats.

## 13. Testing Strategy
> Progress: [Done] Outlined automated and manual verification pathways.
- Unit tests for config parsing, command flag handling, path validation.
- Integration tests (mocked git + filesystem) to cover `init`, `new`, `create_layer` flows.
- Smoke test script executed in CI to run `init` then `new --name test-app --modules analytics,auth` inside temp directory, verifying directories, instruction file, and marker files.
- Manual QA checklist: verify warnings appear when `bun`/CouchDB absent, ensure re-running commands is safe, ensure HTTPS prompts appear when configured.

## 14. Release & Distribution
> Progress: [Done] Set release packaging, versioning, and documentation expectations.
- Provide Makefile target `make build` to produce platform binaries.
- Publish artifacts to internal package registry or GitHub Releases.
- Document installation steps in project README and share config template.
- Ensure version flag (`couchfusion --version`) reflects semantic versioning.

## 15. Security & Compliance
> Progress: [Done] Addressed credential handling and repository hygiene.
- Avoid storing credentials in plain text; instruct users to configure git authentication outside CLI where possible.
- Mask sensitive config fields in logs.
- Respect `.gitignore` entries for generated files under `/apps` and `/layers`.
- Limit HTTPS credential prompts to runtime memory; never persist secrets to disk.

## 16. Future Enhancements (Backlog)
> Progress: [Done] Captured follow-on opportunities beyond MVP.
- Command `config init` to bootstrap config file from interactive prompts.
- Layer linking automation that adds required dependencies to app package files.
- Template registry management (list available starters, fetch remote metadata).
- Telemetry opt-in for usage analytics.
- Plugins architecture for custom scaffolds.

## 17. Stakeholder Decisions (Resolved Questions)
> Progress: [Done] Recorded stakeholder responses for historical context.
1. **Starter repo version pinning:** CLI relies on branches declared in config; no additional tag management or version pinning workflow.
2. **HTTPS git support:** CLI must support HTTPS clones by prompting for username/password or tokens when `protocol: https` is configured, in addition to SSH.
3. **Module selection wiring:** CLI generates a instructions file detailing required `extends` entries for `nuxt.config.ts` and leaves code edits to the developer.
4. **CouchDB endpoint override:** Health check remains fixed to `http://localhost:5984`; no remote configuration required.

---

This PRD supersedes the initial draft and should be updated as new requirements emerge.
