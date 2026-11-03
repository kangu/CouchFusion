# CouchFusion Web Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a secure, loopback-only `couchfusion ui` control plane that manages explicitly registered workspaces, provisioning, configuration, environment health, package tasks, development processes, and live activity while preserving the existing CLI.

**Architecture:** Extract presentation-neutral Go services from the current command/TUI code, then place CLI and HTTP adapters over those services. Serve a statically generated Nuxt 4 SPA from the Go binary, use JSON for snapshots/mutations, and use SSE for bounded job/process events.

**Tech Stack:** Go 1.24 standard library, existing Git/Bun/CouchDB adapters, YAML v3, Nuxt 4, Vue 3, TypeScript, Bun, Vitest, Vue Test Utils, Playwright.

## Global Constraints

- Bind only to `127.0.0.1`; phase 1 has no LAN or remote mode.
- Workspace registration is explicit and persisted in `~/.couchfusion/workspaces.json`.
- Web actions never recursively delete workspace, site, or layer directories and never expose force overwrite.
- Browser requests cannot provide arbitrary executables, shell fragments, command arguments, or operational roots after registration.
- Secrets never appear in ordinary API DTOs, job snapshots, events, logs, or process environments returned to the client.
- Browser disconnection does not cancel work; server shutdown stops managed development processes.
- Existing `init`, `new`, and `create_layer` CLI behavior remains supported throughout implementation.
- Each backend capability receives direct tests before an HTTP endpoint exposes it.
- Frontend production assets are embedded into the single Go release binary.

---

## File Structure

### Go application and adapters

- `internal/app/types.go`: shared application request/result types and stable error codes.
- `internal/app/service.go`: composition root for registry, config, project, job, process, environment, and provisioning use cases.
- `internal/registry/registry.go`: workspace registry model, canonicalization, validation, and persistence.
- `internal/registry/registry_test.go`: registry behavior and atomic-write tests.
- `internal/project/inspect.go`: safe workspace/site/layer inspection.
- `internal/project/inspect_test.go`: filesystem fixture tests.
- `internal/project/tasks.go`: curated package task discovery.
- `internal/project/tasks_test.go`: script allow-list tests.
- `internal/jobs/manager.go`: finite job state machine, workspace locks, cancellation, events, and redaction.
- `internal/jobs/manager_test.go`: transitions, conflict, cancellation, bounds, and race-oriented tests.
- `internal/processes/manager.go`: managed long-running site processes and event buffers.
- `internal/processes/manager_test.go`: lifecycle, duplicate prevention, shutdown, and URL extraction tests.
- `internal/config/config.go`: existing base model and loading behavior.
- `internal/config/layers.go`: embedded/global/workspace merge with source attribution.
- `internal/config/write.go`: validated atomic configuration writes.
- `internal/config/config_test.go`: base and layered configuration tests.
- `internal/workspace/workspace.go`: explicit-root lower-level provisioning methods.
- `internal/workspace/workspace_test.go`: provisioning mutation tests using temp fixtures and fake Git operations.
- `internal/web/server.go`: loopback listener, timeouts, graceful shutdown, embedded SPA routing.
- `internal/web/session.go`: launch-token exchange, cookie, origin/host checks, and security headers.
- `internal/web/routes.go`: `/api/v1` router and response/error helpers.
- `internal/web/workspaces.go`: workspace, project, config, health, and provisioning handlers.
- `internal/web/activity.go`: job/process snapshot, cancellation, control, and SSE handlers.
- `internal/web/server_test.go`: security, routing, DTO, confinement, and SSE tests.
- `internal/web/assets.go`: `go:embed` production asset filesystem.
- `internal/web/dist/.gitkeep`: build destination present before the first frontend production build.
- `internal/cli/run.go`: command dispatch returning errors rather than exiting.
- `internal/cli/ui.go`: `couchfusion ui` flag parsing and lifecycle.
- `internal/cli/run_test.go`: command compatibility and UI flag tests.
- `main.go`: minimal process entry point and exit-code formatting.

### Nuxt client

- `web/package.json`: frontend scripts and pinned dependencies.
- `web/nuxt.config.ts`: SPA/static generation, strict dev settings, and output configuration.
- `web/tsconfig.json`: Nuxt TypeScript project references.
- `web/app.vue`: application shell.
- `web/assets/css/main.css`: CouchFusion tokens, responsive layout, forms, cards, logs, and focus styles.
- `web/layouts/default.vue`: sidebar/header/content layout.
- `web/middleware/workspace.ts`: registered workspace route validation.
- `web/types/api.ts`: versioned API DTOs matching Go response contracts.
- `web/composables/useApi.ts`: authenticated fetch and structured errors.
- `web/composables/useActivity.ts`: centralized job/process snapshots and SSE subscriptions.
- `web/composables/useWorkspace.ts`: active workspace state and refresh actions.
- `web/pages/index.vue`: explicit workspace launcher.
- `web/pages/workspaces/[workspaceId]/index.vue`: workspace overview.
- `web/pages/workspaces/[workspaceId]/sites/index.vue`: site inventory.
- `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`: site tasks, process controls, URLs, and logs.
- `web/pages/workspaces/[workspaceId]/layers.vue`: layer inventory and creation flow.
- `web/pages/workspaces/[workspaceId]/config.vue`: layered structured configuration editor.
- `web/pages/activity.vue`: active/recent jobs and process activity.
- `web/pages/system.vue`: runtime health and managed setup actions.
- `web/components/workspaces/WorkspaceDialog.vue`: register/initialize forms.
- `web/components/provision/SiteWizard.vue`: site creation review/progress flow.
- `web/components/provision/LayerWizard.vue`: layer creation review/progress flow.
- `web/components/activity/LogViewer.vue`: bounded searchable event rendering.
- `web/components/activity/JobStatus.vue`: progress, conflict, error, and cancellation UI.
- `web/components/sites/TaskList.vue`: curated task actions.
- `web/components/system/HealthCard.vue`: status and repair action.
- `web/tests/*.test.ts`: component/composable tests.
- `web/e2e/control-plane.spec.ts`: browser acceptance flow.

