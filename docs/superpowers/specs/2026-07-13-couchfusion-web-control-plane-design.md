# CouchFusion Web Control Plane Design

**Date:** 2026-07-13

**Status:** Proposed for implementation

## 1. Summary

CouchFusion will evolve from a three-command Go CLI into a local, browser-based control plane while preserving the existing CLI. A new `couchfusion ui` command will start a loopback-only Go HTTP server, open an embedded Nuxt single-page application, and manage explicitly registered CouchFusion workspaces and their sites.

The CLI, Bubble Tea interface, and HTTP API will become adapters over shared Go application services. Phase 1 delivers workspace registration and creation, site and layer provisioning, layered configuration, prerequisite health, curated package tasks, long-running development processes, live logs, and operation history. Phase 2 adds CouchDB administration. Phase 3 adds CouchFusion-aware content and site administration.

## 2. Product Decisions

- The UI is a global manager for multiple CouchFusion workspaces.
- Workspace registration is entirely explicit. CouchFusion does not scan the filesystem for workspaces.
- The server is single-user and local-only. LAN and remote access are out of scope.
- Removing a workspace from the UI only forgets its registry entry. It never deletes the workspace directory.
- Site/layer deletion and forced overwrites are not exposed in the phase-1 web UI.
- Jobs continue if the browser tab closes.
- UI-started development processes stop gracefully when the `couchfusion ui` server exits.
- Persistent background daemons are out of scope.
- Configuration is layered: global defaults plus optional workspace overrides.
- The task UI highlights standard package actions and exposes declared `package.json` scripts, but it does not expose an arbitrary browser shell.
- The application is shipped as one Go binary with embedded static Nuxt assets.
- The existing `init`, `new`, and `create_layer` commands remain supported.

## 3. Goals

### 3.1 Phase-1 goals

- Launch a polished local UI with `couchfusion ui`.
- Register, create, inspect, select, and forget workspace references.
- Show workspace sites, layers, effective configuration, environment health, and active work.
- Create sites and layers through browser-native forms that use the same domain workflows as the CLI.
- Install site dependencies and run declared package scripts.
- Start, stop, and restart development servers with live output and detected URLs.
- Stream structured progress and bounded logs for provisioning and task jobs.
- Protect secrets, filesystem paths, and destructive operations at the server boundary.
- Preserve single-binary installation and cross-platform release artifacts.
- Introduce testable application boundaries before adding CouchDB/content administration.

### 3.2 Long-term goals

- Add local CouchDB database, user, permission, configuration, health, and maintenance tools.
- Add CouchFusion-aware document, page, content, preview, validation, and publishing tools.
- Allow future integrations to contribute inspections and actions through stable Go interfaces and typed client contracts.

## 4. Non-Goals

- Remote or multi-user hosting.
- Authentication accounts, teams, roles, or cloud synchronization.
- Implicit filesystem crawling.
- Deleting workspace, site, or layer directories from the web UI.
- An arbitrary terminal emulator or arbitrary command execution endpoint.
- Persistent process management after the UI server exits.
- Replacing the existing CLI or TUI.
- Phase-1 CouchDB document/configuration administration.
- Phase-1 content editing or publishing.
- A general third-party plugin runtime in phase 1.

## 5. Current-State Findings

The repository already contains useful domain behavior but couples it to command and terminal concerns:

- `main.go` combines flag parsing, preflight orchestration, prompts, TUI routing, logging, and process exits.
- `internal/workspace` contains provisioning behavior but uses `os.Getwd`, stdin/stdout prompts, process environment, and synchronous functions.
- Bubble Tea models encode useful wizard flows but are presentation-specific.
- Git clone supports redirected output, while other workflow steps do not emit structured progress.
- Git, Bun, and CouchDB managers have injectable probes/installers and are suitable starting points for application adapters.
- Configuration can be loaded and validated but cannot be written or layered.
- `couchfusion.json` provides site metadata, but no workspace/site discovery service exists.
- The existing `--force` path can delete non-empty targets and must not be reachable from phase-1 web endpoints.
- Workspace orchestration, config, Git execution, and shared UI have insufficient automated coverage for a server-facing refactor.

## 6. Architecture

### 6.1 Runtime shape

```text
CLI flags / Bubble Tea                 Embedded Nuxt SPA
          |                                   |
          v                                   v
      CLI adapter                    HTTP + SSE adapter
          |                                   |
          +---------- typed requests ----------+
                              |
                              v
                    Application services
          +-------------------+--------------------+
          |                   |                    |
   Workspace/config      Job/process engine   Health/task catalog
          |                   |                    |
          +-------------------+--------------------+
                              |
                              v
          Filesystem / Git / Bun / CouchDB / YAML adapters
```

