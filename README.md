# CouchFusion – Nuxt Apps + CouchDB

> The CouchApp vision evolved

CouchFusion is the opinionated developer platform that powers our Nuxt applications and shared CouchDB-backed layers. It combines a consistent monorepo structure (`/apps`, `/layers`, `/docs`), reusable feature packs, and automation that keeps every project aligned.

## Platform Overview
- **Stack** – [Nuxt 4](https://nuxt.com/) with [VueJS](https://vuejs.org/), TypeScript, [Bun](https://bun.sh/) + Node for local dev parity, and [CouchDB](https://couchdb.apache.org/) for persistence. ([Why not React?](./docs/specs/why-not-react.md))
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

### CouchDB

This amazing open source project is a rather unknown gem to most people and companies in the industry.
It is rock solid, runs on any platform, great performance and network-native will a full HTTP API which exposes
JSON data.

It's a perfect match for a generic website CMS/builder as it can integrate tightly with any kind of data thrown at it.

### Prerequisites
- `CouchDB` 3.x - couchfusion now manages local CouchDB setup during command preflight on macOS and Debian/Ubuntu. If CouchDB is already running at `http://127.0.0.1:5984`, the CLI shows the detected version and records first-time acknowledgement. If it is missing, the CLI can install and configure it in the background.
  - macOS installs through Homebrew.
  - Debian/Ubuntu installs through the Apache CouchDB apt repository.
  - Windows managed installation is not supported yet; use the [official installer](https://couchdb.apache.org/#download).
- `bun` and `node` - use this [script](https://raw.githubusercontent.com/kangu/CouchFusion/main/scripts/tooling/install_bun_node.sh) for installing both
> With only Bun on the box you can install dependencies and run Nuxt’s CLI,
  but Bun’s Node-compat layer isn’t yet complete enough to host Nitro/H3 in dev mode. You’ll
  hit 400s because the request pipeline breaks before it reaches the handler. For
  full parity we still need a real Node runtime (18/20) alongside Bun, at least until the
  Nitro/H3 team finishes their Bun support work or Bun closes the remaining gaps.
- `git` available in your PATH with access to the starter repositories

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

### Interactive UI
By default, commands run with a Bubble Tea + Lip Gloss terminal interface when executed in an interactive TTY. Set `COUCHFUSION_NO_TUI=1` to disable the TUI and fall back to plain prompts.

### Build from Source

#### Prerequisites
- Go 1.21+

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
  new:
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
- If no config file exists at `~/.couchfusion/config.yaml` and no `--config` path is provided, the CLI uses the embedded `internal/config/default_config.yaml` and prints a warning.

---

## Global Behaviour
Every command performs the following before executing its workflow:
1. Loads configuration (YAML or JSON).
2. Falls back to the embedded default config when no user config is present.
3. Checks whether the current directory already contains `/apps` and `/layers` for workspace commands that operate on existing workspaces.
4. Runs prerequisite checks:
   - `bun --version`
5. Runs the managed CouchDB preflight:
   - probes `http://127.0.0.1:5984/_up`
   - shows the detected CouchDB version when already running
   - records first-time acknowledgement in `~/.couchfusion/couchdb.env`
   - installs and starts CouchDB on macOS or Debian/Ubuntu when missing and approved
   - prints the generated admin password after installation and waits for Enter before continuing
6. Prints warnings for any missing non-fatal prerequisites but continues execution unless configuration, input validation, or CouchDB setup fails.

Use `--config` to select an alternate config file. Use `--yes` or `-y` to approve CouchDB setup in non-interactive automation. `--verbose` and `--quiet` remain planned logging enhancements.

Managed CouchDB setup writes `~/.couchfusion/couchdb.env` with `0600` permissions:

```dotenv
COUCHDB_URL=http://127.0.0.1:5984
COUCHDB_ADMIN_USER=admin
COUCHDB_ADMIN_AUTH=<base64 admin:generated-password>
COUCHDB_ACCEPTED_VERSION=<detected version>
COUCHDB_ACCEPTED_AT=<timestamp>
```

The auth layer setup reuses `COUCHDB_ADMIN_AUTH` from the process environment or this file before prompting.

---

## Commands

### `couchfusion init`
Initialises a fresh workspace. Creates `/apps` and `/layers`, cloning the base layers repo into `/layers`.

```bash
couchfusion init \
  --config ~/.couchfusion/config.yaml \
  --path ~/Projects/new-workspace \
  --layers-branch develop \
  --force \
  --yes
```

Flags:
- `--path` – target directory to initialise (defaults to `.`).
- `--layers-branch` – override the branch defined in config for the layers clone.
- `--force` – re-clone if directories already exist but are empty. The CLI never deletes non-empty directories unless `--force` is provided.
- `--yes`, `-y` – approve managed CouchDB acknowledgement or installation without interactive prompts.

Example output:
```
[WARN] bun is not available in PATH
[INFO] Creating apps directory... done
[INFO] Cloning layers repo (branch develop)... done
[INFO] Initialization complete.
```

### `couchfusion new`
Scaffolds a new Nuxt app inside `/apps/<app-name>`, records module selections, and wires the selected layers into the cloned starter.

```bash
couchfusion new \
  --name feedback-tool \
  --modules analytics,auth,content \
  --branch preview \
  --force \
  --yes
```

You can also pass the app name positionally:

```bash
couchfusion new feedback-tool
```

If `--name` or `--modules` are omitted, the CLI prompts for them. In an interactive terminal, the command uses a Bubble Tea + Lip Gloss TUI with arrow-key module selection and Space toggles. Set `COUCHFUSION_NO_TUI=1` to use plain prompts.

The command performs the following post-clone setup:

- Reinitializes the cloned app as a fresh git repository.
- Adds selected layer package dependencies to `package.json` as `@my/<module>: "file:../../layers/<module>"`.
- Rewrites the top-level `extends` array in `nuxt.config.ts` to include only the selected `../../layers/<module>` entries.
- Runs selected layer parameter handlers. Today, the `auth` layer reuses `COUCHDB_ADMIN_AUTH` from the environment or `~/.couchfusion/couchdb.env` when available, otherwise asks for CouchDB admin credentials, writes `COUCHDB_ADMIN_AUTH` and `COUCHDB_COOKIE_SECRET` to `.env`, and ensures the matching CouchDB user document exists.
- Writes app metadata and module setup documentation.

The command produces two documentation files within the new app directory:

- `couchfusion.json` – records CLI version, app name, selected modules, timestamp.
- `docs/module_setup.json` – records selected module metadata, resolved layer entries, and post-create follow-up steps.

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
    "Review nuxt.config.ts and package.json to confirm selected layer wiring.",
    "Review layer-specific documentation under /layers/<module>/docs for additional setup."
  ]
}
```

After a successful run:

```bash
cd apps/feedback-tool
bun install
bun run dev
```

### `couchfusion create_layer`
Clones a new layer starter into `/layers/<layer-name>` and reinitializes it as a fresh git repository.

```bash
couchfusion create_layer \
  --name analytics-pro \
  --branch feature/init \
  --force \
  --yes
```

After cloning, add the new layer to your config `modules` list manually so future app scaffolds can reference it.

---

## HTTPS Credential Prompts
When using HTTPS repositories with `authPrompt: true`, the CLI prompts for username and password/token. Entries are injected into the clone URL for the current command only and are not saved. If authentication fails, the CLI masks the password in error output and prompts to retry.

---

## Logging & Exit Codes
- Logs follow `[LEVEL] timestamp | version message` format.
- Additional `--verbose` and `--quiet` modes are planned; by default git output is streamed during clone operations.
- Non-zero exit codes indicate fatal errors (config missing, git failure, invalid inputs). Warnings do not change the exit code.

---

## Troubleshooting
| Issue | Resolution |
|-------|------------|
| `config file not found` | Create the YAML or pass `--config` pointing to it. |
| `workspace not initialized` | Run `couchfusion init` from the workspace root first. |
| `git clone failed` | Verify repository URL, credentials, and access rights. For HTTPS, ensure tokens permit repo access. |
| `bun is not available in PATH` | Install bun or ensure it is discoverable; CLI continues but downstream tasks may fail. |
| `couchdb install or acknowledgement requires interactive confirmation or --yes` | Re-run in an interactive terminal or pass `--yes` / `-y` for automation. |
| `managed CouchDB installation is supported on macOS and Debian/Ubuntu only` | Install CouchDB manually for your OS and re-run the command. |

---

## Development Notes
- `go build ./...` validates the code compiles; add unit tests under `internal/...` as the project evolves.
- Set `COUCHFUSION_VERSION` environment variable to override the embedded version string during development builds.
- The CLI currently mutates cloned app files during `new`: `package.json`, `.env` for selected layer parameters, `nuxt.config.ts`, `couchfusion.json`, and `docs/module_setup.json`.
- Managed CouchDB setup lives under `internal/couchdb`; tests use injected probes/installers and do not run system package managers.
- Use `GOCACHE=<repo>/.gocache go test ./...` when the default Go cache is outside the writable sandbox.

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

## 🔗 Related Repositories

This CLI depends on the following projects:

- [layers](https://github.com/kangu/CouchFusion-BaseLayers) — provides core functionality and shared logic.
- [starter-app](https://github.com/kangu/CouchFusion-StarterApp) — starter boilerplate app.

Together, these form the **CouchFusion toolchain**.