### Build and documentation

- `build.sh`: frontend production build before cross-compiling Go.
- `release.sh`: verify embedded assets before archives are produced.
- `scripts/install.sh`: unchanged behavior, documented UI command.
- `scripts/install.ps1`: unchanged behavior, documented UI command.
- `README.md`: install, launch, security, workspace, task, and troubleshooting documentation.
- `docs/implementation_results/2026-07-13-web-control-plane-phase-1.md`: delivered behavior and verification record.

---

### Task 1: Protect Existing CLI Behavior and Create a Testable Dispatch Boundary

**Files:**
- Create: `internal/cli/run.go`
- Create: `internal/cli/run_test.go`
- Modify: `main.go`

**Interfaces:**
- Produces: `cli.Run(ctx context.Context, args []string, io cli.IO, commands cli.Commands) error`
- Produces: `cli.IO{In io.Reader, Out io.Writer, Err io.Writer}`
- Produces: typed `cli.ExitError{Code int, Err error}` for process exit mapping.
- Produces: `cli.Commands` callbacks that temporarily adapt the existing workflows; Task 8 replaces those callbacks with `app.Service` calls.
- Consumes later: Task 12 adds `ui` dispatch to this boundary.

- [x] **Step 1: Write failing command-boundary tests**

Add table-driven tests that call `Run` without mutating `os.Args` and assert `version`, missing command, and unknown command behavior:

```go
func TestRunVersion(t *testing.T) {
    var out bytes.Buffer
    err := Run(context.Background(), []string{"version"}, IO{Out: &out, Err: io.Discard}, Commands{})
    if err != nil { t.Fatal(err) }
    if got := strings.TrimSpace(out.String()); got != Version { t.Fatalf("version = %q", got) }
}

func TestRunRejectsUnknownCommand(t *testing.T) {
    err := Run(context.Background(), []string{"unknown"}, IO{Out: io.Discard, Err: io.Discard}, Commands{})
    var exitErr ExitError
    if !errors.As(err, &exitErr) || exitErr.Code != 2 { t.Fatalf("error = %#v", err) }
}
```

- [x] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/cli -run 'TestRun' -count=1`

Expected: FAIL because `internal/cli` and `Run` do not exist.

- [x] **Step 3: Implement the minimal command boundary**

Define `Version = "0.5.0"`, normalize nil IO streams, return dispatch errors rather than calling `os.Exit`, and move command selection into `internal/cli`. Keep existing command implementations callable during this first step through injected callbacks; do not change their workflow behavior until Task 8.

```go
type IO struct { In io.Reader; Out io.Writer; Err io.Writer }
type Commands struct {
    Init func(context.Context, []string) error
    New func(context.Context, []string) error
    CreateLayer func(context.Context, []string) error
}

type ExitError struct { Code int; Err error }
func (e ExitError) Error() string { return e.Err.Error() }
func (e ExitError) Unwrap() error { return e.Err }

