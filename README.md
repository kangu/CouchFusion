# cli-init – Nuxt Apps + Layers Bootstrapper

`cli-init` is a Go-based command line tool that standardises the bootstrapping of the NuxtJS apps + shared layers ecosystem. It provisions the `/apps` and `/layers` workspace structure, clones starter repositories, and records module selections so developers know how to wire Nuxt `extends` entries manually.

---

## Installation & Setup

### Prerequisites
- Go 1.21+
- `git` available in your PATH with access to the starter repositories
- `bun` installed (required by downstream Nuxt apps; CLI only warn if missing)
- CouchDB reachable at `http://localhost:5984` (CLI warns if unavailable)

### Build the CLI
```bash
# from repo root
go build -o cli-init ./cli-init
# or within the folder
cd cli-init
go build ./...
```

Add the binary to your PATH or run it directly via `./cli-init` from the build directory.

### Configuration File
The CLI reads configuration from `~/.cli-init/config.yaml` by default. Override the path with `--config /path/to/config.yaml` on any command.

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

### `cli-init init`
Initialises a fresh workspace. Creates `/apps` and `/layers`, cloning the base layers repo into `/layers`.

```bash
cli-init init \
  --config ~/.cli-init/config.yaml \
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

### `cli-init create_app`
Scaffolds a new Nuxt app inside `/apps/<app-name>` and records module selections.

```bash
cli-init create_app \
  --name feedback-tool \
  --modules analytics,auth,content \
  --branch preview \
  --force
```

If `--name` or `--modules` are omitted, the CLI prompts for them. Module prompts list available options based on `modules` in config. The command produces two documentation files within the new app directory:

- `cli-init.json` – records CLI version, app name, selected modules, timestamp.
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

### `cli-init create_layer`
Clones a new layer starter into `/layers/<layer-name>`.

```bash
cli-init create_layer \
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
| `workspace not initialized` | Run `cli-init init` from the workspace root first. |
| `git clone failed` | Verify repository URL, credentials, and access rights. For HTTPS, ensure tokens permit repo access. |
| `bun is not available in PATH` | Install bun or ensure it is discoverable; CLI continues but downstream tasks may fail. |
| `Unable to reach CouchDB` | Start CouchDB locally or update configuration if you intentionally work offline. |

---

## Development Notes
- `go build ./...` validates the code compiles; add unit tests under `internal/...` as the project evolves.
- Set `CLI_INIT_VERSION` environment variable to override the embedded version string during development builds.
- The CLI never mutates Nuxt `nuxt.config.ts`; it surfaces instructions for manual changes via `docs/module_setup.json`.

For questions or enhancements, update the PRD in `cli-init/docs/specs/cli_bootstrap_prd.md` and track changes under `cli-init/docs/implementation_results/` per project process.
