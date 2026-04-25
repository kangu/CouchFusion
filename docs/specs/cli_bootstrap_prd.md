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
- Manage Bun availability and local CouchDB service (`127.0.0.1:5984`) at startup with actionable prompts.
- Ensure commands remain idempotent and report when directories already exist or repos are previously cloned.

### Non-Goals
- Managing dependency installation beyond prerequisite checks for tools other than Bun and CouchDB.
- Handling starter repository version pinning beyond branches specified in config (no tag management workflow).
- Handling cloud provisioned infrastructure beyond local CouchDB reachability check.

### Current Direction Updates
- The implementation now intentionally mutates cloned app files during `couchfusion new` when those edits are part of selected layer setup.
- Selected layers are wired automatically into `package.json` and `nuxt.config.ts`; `docs/module_setup.json` remains a handoff record rather than the only wiring mechanism.
- The interactive experience now uses Bubble Tea + Lip Gloss TUIs when stdin/stdout are terminals, with `COUCHFUSION_NO_TUI=1` as the plain-prompt fallback.
- The CLI falls back to an embedded default config when no user config exists.
- Cloned app and layer starter repositories are reinitialized as fresh git repositories after clone. The base layers clone used by `init` is expected to preserve its repository state unless future requirements say otherwise.
- Bun setup is now part of global preflight: missing Bun can be installed on macOS/Linux, with current-process PATH refresh and shell profile updates.
- Local CouchDB setup is now part of global preflight: running instances are version-detected and acknowledged once, while missing instances can be installed on macOS and Debian/Ubuntu.

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
- **System binaries:** `git`; Bun is managed when missing on macOS/Linux.
- **Services:** CouchDB at `http://127.0.0.1:5984` (managed local preflight).
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
3. Check Bun availability via `bun --version`; if missing, prompt to install it or require `--yes` in non-interactive mode.
4. Refresh the current process environment with `BUN_INSTALL=$HOME/.bun` and Bun paths, update the user shell profile if needed, and create a `~/.couchfusion/bin/bun` shim when possible.
5. Probe CouchDB at `http://127.0.0.1:5984/_up` and fetch root version metadata.
6. If CouchDB is running, show the detected version and record first-time acknowledgement in `~/.couchfusion/couchdb.env`.
7. If CouchDB is missing, prompt to install it or require `--yes` in non-interactive mode.
8. Display aggregated warnings before executing command logic; continue unless fatal configuration, Bun setup, or CouchDB setup fails.

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
6. Reinitialize the cloned starter as a fresh git repository.
7. Add selected layer dependencies to `package.json` using local `file:../../layers/<module>` links.
8. Run selected layer parameter handlers. Current implementation supports `auth`, which writes CouchDB auth values to `.env` and ensures the CouchDB user document exists.
9. Rewrite `nuxt.config.ts` so its top-level `extends` array contains only selected `../../layers/<module>` entries.
10. Persist selection metadata in `/apps/<app-name>/couchfusion.json` (records modules picked, timestamp, CLI version).
11. Generate handoff guidance in `/apps/<app-name>/docs/module_setup.json` capturing selected modules, resolved layer entries, and next steps.
12. Summarize next steps: `cd apps/<app-name>`, `bun install`, and `bun run dev`.

### `create_layer`
1. Validate workspace init.
2. Prompt for new layer name; ensure uniqueness against existing `/layers` directories.
3. Clone `layer_starter_repo` into `/layers/<layer-name>`.
4. Reinitialize the cloned layer as a fresh git repository.
5. Register new layer within config modules section (future automation) or provide manual instructions and a reminder to update shared documentation.

## 10. UX, Prompts & Output
> Progress: [Done] Captured messaging style and automation flags.
- Use clear logs for non-TUI mode and Bubble Tea + Lip Gloss flows for interactive TTY sessions.
- Keep `COUCHFUSION_NO_TUI=1` as the explicit fallback for plain prompts.
- Provide structured warnings for non-managed prerequisites.
- Display final success summary with next commands, module follow-up paths, and location paths.
- Support `--yes` / `-y` for managed Bun and CouchDB preflight automation. Treat `--verbose` and `--quiet` as planned enhancements until implemented.
- When HTTPS protocol requires credentials, prompt inline and optionally cache via system credential helper if available.

## 11. Error Handling & Edge Cases
> Progress: [Done] Listed failure scenarios and mitigation.
- Missing config file: exit with instructions to create config; offer `couchfusion config init` in future (out of scope for MVP).
- Git clone failure: bubble up stderr, include hint to verify credentials or repo URL.
- Existing directories: abort unless `--force` set and directory empty; never delete non-empty folders automatically.
- Network timeouts: provide retry suggestion and identify the failing CouchDB setup phase where possible.
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
- Manual QA checklist: verify Bun present/missing preflight branches, verify CouchDB running/missing preflight branches, ensure re-running commands is safe, ensure HTTPS prompts appear when configured.

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
- `add_layer` workflow for applying the same module wiring and parameter handling to existing apps.
- Test coverage for config parsing, filesystem mutation helpers, package dependency merging, Nuxt config rewrites, and TUI-independent workflows.
- Safer structured parsing for `nuxt.config.ts` if regex rewriting becomes too fragile for real-world configs.
- Template registry management (list available starters, fetch remote metadata).
- Logging automation flags such as `--verbose` and `--quiet`.
- Telemetry opt-in for usage analytics.
- Plugins architecture for custom scaffolds.

## 17. Stakeholder Decisions (Resolved Questions)
> Progress: [Done] Recorded stakeholder responses for historical context.
1. **Starter repo version pinning:** CLI relies on branches declared in config; no additional tag management or version pinning workflow.
2. **HTTPS git support:** CLI must support HTTPS clones by prompting for username/password or tokens when `protocol: https` is configured, in addition to SSH.
3. **Module selection wiring:** CLI generates a instructions file detailing required `extends` entries for `nuxt.config.ts` and leaves code edits to the developer.
4. **CouchDB endpoint override:** Managed local preflight remains fixed to `http://127.0.0.1:5984`; no remote configuration required.

---

This PRD supersedes the initial draft and should be updated as new requirements emerge.