func Run(ctx context.Context, args []string, streams IO, commands Commands) error {
    streams = streams.defaults()
    if len(args) == 0 { return ExitError{Code: 2, Err: errors.New("command required")} }
    switch args[0] {
    case "version", "--version", "-v":
        _, err := fmt.Fprintln(streams.Out, Version)
        return err
    case "init":
        if commands.Init == nil { return ExitError{Code: 1, Err: errors.New("init command is unavailable")} }
        return commands.Init(ctx, args[1:])
    case "new":
        if commands.New == nil { return ExitError{Code: 1, Err: errors.New("new command is unavailable")} }
        return commands.New(ctx, args[1:])
    case "create_layer":
        if commands.CreateLayer == nil { return ExitError{Code: 1, Err: errors.New("create_layer command is unavailable")} }
        return commands.CreateLayer(ctx, args[1:])
    default:
        return ExitError{Code: 2, Err: fmt.Errorf("unknown command: %s", args[0])}
    }
}
```

Make `main()` call `cli.Run` with callbacks that invoke the existing `runInit`, `runNew`, and `runCreateLayer` behavior. Print one safe dispatch error to stderr and exit using `ExitError.Code` or `1`. Task 8 removes process exits from the workflow implementations after application services exist.

- [x] **Step 4: Run compatibility verification**

Run: `go test ./internal/cli ./... -count=1 && go vet ./...`

Expected: all existing and new tests PASS; vet produces no diagnostics.

- [x] **Step 5: Commit the command boundary**

```bash
git add main.go internal/cli/run.go internal/cli/run_test.go
git commit -m "refactor: add testable CLI command boundary"
```

### Task 2: Make Workspace Provisioning Explicit-Root and Prompt-Free

**Files:**
- Create: `internal/workspace/workspace_test.go`
- Modify: `internal/workspace/workspace.go`
- Modify: `internal/workspace/parameters.go`
- Modify: `internal/workspace/auth_credentials.go`
- Modify: `internal/workspace/init_tui.go`
- Modify: `internal/workspace/new_tui.go`
- Modify: `internal/workspace/create_layer_tui.go`

**Interfaces:**
- Produces: `workspace.Init(ctx, cfg, InitRequest, Dependencies) error`
- Produces: `workspace.CreateSite(ctx, cfg, CreateSiteRequest, Dependencies) error`
- Produces: `workspace.CreateLayer(ctx, cfg, CreateLayerRequest, Dependencies) error`
- Produces request types with explicit `Root string`; web callers cannot set `Force` true.
- Produces `workspace.EventSink` for safe structured progress.

- [x] **Step 1: Write failing explicit-root and non-destructive tests**

Use `t.TempDir`, fixture `apps/layers`, and an injected fake cloner. Verify creation uses `request.Root` even after changing process cwd, rejects existing non-empty targets when `Force` is false, and never prompts when credentials are supplied in the request context.

```go
func TestCreateSiteUsesExplicitRoot(t *testing.T) {
    root := newWorkspaceFixture(t)
    other := t.TempDir()
    t.Chdir(other)
    clone := &fakeCloner{writePackage: true}
    err := CreateSite(context.Background(), testConfig(), CreateSiteRequest{
        Root: root, Name: "alpha", Modules: []string{"content"},
    }, Dependencies{Clone: clone.Clone})
    if err != nil { t.Fatal(err) }
    if clone.Target != filepath.Join(root, "apps", "alpha") { t.Fatalf("target = %s", clone.Target) }
}
```

- [x] **Step 2: Run the focused workspace tests and verify failure**

Run: `go test ./internal/workspace -run 'Test(CreateSite|CreateLayer|Init)' -count=1`

Expected: FAIL because the request/dependency APIs do not exist.

- [x] **Step 3: Implement requests, dependencies, and event sink**

```go
type InitRequest struct { Root, LayerBranch string; Force bool }
type CreateSiteRequest struct {
    Root, Name, Branch string
    Modules []string
    Force bool
    Auth *AuthCredentials
}
type CreateLayerRequest struct { Root, Name, Branch string; Force bool }

type Event struct { Code, Message string; Level EventLevel }
type EventSink interface { Emit(Event) }
type Dependencies struct {
    Clone func(context.Context, string, string, string, string, bool, ...gitutil.CloneOption) error
    Events EventSink
}
```

Default missing dependencies to current production adapters. Move stdin collection into CLI/TUI adapters; application functions validate all required values and return typed errors. Keep legacy `RunInit`, `RunNew`, and `RunCreateLayer` wrappers temporarily so existing callers stay green.

- [x] **Step 4: Update TUI callers and run regression tests**

Run: `go test ./internal/workspace ./... -count=1`

Expected: PASS, including prior TUI-independent tests.

- [x] **Step 5: Commit explicit-root provisioning**

```bash
git add internal/workspace
git commit -m "refactor: make workspace provisioning explicit and reusable"
```

### Task 3: Add the Explicit Workspace Registry

**Files:**
- Create: `internal/registry/registry.go`
- Create: `internal/registry/registry_test.go`

**Interfaces:**
- Produces: `registry.Entry`, `registry.Store`, `registry.Service`.
- Produces: `List`, `Register`, `Forget`, `Relink`, and `Touch` operations.
- Consumes: filesystem validation of direct `apps` and `layers` directories.

- [x] **Step 1: Write failing registry tests**

Cover `0600` persistence, stable IDs, canonical roots, duplicate rejection, invalid workspace rejection, missing-root list behavior, forget-without-delete, and relink.

```go
func TestForgetDoesNotDeleteWorkspace(t *testing.T) {
    root := newValidWorkspace(t)
    service := newRegistryService(t)
    entry, err := service.Register(context.Background(), root, "Demo")
    if err != nil { t.Fatal(err) }
    if err := service.Forget(context.Background(), entry.ID); err != nil { t.Fatal(err) }
    if _, err := os.Stat(root); err != nil { t.Fatalf("workspace removed: %v", err) }
}
```

- [x] **Step 2: Run registry tests and verify failure**

Run: `go test ./internal/registry -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement atomic registry persistence**

```go
type Entry struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Root string `json:"root"`
    CreatedAt time.Time `json:"createdAt"`
    LastOpenedAt time.Time `json:"lastOpenedAt"`
}

type Store struct { Path string }
type Service struct { Store Store; Now func() time.Time; NewID func() (string, error) }
```

Canonicalize with `filepath.Abs`, `filepath.Clean`, and `filepath.EvalSymlinks` when the root exists. Persist JSON through a same-directory temporary file, `Chmod(0600)`, `Sync`, close, and rename. Create the parent directory with `0700`.

- [x] **Step 4: Run registry tests and race check**

Run: `go test -race ./internal/registry -count=1`

Expected: PASS with no race report.

- [x] **Step 5: Commit registry support**

```bash
git add internal/registry
git commit -m "feat: add explicit workspace registry"
```

### Task 4: Add Layered Configuration and Atomic Writers

**Files:**
- Create: `internal/config/layers.go`
- Create: `internal/config/write.go`
- Create: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

**Interfaces:**
- Produces: `config.LoadEffective(globalPath, workspaceRoot string) (Effective, error)`.
- Produces: `config.Effective{Config Config, Sources SourceMap}`.
- Produces: `config.WriteGlobal` and `config.WriteWorkspace` with validation and atomic replacement.