The application core must not read stdin, write presentation text to stdout, depend on the process working directory, call `os.Exit`, or serialize secrets. Every operation receives explicit dependencies and an explicit workspace identity/root.

### 6.2 Deployment shape

`couchfusion ui` will:

1. Load global CouchFusion state and configuration.
2. Bind an HTTP listener to `127.0.0.1` on the requested or an automatically selected port.
3. Generate an unguessable per-launch session token.
4. Construct the application services, job manager, process manager, and API router.
5. Serve static assets embedded with `go:embed`.
6. Open the tokenized URL in the default browser unless `--no-open` is set.
7. Shut down gracefully on signal, stopping managed site processes before exit.

Supported flags:

```text
couchfusion ui [--port 0] [--no-open] [--config path]
```

No phase-1 flag will bind to a non-loopback address.

### 6.3 Source boundaries

The implementation should converge on these responsibilities:

- `internal/app`: presentation-neutral use cases and typed inputs/results.
- `internal/registry`: global workspace registry persistence.
- `internal/project`: workspace and site inspection models.
- `internal/jobs`: job state, events, cancellation, history, and locking.
- `internal/processes`: managed long-running package processes.
- `internal/config`: global/workspace load, merge, validation, source tracking, and atomic writes.
- `internal/workspace`: lower-level provisioning operations without prompts or implicit cwd.
- `internal/web`: HTTP routing, middleware, API DTOs, SSE, and embedded assets.
- `web/`: Nuxt SPA source, tests, and generated client types.
- `main.go` or `internal/cli`: CLI command parsing and presentation adapters.

The implementation plan may introduce these boundaries incrementally. Existing packages should not be reorganized unless the change creates a required seam.

## 7. Domain Model and Persistence

### 7.1 Workspace registry

The registry is stored at `~/.couchfusion/workspaces.json` with directory permissions `0700` and file permissions `0600`.

Each entry contains:

```json
{
  "id": "stable-random-id",
  "name": "Marketing Sites",
  "root": "/absolute/path/to/workspace",
  "createdAt": "2026-07-13T12:00:00Z",
  "lastOpenedAt": "2026-07-13T12:00:00Z"
}
```

Rules:

- `root` is canonicalized to an absolute path with symlinks resolved when possible.
- Duplicate canonical roots are rejected.
- Adding an existing workspace requires readable `apps/` and `layers/` directories.
- Creating a workspace runs initialization first and registers it only after success.
- Forgetting removes only the registry record.
- Missing or moved roots remain visible as unavailable and can be forgotten or relinked.
- Registry writes use a temporary file, `fsync` where supported, and atomic rename.

### 7.2 Workspace inspection

A workspace view contains:

- Registry identity and availability.
- Root path and effective configuration state.
- Environment health summary.
- Sites under `apps/`.
- Layers under `layers/`.
- Active and recent jobs.
- Active managed processes.

The server lists only direct child directories of `apps/` and `layers/`. It does not recursively scan outside a registered root.

### 7.3 Site model

Site inspection reads, without executing project code:

- `couchfusion.json` when present.
- `package.json` name, dependencies, devDependencies, package-manager declaration, and scripts.
- Selected layers inferred from CouchFusion metadata and local `file:../../layers/*` dependencies.
- Dependency installation state such as the presence of `node_modules` and lockfiles.
- Git repository status through explicit Git adapter calls.
- Managed development process state and detected URLs.

Malformed metadata is reported as a site warning and does not make the whole workspace unavailable.

### 7.4 Configuration layers

Configuration precedence is:

1. Embedded defaults.
2. `~/.couchfusion/config.yaml` global configuration.
3. `<workspace>/couchfusion.yaml` workspace overrides.

The effective model returns each editable value together with its source. Arrays and maps use explicit semantics:

- Repository objects merge by command key.
- Module objects merge by module key.
- A workspace module may override description/extends values or add a module.
- `defaultLayerSelection` replaces the inherited array when declared.
- A workspace override cannot persist Git credentials.

Writes are structured, validated before replacement, permission-safe, and atomic. The UI provides forms for repositories, branches, protocols, module catalog entries, default selection, and workspace display name. A read-only YAML preview is allowed; arbitrary raw YAML editing is not part of phase 1.

## 8. Application Services

### 8.1 Workspace service

Use cases:

- List registered workspaces.
- Register an existing workspace.
- Initialize and register a new workspace.
- Read a workspace dashboard.
- Update workspace display metadata.
- Forget a workspace.
- Relink a missing workspace to a new root after validation.

