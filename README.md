# CouchFusion – Nuxt Apps + CouchDB

> The CouchApp vision evolved

CouchFusion is the opinionated developer platform that powers our Nuxt applications and shared CouchDB-backed layers. It combines a consistent monorepo structure (`/apps`, `/layers`, `/docs`), reusable feature packs, and automation that keeps every project aligned.

## Platform Overview
- **Stack** – [Nuxt 3/4](https://nuxt.com/), TypeScript, [Bun](https://bun.sh/) + Node for local dev parity, and [CouchDB](https://couchdb.apache.org/) for persistence.
- **Workspace** – A single repository where Nuxt apps extend layers such as analytics, auth, content, and more. Each layer ships with docs and upgrade guidance.
- **Automation** – The Go-based couchfusion CLI bootstraps workspaces, scaffolds new apps or layers, writes configuration handoff files, and seeds CouchDB credentials.

## Why It Matters
- **Velocity** – Spin up new apps or layers in minutes with consistent routing, env files, and module wiring instructions.
- **Reliability** – Shared layers encapsulate battle-tested behaviours (auth, database access, analytics) so teams ship features instead of plumbing.

## Vision
Deliver a batteries-included platform where any contributor can clone the repo, run one command, and start building features with confidence. The CLI and documentation evolve together so CouchFusion remains the source of truth for Nuxt + CouchDB development.

---

## CouchFusion CLI

`couchfusion` is the Go CLI that provisions the workspace, scaffolds Nuxt apps and layers, and performs post-clone setup (environment docs, CouchDB admin seeding, etc.).

### Installation & Setup

### Prerequisites
- Go 1.21+
- CouchDB 3.x - use this [script](https://raw.githubusercontent.com/kangu/CouchFusion/main/scripts/tooling/install_couchdb.sh) to install on Debian-based systems and use the [official installer from](https://couchdb.apache.org/#download) the CouchDB website for MacOS and Windows
- `git` available in your PATH with access to the starter repositories
- `bun` and `node` - use this [script](https://raw.githubusercontent.com/kangu/CouchFusion/main/scripts/tooling/install_node.sh) for installing both
> With only Bun on the box you can install dependencies and run Nuxt’s CLI,
  but Bun’s Node-compat layer isn’t yet complete enough to host Nitro/H3 in dev mode. You’ll
  hit 400s because the request pipeline breaks before it reaches the handler. For
  full parity we still need a real Node runtime (18/20) alongside Bun, at least until the
  Nitro/H3 team finishes their Bun support work or Bun closes the remaining gaps.

### Quick Install
macOS / Linux:
```bash
curl -fsSL https://raw.githubusercontent.com/kangu/CouchFusion/main/scripts/install.sh | bash
```

Windows (PowerShell):
```powershell
powershell -ExecutionPolicy Bypass -NoLogo -Command "iwr https://raw.githubusercontent.com/kangu/CouchFusion/main/scripts/install.ps1 -UseBasicParsing | iex"
```

> Tip: set `COUCHFUSION_VERSION=v0.1.0` (or similar) before running the installer to pin a specific release; otherwise the scripts install the latest GitHub release.

The installers place the binary in `~/.couchfusion/bin` (or `%USERPROFILE%\.couchfusion\bin`) and add that directory to your shell PATH if needed.

### Build from Source
```bash
# from repo root
go build -o couchfusion ./cli-init
```

Add the binary to your PATH or run it directly via `./couchfusion` from the build directory.

### Configuration File
The CLI reads configuration from `~/.couchfusion/config.yaml` by default. Override the path with `--config /path/to/config.yaml` on any command.

Sample YAML:
```yaml
repos:
  init:
    url: git@github.com:your-org/layers-base.git
    branch: main
    protocol: ssh
    authPrompt: false
  create_app:
    url: https://github.com/your-org/nuxt-app-starter.git
    branch: stable
    protocol: https
    authPrompt: true
  create_layer:
    url: git@github.com:your-org/nuxt-layer-starter.git
    branch: main
    protocol: ssh
modules:
  analytics:
    description: Analytics tracking utilities
    extends: "@layers/analytics"
  auth:
    description: Authentication middleware + helpers
    extends: "@layers/auth"
  content:
    description: Content editing workbench
workspace:
  defaultRoot: "/Users/me/Projects/nuxt-apps"
prompts:
  defaultLayerSelection:
    - analytics
    - auth
```

Key notes:
- `protocol` can be `ssh` or `https`. When `https` and `authPrompt: true`, the CLI requests username/password (or token) interactively for `git clone` and does **not** store credentials.
- Add additional modules under `modules` to match the layers in your ecosystem. If `extends` is omitted, it defaults to `@layers/<module>`.
- Set `workspace.defaultRoot` if you routinely run the CLI outside the workspace root.

---

## Global Behaviour
Every command performs the following before executing its workflow:
1. Loads configuration (YAML or JSON).
2. Checks whether the current directory already contains `/apps` and `/layers` (for non-`init` commands).
3. Runs prerequisite checks:
   - `bun --version`
   - GET `http://localhost:5984/_up`
4. Prints warnings for any missing prerequisites but continues execution unless configuration is invalid.

Use `--yes` on commands to skip interactive confirmations (planned enhancement) and `--config` to select alternate config files.

---

## Commands

### `couchfusion init`
Initialises a fresh workspace. Creates `/apps` and `/layers`, cloning the base layers repo into `/layers`.

```bash
couchfusion init \
  --config ~/.couchfusion/config.yaml \
  --path ~/Projects/new-workspace \
  --layers-branch develop \
  --force
```

Flags:
- `--path` – target directory to initialise (defaults to `.`).
- `--layers-branch` – override the branch defined in config for the layers clone.
- `--force` – re-clone if directories already exist but are empty. The CLI never deletes non-empty directories unless `--force` is provided.

Example output:
```
[WARN] bun is not available in PATH
[INFO] Creating apps directory... done
[INFO] Cloning layers repo (branch develop)... done
[INFO] Initialization complete.
```

### `couchfusion create_app`
Scaffolds a new Nuxt app inside `/apps/<app-name>` and records module selections.

```bash
couchfusion create_app \
  --name feedback-tool \
  --modules analytics,auth,content \
  --branch preview \
  --force
```

If `--name` or `--modules` are omitted, the CLI prompts for them. Module prompts list available options based on `modules` in config. The command produces two documentation files within the new app directory:

- `couchfusion.json` – records CLI version, app name, selected modules, timestamp.
- `docs/module_setup.json` – lists Nuxt `extends` entries and follow-up steps the developer must apply manually.

Example `docs/module_setup.json`:
```json
{
  "extends": [
    { "module": "@layers/analytics", "notes": "Adds analytics tracking functionality; ensure relevant env vars are configured." },
    { "module": "@layers/auth", "notes": "Provides authentication helpers and auth middleware integration." }
  ],
  "generatedAt": "2025-10-27T00:00:00Z",
  "cliVersion": "0.1.0",
  "selectedModules": ["analytics", "auth"],
  "nextSteps": [
    "Update nuxt.config.ts to include the listed extends entries.",
    "Review layer-specific documentation under /layers/<module>/docs for additional setup."
  ]
}
```

### `couchfusion create_layer`
Clones a new layer starter into `/layers/<layer-name>`.

```bash
couchfusion create_layer \
  --name analytics-pro \
  --branch feature/init \
  --force
```

After cloning, add the new layer to your config `modules` list manually so future app scaffolds can reference it.

---

## HTTPS Credential Prompts
When using HTTPS repositories with `authPrompt: true`, the CLI prompts for username and password/token. Entries are injected into the clone URL for the current command only and are not saved. If authentication fails, the CLI masks the password in error output and prompts to retry.

---

## Logging & Exit Codes
- Logs follow `[LEVEL] timestamp | version message` format.
- Use `--verbose` (future enhancement) for additional git details; by default git outputs are streamed directly from the `git clone` commands.
- Non-zero exit codes indicate fatal errors (config missing, git failure, invalid inputs). Warnings do not change the exit code.

---

## Troubleshooting
| Issue | Resolution |
|-------|------------|
| `config file not found` | Create the YAML or pass `--config` pointing to it. |
| `workspace not initialized` | Run `couchfusion init` from the workspace root first. |
| `git clone failed` | Verify repository URL, credentials, and access rights. For HTTPS, ensure tokens permit repo access. |
| `bun is not available in PATH` | Install bun or ensure it is discoverable; CLI continues but downstream tasks may fail. |
| `Unable to reach CouchDB` | Start CouchDB locally or update configuration if you intentionally work offline. |

---

## Development Notes
- `go build ./...` validates the code compiles; add unit tests under `internal/...` as the project evolves.
- Set `COUCHFUSION_VERSION` environment variable to override the embedded version string during development builds.
- The CLI never mutates Nuxt `nuxt.config.ts`; it surfaces instructions for manual changes via `docs/module_setup.json`.

For questions or enhancements, update the couchfusion PRD in `cli-init/docs/specs/cli_bootstrap_prd.md` and track changes under `cli-init/docs/implementation_results/` per project process.

---

## Release Guide

Follow these steps to cut and publish a new GitHub release (`kangu/CouchFusion`):

1. **Prep the version**
   - Update the `version` constant in `cli-init/main.go`.
   - Regenerate docs or changelog entries as needed.
   - Commit your changes with a message noting the new version.

2. **Tag the release**
   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

3. **Build binaries**
   - Run `./build.sh` to compile couchfusion for Linux (amd64/arm64), macOS, and Windows.
   - After each build, package the binary with the naming convention expected by the installers:
     ```bash
     tar -czf couchfusion_linux_amd64.tar.gz -C build/linux-x86 couchfusion
     tar -czf couchfusion_linux_arm64.tar.gz -C build/linux-arm couchfusion
     tar -czf couchfusion_darwin_amd64.tar.gz -C build/darwin-amd64 couchfusion
     tar -czf couchfusion_darwin_arm64.tar.gz -C build/darwin-arm64 couchfusion
     zip couchfusion_windows_amd64.zip build/windows/couchfusion.exe
     ```

4. **Publish on GitHub**
   - Create a new release for the tag.
   - Upload the packaged archives listed above.
   - Paste release notes / highlights; include installer commands for convenience.

5. **Verify installers**
   - Run the macOS/Linux one-liner and confirm `couchfusion --version` matches the release.
   - Run the PowerShell installer (or test via GitHub Actions/VM) to ensure Windows installs cleanly.

6. **Communicate**
   - Announce the release via your team channel.
   - Update any dependent documentation or references if paths or features changed.