- [x] **Step 1: Write failing precedence and writer tests**

Cover embedded-only, global overrides, workspace overrides, repo/module map merging, default-selection replacement, source attribution, invalid-file preservation, and `0600` global writes.

```go
func TestLoadEffectiveTracksWorkspaceSource(t *testing.T) {
    root := t.TempDir()
    writeYAML(t, filepath.Join(root, "couchfusion.yaml"), "repos:\n  new:\n    branch: preview\n")
    got, err := LoadEffective("", root)
    if err != nil { t.Fatal(err) }
    if got.Config.Repos["new"].Branch != "preview" { t.Fatalf("branch = %q", got.Config.Repos["new"].Branch) }
    if got.Sources["repos.new.branch"] != SourceWorkspace { t.Fatalf("source = %q", got.Sources["repos.new.branch"]) }
}
```

- [x] **Step 2: Run config tests and verify failure**

Run: `go test ./internal/config -count=1`

Expected: FAIL because layered APIs do not exist.

- [x] **Step 3: Implement field-aware merging and source tracking**

Use pointer-bearing override DTOs so omitted booleans and empty values differ from explicit values. Merge embedded, global, and workspace documents in order. Validate the effective result with the existing rules plus default-module membership checks.

```go
type Source string
const (SourceEmbedded Source = "embedded"; SourceGlobal Source = "global"; SourceWorkspace Source = "workspace")
type SourceMap map[string]Source
type Effective struct { Config Config `json:"config"`; Sources SourceMap `json:"sources"` }
```

Writers marshal only the selected layer, reject credential-bearing URL userinfo, validate a simulated effective merge, and atomically replace the destination.

- [x] **Step 4: Run config and full regression tests**

Run: `go test ./internal/config ./... -count=1`

Expected: PASS.

- [x] **Step 5: Commit layered configuration**

```bash
git add internal/config
git commit -m "feat: add layered CouchFusion configuration"
```

### Task 5: Inspect Workspaces, Sites, Layers, and Curated Tasks

**Files:**
- Create: `internal/project/inspect.go`
- Create: `internal/project/inspect_test.go`
- Create: `internal/project/tasks.go`
- Create: `internal/project/tasks_test.go`

**Interfaces:**
- Produces: `project.Inspector.Workspace(ctx, registry.Entry) (Workspace, error)`.
- Produces: `project.Inspector.Site(ctx, root, siteID string) (Site, error)`.
- Produces: `project.Catalog.Tasks(site Site) []Task`.
- Produces task IDs resolved server-side to exact Bun commands.

- [x] **Step 1: Write failing safe-inspection tests**

Create filesystem fixtures for valid/malformed metadata, sites/layers sorting, missing roots, direct-child-only listing, symlink escape rejection, dependency state, and package scripts.

```go
func TestCatalogRejectsUndeclaredTask(t *testing.T) {
    catalog := Catalog{}
    site := Site{Scripts: map[string]string{"dev": "nuxt dev"}}
    if _, err := catalog.Resolve(site, "script:publish"); !errors.Is(err, ErrTaskNotFound) {
        t.Fatalf("error = %v", err)
    }
}
```

- [x] **Step 2: Run project tests and verify failure**

Run: `go test ./internal/project -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement read-only inspection and task resolution**

Use `os.ReadDir` direct children, `json.Decoder`, canonical path containment, and warning accumulation. Define task commands internally:

```go
type Task struct { ID, Name, Kind string; LongRunning bool }
type Command struct { Executable string; Args []string; Dir string }

func (Catalog) Resolve(site Site, taskID string) (Command, error) {
    if taskID == "install" { return Command{Executable: "bun", Args: []string{"install"}, Dir: site.Root}, nil }
    name, ok := strings.CutPrefix(taskID, "script:")
    if !ok || site.Scripts[name] == "" { return Command{}, ErrTaskNotFound }
    return Command{Executable: "bun", Args: []string{"run", name}, Dir: site.Root}, nil
}
```

- [x] **Step 4: Run project tests**

Run: `go test -race ./internal/project -count=1`

Expected: PASS.

- [x] **Step 5: Commit project inspection**

```bash
git add internal/project
git commit -m "feat: inspect CouchFusion projects and tasks"
```

### Task 6: Implement Finite Jobs, Locks, Events, and Redaction

**Files:**
- Create: `internal/jobs/manager.go`
- Create: `internal/jobs/manager_test.go`

**Interfaces:**
- Produces: `jobs.Manager.Start`, `Get`, `Cancel`, `Subscribe`, and `Shutdown`.
- Produces: one active mutation lock per workspace.
- Produces: bounded `jobs.Event` streams with monotonic IDs.
- Produces: memory-only one-time `SecretHandoff` scoped to initiating session.

- [x] **Step 1: Write failing job-state tests**

Cover success, failure, panic recovery, cancellation, workspace conflict, browser-independent execution, bounded events, redaction, subscriber replay, shutdown, and one-time secret consumption.

```go
func TestManagerRejectsConcurrentWorkspaceMutation(t *testing.T) {
    manager := NewManager(Options{MaxEvents: 100})
    release := make(chan struct{})
    first, err := manager.Start(Request{WorkspaceID: "w1", Run: func(context.Context, Reporter) error { <-release; return nil }})
    if err != nil { t.Fatal(err) }
    _, err = manager.Start(Request{WorkspaceID: "w1", Run: func(context.Context, Reporter) error { return nil }})
    var conflict ConflictError
    if !errors.As(err, &conflict) || conflict.ActiveJobID != first.ID { t.Fatalf("error = %#v", err) }
    close(release)
}
```

- [x] **Step 2: Run jobs tests and verify failure**

Run: `go test ./internal/jobs -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement synchronized state and redaction**