### 8.2 Provisioning service

Use cases:

- Create a site from the configured starter and selected modules.
- Create a layer from the configured starter.
- Validate proposed names, branches, modules, credentials, and target paths before job creation.

Provisioning receives an explicit workspace root. It reuses Git, package mutation, Nuxt configuration, metadata, and layer-parameter behavior extracted from `internal/workspace`.

Phase-1 web requests always set destructive overwrite behavior to false. An existing target produces a typed conflict error.

### 8.3 Environment service

Health checks for Git, Bun, Node, and CouchDB are independent read operations. The dashboard distinguishes:

- Available with version/path.
- Missing but installable.
- Missing and unsupported for managed installation.
- Present but unhealthy.
- Check failed with a safe diagnostic.

Managed installation is launched as an explicit job after user confirmation; opening a workspace never installs software automatically. Installation events must not contain generated passwords or credential values. If CouchDB installation generates a password, the job stores it in a memory-only, session-scoped handoff that can be consumed exactly once after successful completion. The secret is not included in the job snapshot, event stream, or history and is discarded when consumed, when the session ends, or when the server exits.

### 8.4 Task catalog

The site task catalog is derived from `package.json` and exposes typed actions:

- `install`: `bun install`.
- `dev`: the `dev` script when declared.
- `build`: the `build` script when declared.
- `preview`: the `preview` script when declared.
- `test`: the `test` script when declared.
- `script`: another exact key declared in `package.json.scripts`.

The browser never supplies an executable, arbitrary argument array, shell fragment, or working directory. The server resolves every command from the inspected site and runs it with `exec.CommandContext` without a shell.

### 8.5 Job manager

Provisioning, installation, build, test, and other finite mutations run as jobs.

A job has:

- ID, type, workspace ID, optional site ID/path.
- Queued, running, succeeded, failed, or cancelled status.
- Created, started, and completed timestamps.
- Structured progress stage and optional percentage.
- Bounded ordered events with level, message code, safe text, and timestamp.
- Redacted error summary.
- Cancellation capability.

Rules:

- At most one mutation job runs per workspace.
- Conflicting requests return HTTP 409 with the active job ID.
- Read-only inspection continues while a mutation runs.
- Cancellation propagates through `context.Context` to Git and child processes.
- A browser disconnect does not cancel a job.
- Job history is kept in memory in phase 1, bounded by count and log size.
- Server restart does not claim interrupted jobs succeeded; persistence of job history is deferred.

SSE is used for job and process event streams. Clients reconnect with the last event ID and can fall back to fetching the current job snapshot.

### 8.6 Process manager

Development servers are distinct from finite jobs.

The process manager:

- Starts only a catalogued site script.
- Allows at most one managed process per site and script category.
- Captures stdout/stderr into a bounded ring buffer.
- Emits lifecycle and log events over SSE.
- Detects localhost URLs from output as advisory links.
- Supports stop and restart.
- Sends an interrupt first, then uses a bounded grace period before force termination.
- Stops all managed processes during server shutdown.
- Marks processes exited when the child terminates independently.

Builds, tests, and installs remain finite jobs even if their output is streamed similarly.

## 9. HTTP API

All endpoints live under `/api/v1`. API DTOs are separate from domain objects so filesystem handles, commands, and credentials cannot be serialized accidentally.

Representative resources:

```text
GET    /api/v1/bootstrap
GET    /api/v1/workspaces
POST   /api/v1/workspaces/register
POST   /api/v1/workspaces/initialize
GET    /api/v1/workspaces/{workspaceId}
PATCH  /api/v1/workspaces/{workspaceId}
DELETE /api/v1/workspaces/{workspaceId}
POST   /api/v1/workspaces/{workspaceId}/relink

GET    /api/v1/workspaces/{workspaceId}/config
PUT    /api/v1/workspaces/{workspaceId}/config
GET    /api/v1/workspaces/{workspaceId}/health
POST   /api/v1/workspaces/{workspaceId}/sites
POST   /api/v1/workspaces/{workspaceId}/layers

GET    /api/v1/workspaces/{workspaceId}/sites/{siteId}
GET    /api/v1/workspaces/{workspaceId}/sites/{siteId}/tasks
POST   /api/v1/workspaces/{workspaceId}/sites/{siteId}/tasks/{taskId}/run
POST   /api/v1/workspaces/{workspaceId}/sites/{siteId}/processes/{taskId}/start
POST   /api/v1/processes/{processId}/stop
POST   /api/v1/processes/{processId}/restart

GET    /api/v1/jobs/{jobId}
POST   /api/v1/jobs/{jobId}/cancel
GET    /api/v1/jobs/{jobId}/events
POST   /api/v1/jobs/{jobId}/secret/consume
GET    /api/v1/processes/{processId}/events
```