```go
type Status string
const (Queued Status = "queued"; Running Status = "running"; Succeeded Status = "succeeded"; Failed Status = "failed"; Cancelled Status = "cancelled")
type Event struct { ID uint64; Time time.Time; Level, Code, Text string }
type Snapshot struct { ID, Type, WorkspaceID, SiteID string; Status Status; Events []Event; Error *SafeError }
type Request struct { Type, WorkspaceID, SiteID, SessionID string; Run func(context.Context, Reporter) error }
```

Guard maps and ring buffers with a mutex, execute each job in its own goroutine, recover panics at the job boundary, and redact URL userinfo plus configured secret keys before storage. The secret handoff is never part of `Snapshot`.

- [x] **Step 4: Run race-enabled job tests**

Run: `go test -race ./internal/jobs -count=1`

Expected: PASS with no race report.

- [x] **Step 5: Commit the job engine**

```bash
git add internal/jobs
git commit -m "feat: add secure workspace job engine"
```

### Task 7: Implement Managed Site Processes

**Files:**
- Create: `internal/processes/manager.go`
- Create: `internal/processes/manager_test.go`

**Interfaces:**
- Produces: `processes.Manager.Start`, `Stop`, `Restart`, `Get`, `Subscribe`, and `Shutdown`.
- Consumes: server-resolved `project.Command`; never accepts shell text.
- Produces: bounded process events and advisory localhost URLs.

- [x] **Step 1: Write failing lifecycle tests**

Use the test process helper pattern (`os.Args[0] -test.run=TestHelperProcess --`) to verify start/output/exit, duplicate rejection, interrupt then kill, restart, URL extraction, bounded output, and shutdown of all children.

```go
func TestManagerRejectsDuplicateSiteProcess(t *testing.T) {
    manager := newTestManager(t)
    first, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "dev", Command: helperCommand("wait")})
    if err != nil { t.Fatal(err) }
    _, err = manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "dev", Command: helperCommand("wait")})
    if !errors.Is(err, ErrAlreadyRunning) { t.Fatalf("error = %v", err) }
    _ = manager.Stop(context.Background(), first.ID)
}
```

- [x] **Step 2: Run process tests and verify failure**

Run: `go test ./internal/processes -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement process ownership and event capture**

Start with `exec.CommandContext` and explicit `Dir`. Connect stdout/stderr pipes, scan bounded lines, redact before buffering, and parse only `http://localhost`, `http://127.0.0.1`, and equivalent HTTPS URLs. Store the original resolved command privately for restart; exclude it from public snapshots.

- [x] **Step 4: Run race-enabled process tests**

Run: `go test -race ./internal/processes -count=1`

Expected: PASS with no leaked helper processes.

- [x] **Step 5: Commit managed processes**

```bash
git add internal/processes
git commit -m "feat: manage CouchFusion site processes"
```

### Task 8: Compose Application Services and Refactor CLI Workflows

**Files:**
- Create: `internal/app/types.go`
- Create: `internal/app/service.go`
- Create: `internal/app/service_test.go`
- Modify: `internal/cli/run.go`
- Modify: `main.go`

**Interfaces:**
- Produces: one `app.Service` used by CLI and web adapters.
- Produces: typed error codes `validation`, `conflict`, `not_found`, `unavailable`, `filesystem`, `git`, `process`, `config`, and `internal`.
- Consumes: registry, config, project, jobs, processes, workspace, Git/Bun/CouchDB managers.

- [x] **Step 1: Write failing service orchestration tests**

Test initialize-and-register success/failure, site/layer jobs, health without installation, explicit install jobs, task job/process routing, workspace forget safety, and error mapping with fakes.

```go
func TestInitializeRegistersOnlyAfterProvisioningSuccess(t *testing.T) {
    fakeRegistry := &registryFake{}
    service := newServiceForTest(t, withInitError(errors.New("clone failed")), withRegistry(fakeRegistry))
    _, err := service.InitializeWorkspace(context.Background(), InitializeWorkspaceRequest{Root: t.TempDir(), Name: "Demo"}, "session")
    if err == nil { t.Fatal("expected error") }
    if fakeRegistry.RegisterCalls != 0 { t.Fatalf("register calls = %d", fakeRegistry.RegisterCalls) }
}
```

- [x] **Step 2: Run app tests and verify failure**

Run: `go test ./internal/app -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement the composition service**

Define narrow interfaces per dependency and immutable request DTOs. Convert lower-level errors once at the application boundary. Route finite work to `jobs.Manager`, long-running `dev/preview` tasks to `processes.Manager`, and read operations directly to inspectors.

- [x] **Step 4: Move CLI commands onto the service**

Make `cli.Run` parse flags and collect terminal inputs, then call `app.Service`. Keep Bubble Tea as a CLI presentation adapter. Remove `logging.Fatalf` and workflow-level `os.Exit` from reusable paths.

- [x] **Step 5: Run all backend tests**

Run: `go test -race ./... -count=1 && go vet ./...`

Expected: PASS with no race or vet diagnostics.

- [x] **Step 6: Commit shared application services**

```bash
git add internal/app internal/cli main.go internal/workspace
git commit -m "refactor: share CouchFusion application services"
```

### Task 9: Build the Secured Loopback HTTP Foundation

**Files:**
- Create: `internal/web/server.go`
- Create: `internal/web/session.go`
- Create: `internal/web/routes.go`
- Create: `internal/web/server_test.go`
- Create: `internal/web/assets.go`
- Create: `internal/web/dist/.gitkeep`

**Interfaces:**
- Produces: `web.NewServer(web.Options) (*Server, error)`.
- Produces: `Server.Start(ctx) (LaunchInfo, error)` and `Server.Shutdown(ctx) error`.
- Produces: launch token exchange and authenticated `/api/v1/bootstrap`.

- [x] **Step 1: Write failing server-security tests**

Cover loopback address enforcement, token entropy/one-time exchange, `HttpOnly`/`SameSite=Strict` cookie, token-free redirect, origin and host rejection, JSON content type, body limits, CSP/frame headers, unauthenticated API/SSE rejection, and SPA fallback.

```go
func TestMutationRejectsForeignOrigin(t *testing.T) {
    server := newAuthenticatedTestServer(t)
    req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/register", strings.NewReader(`{"root":"/tmp/x"}`))
    req.Header.Set("Origin", "https://attacker.example")
    req.Header.Set("Content-Type", "application/json")
    req.AddCookie(server.Cookie)
    rr := httptest.NewRecorder()
    server.Handler.ServeHTTP(rr, req)
    if rr.Code != http.StatusForbidden { t.Fatalf("status = %d", rr.Code) }
}
```

- [x] **Step 2: Run web tests and verify failure**

Run: `go test ./internal/web -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement listener, middleware, and static fallback**

Use `net.Listen("tcp", "127.0.0.1:<port>")`, `crypto/rand` for a 32-byte token, SHA-256 token storage, strict cookie flags, request limits, explicit timeouts, and a `http.ServeMux`. Serve only embedded assets; unknown non-API GET routes return SPA `index.html`.

- [x] **Step 4: Run web security tests**

Run: `go test -race ./internal/web -count=1`

Expected: PASS.

- [x] **Step 5: Commit the HTTP foundation**

```bash
git add internal/web
git commit -m "feat: add secure CouchFusion UI server"
```

### Task 10: Expose Workspace, Configuration, Provisioning, Job, Process, and SSE APIs

**Files:**
- Create: `internal/web/workspaces.go`
- Create: `internal/web/activity.go`
- Modify: `internal/web/routes.go`
- Modify: `internal/web/server_test.go`

**Interfaces:**
- Produces: the `/api/v1` routes enumerated in the approved design.
- Consumes: `app.Service` only; handlers do not call filesystem/process packages directly.
- Produces: stable response envelope and safe field/error DTOs.

- [x] **Step 1: Write failing API contract tests**

Test register/list/get/forget/relink, config read/write, health, create site/layer, task run, process controls, job cancellation, one-time secret consumption, SSE replay, 404/409/422 mapping, and JSON secret absence.

```go
func TestWorkspaceDTOContainsNoCredentials(t *testing.T) {
    rr := performAuthenticated(t, testServerWithWorkspace(), http.MethodGet, "/api/v1/workspaces/w1", nil)
    if rr.Code != http.StatusOK { t.Fatalf("status = %d", rr.Code) }
    body := strings.ToLower(rr.Body.String())
    for _, forbidden := range []string{"adminauth", "password", "cookie_secret", "authorization"} {
        if strings.Contains(body, forbidden) { t.Fatalf("response contains %q: %s", forbidden, body) }
    }
}
```

- [x] **Step 2: Run API tests and verify failure**

Run: `go test ./internal/web -run 'Test(Workspace|Config|Health|Provision|Job|Process|SSE)' -count=1`

Expected: FAIL because the resource handlers do not exist.

- [x] **Step 3: Implement DTO mapping and handlers**

Define request structs with `DisallowUnknownFields`, validate route IDs, call application methods, and map errors centrally. Return `202` with job/process snapshots for asynchronous actions. SSE sends `id`, `event`, and JSON `data`, flushes after each event, respects request cancellation, and replays from `Last-Event-ID`.

- [x] **Step 4: Run API, race, and serialization tests**

Run: `go test -race ./internal/web ./internal/app -count=1`

Expected: PASS.

- [x] **Step 5: Commit phase-1 API routes**

```bash
git add internal/web
git commit -m "feat: expose CouchFusion control plane API"
```

### Task 11: Scaffold the Nuxt SPA and Typed Client Foundation

**Files:**
- Create: `web/package.json`
- Create: `web/nuxt.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/app.vue`
- Create: `web/assets/css/main.css`
- Create: `web/layouts/default.vue`
- Create: `web/types/api.ts`
- Create: `web/composables/useApi.ts`
- Create: `web/composables/useActivity.ts`
- Create: `web/composables/useWorkspace.ts`
- Create: `web/tests/useApi.test.ts`
- Create: `web/tests/useActivity.test.ts`

**Interfaces:**
- Consumes: authenticated relative `/api/v1` JSON and SSE endpoints.
- Produces: typed API methods, centralized activity subscriptions, and the responsive app shell.

- [x] **Step 1: Create the frontend manifest and configuration**