Mutation endpoints return `202 Accepted` plus a job/process resource when work is asynchronous. The secret-consume endpoint succeeds only for the browser session that started the job, only after successful completion, and only once. Validation uses stable machine-readable error codes and field errors. Errors do not include command lines containing credentials, environment dumps, or unrestricted filesystem details.

## 10. Security Model

### 10.1 Network and session boundary

- Listen only on `127.0.0.1`.
- Generate at least 256 bits of random session entropy per launch.
- Put the token in the initial browser URL and exchange it for a `HttpOnly`, `SameSite=Strict` session cookie.
- Remove the token from the address bar after exchange.
- Require the session cookie for API and SSE requests.
- Validate `Origin` and `Host` for every mutation and event connection.
- Reject cross-origin requests and unsupported content types.
- Apply request body and header size limits and server read/write/idle timeouts.
- Send a restrictive Content Security Policy and prevent framing.

### 10.2 Filesystem boundary

- Resolve every workspace through the registry; API callers never submit an operational root path after registration.
- Resolve site/layer IDs against direct children of their registered directories.
- Canonicalize and verify paths remain within their expected root before reads or writes.
- Reject symlink escapes for mutation targets.
- Do not serve arbitrary local files through the HTTP server.
- Do not expose force overwrite or recursive delete endpoints.

### 10.3 Command boundary

- Never invoke user-provided shell text.
- Resolve executables and arguments server-side from typed actions.
- Use explicit working directories under registered workspaces.
- Apply cancellation and shutdown timeouts.
- Redact credentials, URL userinfo, authorization headers, cookies, and known secret environment keys before event storage.

### 10.4 Secret boundary

- Never return `COUCHDB_ADMIN_AUTH`, decoded passwords, cookie secrets, Git tokens, or full process environments from ordinary APIs.
- Password inputs are write-only request fields and are excluded from logs and job snapshots.
- Persisted CouchDB auth retains the existing `0600` storage behavior.
- Future phase-2 APIs must use dedicated redacted DTOs rather than serializing `couchdb.Credentials`.

## 11. Nuxt Application Design

The UI is a Nuxt application generated as a client-side SPA and embedded into the Go binary. It uses TypeScript and the project’s Bun toolchain. SSR is disabled because all data is local, session-bound, and unavailable during a static build.

### 11.1 Information architecture

- **Workspace launcher:** registered workspace cards, availability, recent access, register, and initialize actions.
- **Workspace overview:** health, sites, layers, active work, and primary actions.
- **Sites:** searchable site list and per-site details.
- **Site detail:** metadata, selected layers, Git summary, dependencies, task controls, live processes, URLs, and logs.
- **Layers:** installed layer inventory and create-layer flow.
- **Configuration:** effective values, source labels, global/workspace editing, validation, and YAML preview.
- **Activity:** active and recent jobs with progress, cancellation, output, and errors.
- **System:** CouchFusion version, runtime health, install actions, and diagnostic summary.

Phase-2 CouchDB and phase-3 Content entries are not shown as disabled navigation in phase 1; they appear when their capabilities exist.

### 11.2 Interaction patterns

- Use dashboard transparency inspired by Nuxt DevTools: relationships and status are visible before actions are offered.
- Use Vue CLI UI’s useful separation of projects, configuration, tasks, prompts, and output without adopting its plugin runtime.
- Provisioning uses reviewable multi-step forms: inputs, modules/options, validation summary, confirmation, progress, result.
- Destructive-looking actions state their exact effect. “Forget workspace” explicitly says files remain untouched.
- Health failures provide a diagnosis and a separate repair action rather than triggering installation implicitly.
- Job/process state survives route changes and browser reconnections.
- Logs are searchable, level-filterable, copyable, and bounded; secrets are already redacted server-side.

### 11.3 Client state

- Server data is fetched through typed composables and treated as authoritative.
- A small client store tracks the active workspace, navigation preferences, and live connection state.
- Job/process subscriptions are centralized so route changes do not create duplicate SSE connections.
- Optimistic updates are limited to reversible presentation state; filesystem/process mutations wait for server acknowledgement.

## 12. Error Handling and Recovery

Errors are categorized as validation, conflict, unavailable dependency, filesystem, Git/network, process, configuration, unauthorized session, and internal.

- Validation errors remain on the form with field-specific messages.
- Workspace mutation conflicts link to the active job.
- Partial provisioning failures retain logs and report the target path without automatically deleting it.
- A failed workspace initialization is not registered.
- A failed site/layer creation is inspectable in activity history; retry requires resolving the existing target conflict outside the phase-1 UI.
- Invalid workspace configuration leaves the last valid effective configuration active where possible and surfaces the invalid file location.
- SSE disconnects trigger bounded reconnection and snapshot recovery.
- Server shutdown stops accepting mutations, cancels finite jobs, stops managed processes, flushes safe logs, and closes the listener.
- Unexpected panics are recovered at job and HTTP boundaries, recorded safely, and do not terminate the whole control plane.

## 13. Testing Strategy

### 13.1 Go tests

- Unit tests for registry canonicalization, duplicate detection, atomic persistence, missing roots, and relinking.
- Unit tests for layered config precedence, merge semantics, validation, source tracking, and atomic writes.
- Workspace tests using temporary directories and fake Git/process/CouchDB adapters.
- Job tests for state transitions, workspace locking, cancellation, bounded events, and redaction.
- Process tests for start/stop/restart, duplicate prevention, output capture, URL detection, and shutdown cleanup.
- HTTP handler tests for DTOs, status codes, field errors, token exchange, origin enforcement, path confinement, and request limits.
- Tests that API serialization never includes known credential fields.
- Regression tests for existing CLI behavior through the shared services.
- Race-enabled tests for jobs, process events, and SSE subscribers.

### 13.2 Nuxt tests

- Unit/component tests for launcher, health cards, task controls, provisioning forms, activity state, and error rendering.
- Mocked API contract tests for composables and SSE reconnection.
- Browser tests for register workspace, initialize workspace, create site, run finite task, start/stop dev server, edit workspace config, and forget workspace.
- Accessibility checks for keyboard navigation, form labels, focus management, status announcements, and color contrast.

### 13.3 Release verification

- Build the SPA and verify embedded asset fallback routing.
- Run Go tests, frontend tests, `go vet`, and a production frontend build.
- Cross-compile all existing release targets with embedded assets.
- Smoke-test a release binary on macOS and Linux: start UI, exchange session, register a fixture workspace, inspect it, and shut down cleanly.

## 14. Delivery Sequence

1. Establish application boundaries and protect existing CLI behavior with tests.
2. Add registry and explicit workspace-root services.
3. Add job events, locking, cancellation, and redaction.
4. Refactor provisioning and prerequisite workflows behind application services.
5. Add site inspection, task catalog, and managed processes.
6. Add the secured loopback HTTP API and SSE.
7. Scaffold the Nuxt SPA and implement workspace/site/activity/configuration flows.
8. Embed frontend assets and add `couchfusion ui` lifecycle behavior.
9. Update build, release, installation, and user documentation.
10. Run full security, concurrency, cross-platform, and end-to-end verification.

Each sequence item must leave the CLI buildable and tested. HTTP endpoints are introduced only after the corresponding service has direct tests.

## 15. Phase-1 Acceptance Criteria

- `couchfusion ui` opens a token-protected browser UI served by the CouchFusion binary on loopback.
- A user can explicitly register an existing valid workspace and see its sites and layers.
- A user can initialize a new workspace and see it registered only after success.
- A user can forget a workspace without any filesystem deletion.
- Missing registered workspaces are represented without crashing the dashboard.
- A user can create a site and layer without the server relying on its process cwd or stdin.
- Concurrent mutations in one workspace are rejected with a visible active-job reference.
- A user can view environment health and explicitly start supported repair/install jobs.
- A user can install dependencies, run declared finite scripts, and see live bounded logs.
- A user can start, stop, and restart a site dev process and open detected URLs.
- Closing a browser tab does not stop work; exiting the UI server stops managed dev processes.
- Global and workspace configuration can be edited through structured forms with source attribution.
- The API cannot run arbitrary commands, escape registered roots, delete project directories, or return stored secrets.
- Existing CLI commands continue to work through the same application services.
- Release archives remain single-binary installations with embedded UI assets.

## 16. Future Phases

### Phase 2: CouchDB administration

- Database inventory and health.
- CouchDB users and permissions.
- Safe `_config` management with CouchFusion section conventions.
- Maintenance and diagnostic actions.
- Dedicated redacted credential handling.
- Photon and relevant database links.

### Phase 3: Content and site administration

- CouchFusion document and page inventory.
- Schema-aware content editing and validation.
- Preview and publishing workflows.
- Revision/conflict visibility.
- Site-specific administrative extensions built on stable application-service and client contracts.

These phases extend the same workspace identity, job, security, event, and UI navigation foundations. They do not expand phase 1 into a remotely accessible or multi-user service.