Use scripts `dev`, `build`, `generate`, `test`, `test:run`, `typecheck`, and `e2e`. Configure `ssr: false`, global CSS, strict TypeScript, and generated output copied to `internal/web/dist` by the root build.

- [x] **Step 2: Write failing composable tests**

Test structured API errors, credentials inclusion, one active SSE connection per resource, reconnect from last event ID, snapshot refresh after disconnect, and teardown.

```ts
it('maps field errors from the API', async () => {
  mockFetch.mockRejectedValue({ data: { error: { code: 'validation', fields: { root: 'Workspace is invalid' } } } })
  const api = useApi()
  await expect(api.post('/workspaces/register', { root: '/missing' }))
    .rejects.toMatchObject({ code: 'validation', fields: { root: 'Workspace is invalid' } })
})
```

- [x] **Step 3: Run frontend tests and verify failure**

Run: `cd web && bun install && bun run test:run`

Expected: FAIL because the composables and shell are incomplete.

- [x] **Step 4: Implement the typed client and activity hub**

Use `$fetch` with `credentials: 'same-origin'`, normalize response errors, and key SSE subscriptions by `job:<id>`/`process:<id>`. Store active workspace ID in Nuxt state and local storage only after validating it against `/workspaces`.

- [x] **Step 5: Implement the application shell**

Create accessible navigation for Workspaces, Activity, and System, with workspace-scoped links appearing only when a workspace is active. Include visible connection state, responsive sidebar behavior, focus-visible styles, reduced-motion support, and non-generic CouchFusion visual tokens.

- [x] **Step 6: Run frontend foundation verification**

Run: `cd web && bun run test:run && bun run typecheck && bun run generate`

Expected: tests and typecheck PASS; `.output/public/index.html` exists.

- [x] **Step 7: Commit the Nuxt foundation**

```bash
git add web
git commit -m "feat: scaffold embedded CouchFusion Nuxt UI"
```

### Task 12: Implement Workspace, Site, Configuration, Activity, and System UI Flows

**Files:**
- Create: `web/pages/index.vue`
- Create: `web/pages/workspaces/[workspaceId]/index.vue`
- Create: `web/pages/workspaces/[workspaceId]/sites/index.vue`
- Create: `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`
- Create: `web/pages/workspaces/[workspaceId]/layers.vue`
- Create: `web/pages/workspaces/[workspaceId]/config.vue`
- Create: `web/pages/activity.vue`
- Create: `web/pages/system.vue`
- Create: `web/middleware/workspace.ts`
- Create: `web/components/workspaces/WorkspaceDialog.vue`
- Create: `web/components/provision/SiteWizard.vue`
- Create: `web/components/provision/LayerWizard.vue`
- Create: `web/components/activity/LogViewer.vue`
- Create: `web/components/activity/JobStatus.vue`
- Create: `web/components/sites/TaskList.vue`
- Create: `web/components/system/HealthCard.vue`
- Create: `web/tests/workspaces.test.ts`
- Create: `web/tests/site-detail.test.ts`
- Create: `web/tests/config.test.ts`
- Create: `web/tests/activity.test.ts`

**Interfaces:**
- Consumes: all phase-1 API DTOs and composables from Task 11.
- Produces: all phase-1 browser interactions from the acceptance criteria.

- [x] **Step 1: Write failing component-flow tests**

Cover explicit register/initialize, unavailable workspace, forget copy, site wizard review, existing-target conflict, task controls, process status/logs/URLs, layered source badges, config validation, health repair confirmation, job cancellation, and reconnect state.

- [x] **Step 2: Run focused component tests and verify failure**

Run: `cd web && bun run test:run -- workspaces site-detail config activity`

Expected: FAIL because pages/components do not exist.

- [x] **Step 3: Implement workspace launcher and overview**

Render explicit cards with name, canonical path, availability, last opened time, and actions. Registration accepts a user-entered absolute path; initialization collects path/name/branch and shows a review step. “Forget workspace” copy states that files remain untouched.

- [x] **Step 4: Implement site/layer provisioning and detail flows**

Use multi-step dialogs with field validation, module choices, review, job progress, and success navigation. Site detail exposes curated task buttons, process lifecycle, detected URL links, dependency/Git/layer summaries, and bounded logs.

- [x] **Step 5: Implement config, activity, and system flows**

Show effective values with `embedded`, `global`, or `workspace` source badges. Save structured global/workspace forms only after review. Activity merges finite jobs and live processes. System shows independent Git/Bun/Node/CouchDB health cards and explicit repair confirmation.

- [x] **Step 6: Run frontend test and accessibility verification**

Run: `cd web && bun run test:run && bun run typecheck && bun run generate`

Expected: PASS with no Vue/TypeScript errors.

- [x] **Step 7: Commit phase-1 UI flows**

```bash
git add web
git commit -m "feat: add CouchFusion workspace and site UI"
```

### Task 13: Embed Assets and Add `couchfusion ui`

**Files:**
- Modify: `internal/web/assets.go`
- Modify: `internal/cli/run.go`
- Create: `internal/cli/ui.go`
- Modify: `internal/cli/run_test.go`
- Modify: `main.go`
- Modify: `.gitignore`

**Interfaces:**
- Produces: `couchfusion ui [--port 0] [--no-open] [--config path]`.
- Consumes: generated files copied into `internal/web/dist`.
- Produces: graceful signal shutdown and browser launch.

- [x] **Step 1: Write failing UI command tests**

Inject listener, browser opener, and server factory. Test default random port, explicit port, `--no-open`, invalid port, loopback URL, cancellation shutdown, and process manager shutdown.

- [x] **Step 2: Run CLI UI tests and verify failure**

Run: `go test ./internal/cli -run 'TestUI' -count=1`

Expected: FAIL because UI dispatch does not exist.

- [x] **Step 3: Implement asset copy/embed and UI lifecycle**

Embed `dist/*` with `//go:embed all:dist`. Add `ui` dispatch to `cli.Run`, construct application services, start the server, open the tokenized URL with OS-specific commands, and wait for context cancellation. Opening failure is a warning with the URL, not a server failure.

- [x] **Step 4: Build and smoke-test one binary**

Run: `cd web && bun run generate && rm -rf ../internal/web/dist/* && cp -R .output/public/. ../internal/web/dist/ && cd .. && go test ./... -count=1 && go build -o /tmp/couchfusion-ui . && /tmp/couchfusion-ui version`

Expected: all tests PASS and version prints `0.5.0`.

- [x] **Step 5: Commit embedded UI command**

```bash
git add internal/web internal/cli main.go .gitignore
git commit -m "feat: serve embedded UI from CouchFusion binary"
```

### Task 14: Add Browser Acceptance Coverage and Harden Security/Shutdown

**Files:**
- Create: `web/playwright.config.ts`
- Create: `web/e2e/control-plane.spec.ts`
- Modify: `web/package.json`
- Modify: `internal/web/server_test.go`
- Modify: `internal/jobs/manager_test.go`
- Modify: `internal/processes/manager_test.go`

**Interfaces:**
- Verifies: full browser/API contract and all security/process acceptance criteria.

- [x] **Step 1: Add end-to-end fixture server and failing browser flow**

Create a temporary valid workspace and fake external adapters, start a real loopback server, exchange the launch token, register the fixture, inspect it, create a fake site job, run a finite task, start/stop a helper dev process, save workspace config, and forget the workspace.

- [x] **Step 2: Run the browser test and verify failure**

Run: `cd web && bun run e2e`

Expected: FAIL at the first unimplemented or mismatched acceptance behavior.

- [x] **Step 3: Close contract and security gaps**

Fix only behaviors exposed by the acceptance test. Add explicit regression tests for symlink escape, foreign origin, wrong host, oversized JSON, secret field serialization, concurrent mutation, tab disconnect, shutdown cancellation, and process cleanup.

- [x] **Step 4: Run complete race and browser verification**

Run: `go test -race ./... -count=1 && go vet ./... && cd web && bun run test:run && bun run typecheck && bun run generate && bun run e2e`

Expected: all commands PASS and no managed child remains after the test server exits.

- [x] **Step 5: Commit acceptance hardening**

```bash
git add internal web
git commit -m "test: verify CouchFusion control plane flows"
```

### Task 15: Update Build, Release, and User Documentation

**Files:**
- Modify: `build.sh`
- Modify: `release.sh`
- Modify: `README.md`
- Create: `docs/implementation_results/2026-07-13-web-control-plane-phase-1.md`

**Interfaces:**
- Produces: all existing platform archives as single binaries containing the production UI.
- Produces: launch, security, workspace, task, configuration, and troubleshooting documentation.

- [x] **Step 1: Add build preflight assertions**

Make `build.sh` run frontend install/generate, replace `internal/web/dist` with `.output/public`, assert `index.html`, then run Go tests and cross-compilation. Make `release.sh` reject artifacts whose binary does not contain the embedded index marker or whose expected archive is absent.

- [x] **Step 2: Update the README**

Document `couchfusion ui`, `--port`, `--no-open`, explicit workspace registration, local-only security, non-destructive forget semantics, layered configuration, site tasks/process lifetime, and the requirement that UI-started processes stop with the server. Align Go prerequisites with `go.mod`.

- [x] **Step 3: Write the implementation result record**

Record delivered architecture, user-visible behavior, security controls, exact verification commands, release changes, and explicit phase-2/3 deferrals.

- [x] **Step 4: Run final release verification**

Run: `./build.sh && ./release.sh v0.6.0 && go test -race ./... -count=1 && go vet ./... && cd web && bun run test:run && bun run typecheck && bun run e2e`

Expected: all tests PASS; Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 artifacts are produced with the embedded UI.

- [x] **Step 5: Review repository state and commit documentation/release work**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only intended build, release, README, and implementation-result changes are uncommitted.

```bash
git add build.sh release.sh README.md docs/implementation_results/2026-07-13-web-control-plane-phase-1.md
git commit -m "docs: ship CouchFusion web control plane"
```

---

## Final Verification Checklist

- [x] `go test -race ./... -count=1` passes.
- [x] `go vet ./...` passes.
- [x] `cd web && bun run test:run` passes.
- [x] `cd web && bun run typecheck` passes.
- [x] `cd web && bun run generate` produces `index.html`.
- [x] `cd web && bun run e2e` passes against the production-like loopback server.
- [x] Existing CLI smoke tests for `version`, `init`, `new`, and `create_layer` pass.
- [x] The server binds only to loopback and rejects foreign origin/host requests.
- [x] Workspace/site/layer paths cannot escape registered roots.
- [x] No web route exposes force overwrite, recursive deletion, arbitrary commands, or secrets.
- [x] Closing the browser does not cancel work; server shutdown stops managed dev processes.
- [x] Cross-platform release archives contain one executable with embedded UI assets.
