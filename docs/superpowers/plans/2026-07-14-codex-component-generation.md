# Codex-Assisted Content Component Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent, interactive Codex conversation to the site component registry so a user can prompt for a Content component, approve bounded actions, retain follow-ups, and automatically rebuild the canonical registry.

**Architecture:** The Go server supervises one local `codex app-server` over JSONL stdio and exposes only normalized CouchFusion APIs and SSE events. A conversation manager binds separate Codex threads to explicitly registered sites, persists minimal association metadata in a dedicated CouchDB database, applies turn-scoped approvals, records a dirty-work baseline, and runs the existing registry generator after successful changes. The Nuxt UI renders an integrated composer and active conversation above the existing component ledger.

**Tech Stack:** Go 1.24, Go standard-library JSON/HTTP/process primitives, CouchDB HTTP API, Codex app-server JSON-RPC, Nuxt 4, Vue 3, TypeScript, Vitest/Vue Test Utils, Playwright, Bun.

## Global Constraints

- Use the locally installed Codex session; CouchFusion must never collect or persist an API key, ChatGPT token, or Codex credential.
- The browser may send only CouchFusion IDs, prompt text, idempotency keys, and answers to a currently pending interaction. It may not select cwd, writable roots, executable, model, skill path, sandbox, approval policy, environment, or command.
- Resolve and revalidate every site through the explicit workspace registry. The canonical site application directory is the only writable root.
- Start network access disabled. Network access requires a typed, explicit consent for the active turn.
- Allow pre-existing uncommitted changes. Store relative path/status/hash metadata only; never store source contents or automatically revert work.
- Never stage, commit, push, branch, reset, checkout, clean, or rewrite Git history in generated sites.
- Protect package manifests and lockfiles. Dependency edits require a separate explicit consent in the active turn and cannot be covered by a generic generation approval.
- **Approve for this generation** is scoped to the active turn only and must expire before any retained-thread follow-up, interruption, process restart, or CouchFusion restart.
- Each new component uses a separate Codex thread. Follow-up prompts retain that thread.
- Persist conversation association and summary metadata in the dedicated `couchfusion-control` CouchDB database; Codex remains the transcript authority.
- Store no prompt bodies, transcript copies, diffs, raw command output, credentials, raw app-server request IDs, or pending approval tokens in CouchDB.
- On successful file changes, rebuild the canonical registry automatically. Leave component preview manual.
- Codex modifies files only; review and commit remain the user's responsibility.
- Use no new Go or web runtime dependency unless an implementation task explicitly demonstrates it is unavoidable and the user approves it.
- Keep existing CLI, workspace, task, registry, preview-host, authentication, and origin-protection behavior compatible.
- Readiness must distinguish a missing launcher from a launcher whose packaged native binary cannot start and return actionable, safe diagnostics for either condition.

## Reference Contract

Use the official [Codex app-server README](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md) as the protocol authority. At implementation time, install/repair Codex locally, then generate the exact schema for that installed version:

The development environment was verified with `codex-cli 0.144.4`. Because questions and granular permissions use experimental fields, generate the matching bundle with:

```bash
codex app-server generate-json-schema --experimental --out internal/agent/protocol/schema
```

The checked-in compatibility fixture and minimal Go protocol types must match that generated output. Do not expose generated protocol structures through the browser API.

## File Structure

### Shared mutation ownership

- `internal/mutationlease/manager.go`: workspace-scoped lease with typed conflict metadata.
- `internal/mutationlease/manager_test.go`: acquisition, release, and race coverage.
- `internal/jobs/manager.go`: replace its private workspace lock map with the shared lease.
- `internal/jobs/manager_test.go`: preserve job conflicts and prove cross-manager conflict.

### App-server gateway

- `internal/agent/protocol/types.go`: minimal request/response, notification, thread, turn, item, approval, question, and account types.
- `internal/agent/protocol/client.go`: concurrent JSONL JSON-RPC client, request correlation, notification stream, and server-request responses.
- `internal/agent/protocol/client_test.go`: fake-pipe handshake, correlation, malformed input, and shutdown tests.
- `internal/agent/protocol/schema/`: version-matched JSON Schema compatibility fixture.
- `internal/agent/appserver/supervisor.go`: binary probing, process lifecycle, handshake, skill registration, account readiness, and restart.
- `internal/agent/appserver/supervisor_test.go`: fake child process and readiness/restart tests.
- `internal/agent/appserver/testdata/fake-server.go`: deterministic JSONL app-server used by Go and browser tests.

### Conversation state and persistence

- `internal/agent/model/model.go`: normalized public conversation, turn, item, interaction, readiness, and event types.
- `internal/agent/store/couchdb.go`: lazy `couchfusion-control` database creation and optimistic document CRUD.
- `internal/agent/store/couchdb_test.go`: HTTP fixture tests and persisted-field privacy assertions.
- `internal/agent/baseline/git.go`: Git/file baseline capture and path/hash comparison.
- `internal/agent/baseline/git_test.go`: dirty tracked/untracked attribution tests.
- `internal/agent/prompt/skill/couchfusion-component-builder/SKILL.md`: embedded first-party component-generation workflow.
- `internal/agent/prompt/prompt.go`: materialize the embedded skill and build fixed developer/user inputs.
- `internal/agent/prompt/prompt_test.go`: permissions, deterministic paths, and prompt separation tests.
- `internal/agent/conversations/manager.go`: conversation lifecycle, thread binding, turns, recovery, events, and completion.
- `internal/agent/conversations/interactions.go`: question/approval classification and turn-scoped consent/grants.
- `internal/agent/conversations/manager_test.go`: state-machine and fake-app-server tests.
- `internal/agent/conversations/interactions_test.go`: approval expiry and dependency/network guards.

### Application and HTTP boundaries

- `internal/app/types.go`: agent dependency interfaces, health, and request/response boundaries.
- `internal/app/service.go`: registered-site resolution, agent methods, registry completion adapter, and shutdown.
- `internal/app/service_test.go`: site binding, lease, registry completion, and error mapping tests.
- `internal/web/workspaces.go`: conversation REST handlers and route registration.
- `internal/web/activity.go`: normalized conversation SSE endpoint.
- `internal/web/api_test.go`: validation, authorization envelope, idempotency, and SSE tests.
- `main.go`: construct one shared lease, CouchDB agent store, supervisor, and conversation manager for UI runtime.
- `main_test.go`: UI construction and graceful shutdown regression coverage.

### Nuxt UI

- `web/types/api.ts`: normalized agent DTOs.
- `web/composables/useAgentConversation.ts`: conversation snapshot, SSE reconnect, and typed mutation actions.
- `web/tests/useAgentConversation.test.ts`: reconnect, stale-event, and mutation tests.
- `web/components/sites/ComponentGenerator.vue`: first registry entry, retained list, active transcript, and follow-up composer.
- `web/components/sites/AgentInteractionCard.vue`: questions, dependency/network consent, and approval decisions.
- `web/components/sites/AgentChangedFiles.vue`: baseline/new-change grouping.
- `web/components/sites/ComponentRegistryPanel.vue`: generator slot/props and generated-component highlight.
- `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`: load conversations, own active conversation, and refresh registry.
- `web/components/system/AgentHealthCard.vue`: Codex/app-server/account/control-database/skill readiness.
- `web/pages/system.vue`: add agent readiness without offering automatic Codex installation or login.
- `web/tests/component-generator.test.ts`: focused component/interaction tests.
- `web/tests/site-detail.test.ts`: page integration and registry-refresh regressions.
- `web/tests/system.test.ts`: readiness and repair-guidance states.
- `web/e2e/agent-component-generation.spec.ts`: deterministic fake-server browser acceptance.
- `web/playwright.agent.config.ts`: isolated agent E2E configuration.
- `docs/ui-server.md`: user workflow, readiness repair, safety, and restart behavior.

---

### Task 1: Share Workspace Mutation Leases

**Files:**
- Create: `internal/mutationlease/manager.go`
- Create: `internal/mutationlease/manager_test.go`
- Modify: `internal/jobs/manager.go`
- Modify: `internal/jobs/manager_test.go`

**Interfaces:**
- Produces: `mutationlease.NewManager() *Manager`.
- Produces: `(*Manager).Acquire(scope, ownerID, ownerType string) (release func(), err error)`.
- Produces: `mutationlease.ConflictError{Scope, ActiveOwnerID, ActiveOwnerType}`.
- Changes: `jobs.Options.Leases *mutationlease.Manager`; jobs acquire the workspace ID with owner type `job`.

- [ ] **Step 1: Write failing lease tests**

```go
func TestManagerSerializesOwnersAndReleaseIsIdempotent(t *testing.T) {
    leases := NewManager()
    release, err := leases.Acquire("workspace-1", "job-1", "job")
    if err != nil { t.Fatal(err) }
    _, err = leases.Acquire("workspace-1", "conversation-1", "agent")
    var conflict ConflictError
    if !errors.As(err, &conflict) || conflict.ActiveOwnerID != "job-1" || conflict.ActiveOwnerType != "job" {
        t.Fatalf("conflict = %#v", err)
    }
    release()
    release()
    if _, err := leases.Acquire("workspace-1", "conversation-1", "agent"); err != nil { t.Fatal(err) }
}
```

Add a jobs test that constructs `leases := mutationlease.NewManager()`, starts a blocking job, and verifies a direct agent acquisition for the same workspace returns the job owner metadata.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/mutationlease ./internal/jobs -run 'Lease|WorkspaceConflict' -count=1`

Expected: FAIL because `internal/mutationlease` and `jobs.Options.Leases` do not exist.

- [ ] **Step 3: Implement the shared lease and adapt jobs**

Implement `Manager` with a mutex and `map[string]Owner`. Empty scope means no lease. `Acquire` rejects blank owner ID/type, returns a `sync.Once`-guarded release closure, and deletes only when the stored owner still matches. In `jobs.NewManager`, create a lease manager only when none was injected. In `Start`, allocate the job ID first, acquire `request.WorkspaceID`, store the release closure on the job, and call it from every terminal path. Remove `workspaceLocks`.

- [ ] **Step 4: Run job and race coverage**

Run: `go test -race ./internal/mutationlease ./internal/jobs -count=1`

Expected: PASS with existing `jobs.ConflictError` behavior mapped from `mutationlease.ConflictError` or replaced consistently in `app.MapError`.

- [ ] **Step 5: Commit the shared lease**

```bash
git add internal/mutationlease internal/jobs/manager.go internal/jobs/manager_test.go internal/app/types.go
git commit -m "refactor: share workspace mutation leases"
```

### Task 2: Check In the Versioned Protocol Contract and Implement JSONL JSON-RPC

**Files:**
- Create: `internal/agent/protocol/types.go`
- Create: `internal/agent/protocol/client.go`
- Create: `internal/agent/protocol/client_test.go`
- Create: `internal/agent/protocol/schema/` from the installed experimental schema bundle.
- Create: `internal/agent/protocol/schema/VERSION`
- Create: `internal/agent/protocol/schema/README.md`

**Interfaces:**
- Produces: `protocol.NewClient(input io.WriteCloser, output io.ReadCloser, options Options) *Client`.
- Produces: `(*Client).Call(ctx context.Context, method string, params any, result any) error`.
- Produces: `(*Client).Notify(ctx context.Context, method string, params any) error`.
- Produces: `(*Client).Notifications() <-chan Notification` and `(*Client).ServerRequests() <-chan ServerRequest`.
- Produces: `(*Client).Respond(ctx context.Context, id json.RawMessage, result any, rpcErr *RPCError) error` and `Close() error`.

- [ ] **Step 1: Generate the reviewed schema fixture**

Run:

```bash
mkdir -p internal/agent/protocol/schema
codex --version > internal/agent/protocol/schema/VERSION
codex app-server generate-json-schema --experimental --out internal/agent/protocol/schema
```

Inspect the generated request/notification definitions and record the required method names in the test table: initialize/initialized, account/read, skills/extraRoots/set, skills/list, thread/start/resume/read, turn/start/interrupt, item and turn notifications, command/file/permission approval requests, user-input requests, and serverRequest/resolved.

- [ ] **Step 2: Write failing pipe-based protocol tests**

Create paired `io.Pipe` streams. Verify two concurrent calls receive out-of-order responses by ID, a notification reaches `Notifications`, a server request reaches `ServerRequests`, `Respond` writes the original raw ID, an RPC error becomes typed `*protocol.RPCError`, malformed JSON closes the client with `ErrProtocol`, and cancellation removes the pending call.

The core assertion should use messages without a wire-level `jsonrpc` field, matching app-server:

```go
go func() {
    scanner := bufio.NewScanner(serverInput)
    scanner.Scan(); first := decodeRequest(t, scanner.Bytes())
    scanner.Scan(); second := decodeRequest(t, scanner.Bytes())
    fmt.Fprintf(serverOutput, `{"id":%s,"result":{"value":"second"}}`+"\n", second.ID)
    fmt.Fprintf(serverOutput, `{"id":%s,"result":{"value":"first"}}`+"\n", first.ID)
}()
```

- [ ] **Step 3: Run and verify missing package failure**

Run: `go test ./internal/agent/protocol -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 4: Implement minimal protocol types and client**

Use `json.RawMessage` envelopes so unknown future item fields remain representable. Serialize writes under one mutex. Maintain `map[uint64]chan response`, allocate monotonically increasing IDs, use a scanner capped at 2 MiB per JSONL message, and dispatch by envelope shape: response has `id` plus `result/error`, server request has `id` plus `method`, notification has `method` without `id`. Bound notification/request channels and fail closed with `ErrOverloaded` rather than blocking the reader forever.

Define typed params/results only for `initialize`, `account/read`, `skills/extraRoots/set`, `skills/list`, `thread/start`, `thread/resume`, `thread/read`, `turn/start`, `turn/interrupt`, command/file/permission approvals, user input, `serverRequest/resolved`, and item/turn notifications. Preserve unknown items as `json.RawMessage`.

- [ ] **Step 5: Run protocol tests and static checks**

Run: `go test -race ./internal/agent/protocol -count=1`

Run: `go vet ./internal/agent/protocol`

Expected: PASS.

- [ ] **Step 6: Add the schema refresh contract and commit**

Document in `schema/README.md` the exact `generate-json-schema --experimental --out internal/agent/protocol/schema` command, installed Codex version recording, reviewed-file policy, and requirement that contract fixtures pass before schema updates are committed.

```bash
git add internal/agent/protocol
git commit -m "feat: add Codex app-server protocol client"
```

### Task 3: Supervise App-server and Report Readiness

**Files:**
- Create: `internal/agent/appserver/supervisor.go`
- Create: `internal/agent/appserver/supervisor_test.go`
- Create: `internal/agent/appserver/testdata/fake-server.go`
- Create: `internal/agent/model/model.go`

**Interfaces:**
- Produces: `appserver.NewSupervisor(Options) *Supervisor`.
- Produces: `EnsureReady(ctx) (model.AgentReadiness, error)`, `Client(ctx) (*protocol.Client, error)`, `Events() <-chan protocol.Notification`, `Requests() <-chan protocol.ServerRequest`, and `Shutdown(ctx) error`.
- Produces: readiness states `ready`, `missing`, `launch_failed`, `unsupported`, `unauthenticated`, and `restarting`.

- [ ] **Step 1: Write failing supervisor tests around a fake executable**

Compile `testdata/fake-server.go` into `t.TempDir()`. Make it implement initialize, account/read, skills/extraRoots/set, and controlled exit. Assert the supervisor sends `clientInfo.name = "couchfusion"`, enables the experimental API needed for questions/permissions, registers exactly the managed skill root, returns the account/model as safe readiness metadata, and restarts after an unexpected exit. Add a launcher that returns `exec.ErrNotFound` and one that returns a missing packaged-native-binary error; assert both become safe readiness problems without exposing environment values. Add version probes for supported `0.144.x` and rejected `0.143.x`/`0.145.x` values.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/agent/appserver -count=1`

Expected: FAIL because the supervisor is missing.

- [ ] **Step 3: Implement lazy lifecycle and handshake**

`Options` must inject `Command func(context.Context) *exec.Cmd`, `VersionCommand func(context.Context) *exec.Cmd`, `SkillRoot string`, `Now`, and backoff for tests. `EnsureReady` accepts the schema-tested `0.144.x` minor line, starts one child, attaches the protocol client, sends `initialize` then `initialized`, registers the extra root, checks `account/read`, and returns a sanitized view. Other minor lines are `unsupported` until their schemas and contract fixtures are reviewed. Capture stderr into a bounded 64 KiB ring used only for diagnostics. Unexpected exit clears all pending protocol state, emits restart state, and retries with bounded exponential backoff (250 ms to 5 s). A caller waiting on readiness observes context cancellation.

- [ ] **Step 4: Run supervisor race and leak tests**

Run: `go test -race ./internal/agent/appserver -count=1`

Expected: PASS; every test calls `Shutdown`, and the fake child exits.

- [ ] **Step 5: Commit supervisor foundation**

```bash
git add internal/agent/appserver internal/agent/model
git commit -m "feat: supervise Codex app-server"
```

### Task 4: Persist Minimal Conversation Metadata in CouchDB

**Files:**
- Create: `internal/agent/store/couchdb.go`
- Create: `internal/agent/store/couchdb_test.go`

**Interfaces:**
- Produces: `store.Repository` with `Ensure`, `Create`, `Get`, `ListSite`, `Update`, and `MarkActiveInterrupted`.
- Produces: `store.NewCouchDBRepository(CouchDBOptions) *CouchDBRepository` using database `couchfusion-control`.
- Consumes: existing `couchdb.Store` credentials and an injected `http.Client`.

- [ ] **Step 1: Write failing HTTP-fixture tests**

Use `httptest.Server` to assert `Ensure` performs authenticated `PUT /couchfusion-control`, treats 201/202/412 as success, and never logs Authorization. Assert `Create` writes `_id = agent-conversation:<id>`, `Update` supplies the current `_rev`, conflict 409 returns `ErrConflict`, and `ListSite` filters validated documents returned by `_all_docs?include_docs=true`.

Marshal a document populated with sentinel values and assert the JSON excludes `prompt`, `transcript`, `diff`, `output`, `requestId`, `approvalToken`, `password`, and `authorization`.

- [ ] **Step 2: Run and verify missing package failure**

Run: `go test ./internal/agent/store -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement lazy database creation and optimistic CRUD**

Define `Document` with the exact design fields: ID/revision/type/schema version, conversation/Codex thread/workspace/site IDs, site-root fingerprint, title/status, generated component ID, baseline entries, changed files, bounded last-turn summary, and timestamps. Validate `type == "agent-conversation"` and `schemaVersion == 1`; newer schemas return `ErrNewerSchema` and are readable only as minimal summaries. Use Basic auth decoded from the existing credential store only in request headers. Cap response bodies at 2 MiB.

- [ ] **Step 4: Run store tests and credential regressions**

Run: `go test -race ./internal/agent/store ./internal/couchdb -count=1`

Expected: PASS.

- [ ] **Step 5: Commit CouchDB persistence**

```bash
git add internal/agent/store
git commit -m "feat: persist agent conversation metadata"
```

### Task 5: Capture and Compare the Dirty-work Baseline

**Files:**
- Create: `internal/agent/baseline/git.go`
- Create: `internal/agent/baseline/git_test.go`

**Interfaces:**
- Produces: `baseline.Capture(ctx, root) (Snapshot, error)`.
- Produces: `baseline.Compare(ctx, root, Snapshot) ([]model.ChangedFile, error)`.
- Produces: `baseline.Fingerprint(workspaceID, siteID, appsRoot, siteRoot string) string`.

- [ ] **Step 1: Write failing repository-fixture tests**

Initialize a temporary Git repository, commit `tracked.vue`, then modify it and add `untracked.vue`. Capture the baseline, change both again, add `new.vue`, and delete another tracked file. Assert comparison returns only relative slash-normalized paths, classifies `added|modified|deleted`, and marks whether each path was already dirty at capture time. Add a symlink/path escape fixture and assert it is rejected from UI output.

```go
snapshot, err := Capture(ctx, root)
if err != nil { t.Fatal(err) }
changes, err := Compare(ctx, root, snapshot)
if err != nil { t.Fatal(err) }
assertChange(t, changes, "tracked.vue", model.ChangeModified, true)
assertChange(t, changes, "new.vue", model.ChangeAdded, false)
```

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/agent/baseline -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement status parsing and hashing**

Run trusted commands only through `exec.CommandContext`: `git -C <root> status --porcelain=v2 -z --untracked-files=all` and `git -C <root> ls-files -z --cached --others --exclude-standard`. Parse NUL records without shell interpolation. Hash regular file bytes with SHA-256; represent missing files with an empty hash and explicit status. Reject absolute/display paths, `..`, NULs, and resolved paths outside root. Do not persist file content or Git diffs. `Fingerprint` hashes canonical IDs and paths separated by NUL bytes.

- [ ] **Step 4: Run baseline and race tests**

Run: `go test -race ./internal/agent/baseline -count=1`

Expected: PASS.

- [ ] **Step 5: Commit baseline attribution**

```bash
git add internal/agent/baseline
git commit -m "feat: track agent file baselines"
```

### Task 6: Embed the Content Component Skill and Prompt Policy

**Files:**
- Create: `internal/agent/prompt/skill/couchfusion-component-builder/SKILL.md`
- Create: `internal/agent/prompt/prompt.go`
- Create: `internal/agent/prompt/prompt_test.go`

**Interfaces:**
- Produces: `prompt.MaterializeSkill(stateRoot string) (Skill, error)` with `Skill{Name, Root, Path, Digest}`.
- Produces: `prompt.InitialTurn(userPrompt string, skill Skill) protocol.TurnInput`.
- Produces: `prompt.FollowUp(userPrompt string) protocol.TurnInput`.
- Produces: `prompt.DeveloperInstructions() string`.

- [ ] **Step 1: Write failing prompt and permission tests**

Assert materialization creates `<stateRoot>/skills/<digest>/couchfusion-component-builder/SKILL.md`, directory mode `0700`, file mode `0600`, and identical input is idempotent. Assert the initial turn contains a text item beginning with `$couchfusion-component-builder`, a distinct `{type:"skill", name, path}` item, and the exact unmodified user prompt. Assert developer instructions contain the app-only, dirty-work, no-Git-history, dependency-consent, network-consent, validation, and canonical-registry rules. Assert follow-ups do not repeat or concatenate the developer policy into user text.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/agent/prompt -count=1`

Expected: FAIL because the package and embedded skill are missing.

- [ ] **Step 3: Write the first-party skill and builder**

The skill must direct Codex to inspect applicable `AGENTS.md`, existing `app/components/content` patterns, builder metadata, shared styles, and canonical registry generation. It must require focused validation, forbid hand-maintained duplicate registry JSON, and stop for explicit consent before package/lock changes.

Use `//go:embed skill/couchfusion-component-builder/SKILL.md`. Materialize by digest through a temp file plus atomic rename, reject symlinked destinations, and never write under the selected app. Keep the user prompt in a separate input item and cap it at 16 KiB after trimming; blank input returns `ErrInvalidPrompt`.

- [ ] **Step 4: Run prompt tests**

Run: `go test -race ./internal/agent/prompt -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the embedded skill**

```bash
git add internal/agent/prompt
git commit -m "feat: embed component generation skill"
```

### Task 7: Build the Conversation State Machine and Event Stream

**Files:**
- Create: `internal/agent/conversations/manager.go`
- Create: `internal/agent/conversations/manager_test.go`
- Modify: `internal/agent/model/model.go`

**Interfaces:**
- Produces: `conversations.NewManager(Options) *Manager`.
- Produces: `Create(ctx, CreateRequest) (model.Conversation, error)`, `ListSite`, `Get`, `StartTurn`, `Interrupt`, `Subscribe`, `Recover`, and `Shutdown`.
- Consumes: repository, baseline service, shared lease, app-server supervisor/client, prompt builder, and a completion hook.
- Produces: monotonically sequenced `model.Event` values and complete safe snapshots.

- [ ] **Step 1: Write failing state-machine tests with fakes**

Test these exact transitions:

```text
new -> active -> completed -> rebuilding_registry -> ready
new -> active -> waiting -> active
active -> interrupting -> interrupted -> ready
active -> failed -> ready
```

Assert one active turn per conversation, one mutation lease per workspace, separate Create calls use separate thread IDs, StartTurn retains the thread, sequence numbers strictly increase, subscriber replay honors `after`, and overflow replaces replay with `conversation.snapshot`. Assert `Recover` loads documents, marks persisted active/waiting states interrupted, revalidates the site fingerprint, and calls `thread/read` without replaying pending interactions.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/agent/conversations -run 'Lifecycle|Recovery|Events' -count=1`

Expected: FAIL because the manager is missing.

- [ ] **Step 3: Implement lifecycle, ownership, and bounded events**

`CreateRequest` contains only server-resolved `WorkspaceID`, `SiteID`, `AppsRoot`, `SiteRoot`, `Prompt`, and `IdempotencyKey`. Generate an opaque 128-bit CouchFusion ID, capture the baseline, then call `thread/start` with `cwd = SiteRoot`, no model override, `approvalPolicy = "on-request"`, `approvalsReviewer = "user"`, and a workspace-write sandbox whose writable root is exactly `SiteRoot` and whose network access is false. Supply fixed developer instructions through the typed settings field and the embedded skill/user input as distinct items. Persist the returned Codex thread ID immediately. Follow-ups call `turn/start` on the retained thread with the same explicit cwd/sandbox/approval settings so local sticky settings cannot broaden the boundary. Keep at most 256 events/1 MiB normalized text per conversation in memory. Use a per-conversation mutex plus the shared workspace lease; never hold either while blocking on HTTP/SSE writes. Store idempotency results by session/site/key for 10 minutes in bounded memory.

Normalize only user messages, agent messages, reasoning summaries, command/file item summaries, turn state, and interactions. Unknown item types become a generic informational item with type and status, not raw JSON.

Structured logs include only CouchFusion conversation/turn IDs, lifecycle code, and safe status. They must exclude prompt/message bodies, transcript text, absolute site paths, source content, diffs, raw output, RPC IDs, authorization headers, and CouchDB credentials.

- [ ] **Step 4: Run lifecycle and race tests**

Run: `go test -race ./internal/agent/conversations -run 'Lifecycle|Recovery|Events|Concurrent' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit conversation lifecycle**

```bash
git add internal/agent/conversations internal/agent/model/model.go
git commit -m "feat: manage persistent agent conversations"
```

### Task 8: Add Questions, Approvals, and Protected Consents

**Files:**
- Create: `internal/agent/conversations/interactions.go`
- Create: `internal/agent/conversations/interactions_test.go`
- Modify: `internal/agent/conversations/manager.go`
- Modify: `internal/agent/conversations/manager_test.go`

**Interfaces:**
- Produces: `AnswerQuestion`, `ResolveApproval`, and `GrantConsent` manager methods.
- Produces: decisions `approve_once`, `approve_generation`, `decline`, and `cancel`.
- Produces: consent kinds `dependency_change` and `network_access` scoped to one turn.

- [ ] **Step 1: Write failing security-boundary tests**

Cover command, file-change, permissions, and question server requests. Assert a CouchFusion request ID is newly generated and raw RPC IDs never appear in snapshots. Assert foreign conversation/turn IDs are rejected, first resolution wins, stale resolutions conflict, and app-server `serverRequest/resolved` clears UI state.

For generation approval, approve a matching command twice in turn A, complete turn A, start follow-up turn B, and assert the same command asks again. Repeat with interrupt, app-server restart, and CouchFusion recovery. Assert package files (`package.json`, `bun.lock`, `bun.lockb`, `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`) and network requests remain blocked until their distinct consent is recorded, even after `approve_generation`.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/agent/conversations -run 'Approval|Consent|Question' -count=1`

Expected: FAIL because interaction resolution is missing.

- [ ] **Step 3: Implement sanitized interaction classification**

Store pending raw RPC IDs only in memory. Derive narrow generation grant keys from normalized action type plus command executable/argument shape or relative path class; never use untrusted reason text as a grant key. Map `approve_once` to `accept`. Implement `approve_generation` by accepting the current request once and retaining only the narrow key for the active turn; do not use `acceptForSession` unless a contract test proves that installed version limits it to the current turn. Clear grants/consents on every terminal transition, process generation change, and recovery.

Questions accept only currently declared IDs/options, at most three answers, and 4 KiB total text. `cancel` responds with cancel where supported and immediately requests `turn/interrupt`.

- [ ] **Step 4: Run focused and race tests**

Run: `go test -race ./internal/agent/conversations -run 'Approval|Consent|Question|Restart|FollowUp' -count=1`

Expected: PASS with no approval leakage under `-race`.

- [ ] **Step 5: Commit interactive controls**

```bash
git add internal/agent/conversations
git commit -m "feat: add scoped agent approvals"
```

### Task 9: Complete Turns with Changed Files and Registry Rebuild

**Files:**
- Modify: `internal/agent/conversations/manager.go`
- Modify: `internal/agent/conversations/manager_test.go`
- Modify: `internal/agent/model/model.go`

**Interfaces:**
- Adds: `CompletionHook.Complete(ctx, CompletionRequest) model.RegistryResult`.
- Adds: `CompletionRequest{WorkspaceID, SiteID, SiteRoot, ChangedFiles}`.
- Produces: registry states `not_needed`, `running`, `succeeded`, and `failed` independently from Codex turn status.

- [ ] **Step 1: Write failing completion tests**

Drive `turn/completed` through the fake client. Assert baseline comparison runs once, no changed registry inputs yields `not_needed`, changed `.vue`/registry-source files hold the lease and invoke the hook, success emits `registry.started` then `registry.completed`, and failure emits `registry.failed` while preserving a ready conversation and changed-file list. Assert no generated file is deleted on failure.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/agent/conversations -run 'RegistryCompletion|ChangedFiles' -count=1`

Expected: FAIL because completion hook orchestration is missing.

- [ ] **Step 3: Implement completion ordering**

On successful `turn/completed`, compare against the original conversation baseline, update the persisted changed-file summary, and classify registry input paths. Keep the workspace lease until the synchronous completion hook ends, but clear the active Codex turn and all grants before starting the hook. Persist `ready` plus the registry result. If no files changed, skip the hook. A hook error is a registry warning, not a failed Codex turn.

- [ ] **Step 4: Run conversation package verification**

Run: `go test -race ./internal/agent/conversations ./internal/agent/baseline -count=1`

Expected: PASS.

- [ ] **Step 5: Commit completion behavior**

```bash
git add internal/agent/conversations internal/agent/model/model.go
git commit -m "feat: rebuild registry after agent turns"
```

### Task 10: Wire the Agent Boundary into the Application Service

**Files:**
- Modify: `internal/app/types.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/service_test.go`
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Adds `AgentConversations` dependency exposing readiness/list/get/create/turn/answer/approve/consent/interrupt/subscribe/shutdown.
- Adds application methods with server-resolved workspace/site IDs and browser-safe agent model types.
- Extends `Health` with `Agent model.AgentReadiness`.

- [ ] **Step 1: Write failing service-boundary tests**

Use the existing registry/project fakes and a new conversation fake. Assert Create resolves the registered `project.Site`, derives the canonical apps root from the registered workspace, and passes trusted roots rather than any request path. Assert a moved/outside site fails before the agent fake is called. Assert registry completion resolves only curated `script:registry:build`, runs it directly through `CommandRunner` while the agent owns the shared lease, and returns a bounded result. Assert `Shutdown` stops agent conversations before jobs/processes.

- [ ] **Step 2: Run and verify compile failure**

Run: `go test ./internal/app -run 'Agent|Conversation|RegistryCompletion' -count=1`

Expected: FAIL because the application boundary lacks agent methods.

- [ ] **Step 3: Implement service adapters and error mapping**

Add request types containing only prompt/idempotency/decision/answers/consent. Extend `MapError` for agent not-found, conflict, unavailable, invalid interaction, and changed binding without exposing causes. Add a private `agentSiteBinding` that calls `workspaceEntry`, `Projects.Site`, `filepath.EvalSymlinks`, and `filepath.Rel` to prove the site is under `<workspace>/apps`. The service computes the fingerprint; it never accepts one from HTTP.

Implement the completion hook with `Tasks.Resolve(site, "script:registry:build")` and `Commands.Run`, using a bounded reporter that retains status but not raw command output in the persisted result.

When the agent dependency is unavailable, `Health.Agent` and the conversation collection return capability/readiness data while ordinary service methods remain usable. Do not make UI server startup depend on Codex authentication or control-database availability.

- [ ] **Step 4: Construct dependencies once in the UI runtime**

In `main.go`, create one `mutationlease.Manager`; inject it into `jobs.NewManager` and `conversations.NewManager`. Load the existing CouchDB credential store, create the agent repository, materialize the skill below `~/.couchfusion/agent`, construct the supervisor with `exec.CommandContext(ctx, "codex", "app-server", "--stdio")`, and inject all of them into `app.NewService`. CLI-only service construction may retain an unavailable/no-op agent dependency because the feature is UI-only. Ensure runtime shutdown joins app-server cleanup.

- [ ] **Step 5: Run application and main tests**

Run: `go test -race ./internal/app ./internal/jobs ./... -run 'Agent|Conversation|RegistryCompletion|Shutdown|WorkspaceConflict' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit application wiring**

```bash
git add internal/app main.go main_test.go
git commit -m "feat: wire agent conversations into application"
```

### Task 11: Expose Conversation REST and SSE APIs

**Files:**
- Modify: `internal/web/workspaces.go`
- Modify: `internal/web/activity.go`
- Modify: `internal/web/api_test.go`

**Interfaces:**
- Produces the approved conversation collection/detail/turn/approval/question/consent/interrupt/event routes.
- Uses `Idempotency-Key` on create and follow-up turn requests.
- Streams SSE event name `conversation` with normalized `model.Event` data.
- Returns collection shape `{readiness, conversations}` so the site page can disable only generation while retaining the registry.

- [ ] **Step 1: Extend the API application stub and write failing route tests**

Test:

```text
GET  /api/v1/workspaces/w1/sites/shop/agent-conversations
POST /api/v1/workspaces/w1/sites/shop/agent-conversations
GET  /api/v1/agent-conversations/c1
POST /api/v1/agent-conversations/c1/turns
POST /api/v1/agent-conversations/c1/approvals/r1
POST /api/v1/agent-conversations/c1/questions/r1/answer
POST /api/v1/agent-conversations/c1/consents
POST /api/v1/agent-conversations/c1/interrupt
GET  /api/v1/agent-conversations/c1/events
```

Assert unknown JSON fields return 422; blank/over-16-KiB prompts return validation; invalid decision/consent enums return validation; IDs are path-decoded through the existing safe helper; SSE honors `Last-Event-ID`; and no response contains `codexThreadId`, cwd, absolute skill path, raw RPC ID, authorization, prompt persistence, or credential values.

- [ ] **Step 2: Run and verify failure**

Run: `go test ./internal/web -run 'AgentConversation|AgentSSE' -count=1`

Expected: FAIL because routes and interface methods are missing.

- [ ] **Step 3: Implement thin handlers and SSE**

Add methods to `Application`, register exact Go 1.24 ServeMux patterns, use `decodeRequest`/`decodeEmptyRequest`, and pass only typed request fields. Require a 1–128 character `Idempotency-Key` for create/turn. Approval body is `{decision}`; question body is `{answers: Record<string,string>}`; consent body is `{kind, granted}`. `conversationEvents` reuses `lastEventID` and `serveSSE` with event name `conversation`.

- [ ] **Step 4: Run full web tests**

Run: `go test -race ./internal/web -count=1`

Expected: PASS, including existing authentication, same-origin, body-limit, and CSP tests.

- [ ] **Step 5: Commit the API boundary**

```bash
git add internal/web
git commit -m "feat: expose agent conversation API"
```

### Task 12: Add Typed Web State and SSE Reconnection

**Files:**
- Modify: `web/types/api.ts`
- Create: `web/composables/useAgentConversation.ts`
- Create: `web/tests/useAgentConversation.test.ts`

**Interfaces:**
- Produces: normalized TypeScript types matching the Go DTO exactly.
- Produces: `createAgentConversationClient({api, createEventSource, schedule})`.
- Produces: `useAgentConversation()` with `open`, `close`, `create`, `followUp`, `answer`, `approve`, `consent`, and `interrupt`.

- [ ] **Step 1: Write failing composable tests**

Use the same fake `EventSourceLike` pattern as `useActivity`. Assert opening fetches a snapshot then connects to `/api/v1/agent-conversations/c1/events`, events with increasing sequence trigger snapshot refresh, duplicates/stale sequences are ignored, an error fetches a fresh snapshot then reconnects with `?after=<sequence>`, and close cancels reconnect. Assert create/followUp add a unique idempotency key header through a new optional `headers` field in `FetchOptions`.

- [ ] **Step 2: Run and verify failure**

Run: `cd web && bun run test:run tests/useAgentConversation.test.ts`

Expected: FAIL because types/composable/header support are missing.

- [ ] **Step 3: Implement normalized types and client**

Add exact unions for conversation status, item type/status, interaction kind/decision, consent kind, registry result, changed-file kind, readiness status, and event type. Keep absolute paths and Codex thread IDs absent. Extend `useApi.request` with optional same-origin headers while preserving existing calls. Generate idempotency keys with `crypto.randomUUID()` and keep them stable for a single retried action.

The SSE listener treats events as invalidation plus lightweight immediate state; it always accepts a complete `conversation.snapshot` and otherwise refreshes detail after advancing `sequence`. Use a 750 ms reconnect delay and close on component disposal.

- [ ] **Step 4: Run composable and API-client tests**

Run: `cd web && bun run test:run tests/useAgentConversation.test.ts tests/useApi.test.ts tests/useActivity.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit web state**

```bash
git add web/types/api.ts web/composables/useApi.ts web/composables/useAgentConversation.ts web/tests/useAgentConversation.test.ts web/tests/useApi.test.ts
git commit -m "feat: add agent conversation web client"
```

### Task 13: Build the Integrated Generator Conversation UI

**Files:**
- Create: `web/components/sites/ComponentGenerator.vue`
- Create: `web/components/sites/AgentInteractionCard.vue`
- Create: `web/components/sites/AgentChangedFiles.vue`
- Create: `web/tests/component-generator.test.ts`
- Modify: `web/assets/css/main.css`

**Interfaces:**
- `ComponentGenerator` accepts readiness, conversation list, active snapshot, and busy/error state; emits create/open/follow-up/answer/approve/consent/interrupt.
- `AgentInteractionCard` accepts one pending interaction and emits only decisions allowed by its `availableDecisions`.
- `AgentChangedFiles` accepts baseline and changed paths and renders separate groups.

- [ ] **Step 1: Write failing component tests for every user-visible state**

Mount idle readiness and assert the first control is a naturally sized prompt textarea plus Generate. Mount unavailable states for missing, launch-failed, unsupported, unauthenticated, CouchDB unavailable, and changed binding; assert specific guidance and no disabled unrelated registry controls. Mount active messages, compact streamed progress, waiting question form, command/file approval cards, dependency/network consent, interrupt, registry running/warning, generated component link, collapsed retained conversations, and follow-up composer.

Verify approval buttons emit exactly:

```ts
expect(wrapper.emitted('approve')?.map(([decision]) => decision)).toEqual(
  expect.arrayContaining(['approve_once', 'approve_generation', 'decline', 'cancel']),
)
```

Assert dependency/network cards never render `approve_generation` before typed consent.

- [ ] **Step 2: Run and verify failure**

Run: `cd web && bun run test:run tests/component-generator.test.ts`

Expected: FAIL because the components do not exist.

- [ ] **Step 3: Implement accessible compact components**

Use semantic articles, ordered transcript list, `aria-live="polite"` for status, `role="alert"` for failures, explicit labels, and native buttons/forms. Keep raw reasoning collapsed to short summaries. Cap visible command output and show truncation copy. Render relative paths with `<code>`. Disable only the action currently pending. Confirmation for dependency/network consent must name its scope: “this generation only”.

Follow the existing compact type scale: headings no larger than the registry section heading, mono labels around `.6rem`, body copy around `.8rem–.9rem`, and controls using `.compact-button`. Add responsive single-column behavior below 700 px.

- [ ] **Step 4: Run component tests and accessibility assertions**

Run: `cd web && bun run test:run tests/component-generator.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit generator components**

```bash
git add web/components/sites/ComponentGenerator.vue web/components/sites/AgentInteractionCard.vue web/components/sites/AgentChangedFiles.vue web/tests/component-generator.test.ts web/assets/css/main.css
git commit -m "feat: add component generation conversation UI"
```

### Task 14: Integrate Generation into the Registry Panel and Site Page

**Files:**
- Modify: `web/components/sites/ComponentRegistryPanel.vue`
- Modify: `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`
- Create: `web/components/system/AgentHealthCard.vue`
- Modify: `web/pages/system.vue`
- Modify: `web/tests/site-detail.test.ts`
- Create: `web/tests/system.test.ts`

**Interfaces:**
- The generator is the first registry-panel entry before registry status guidance and component rows.
- Registry refresh/highlight is driven by `registryResult.status === 'succeeded'` and `generatedComponentId`.
- Existing preview host stays explicit and unchanged.

- [ ] **Step 1: Write failing page-integration tests**

Extend the site-page API fake to load the conversation collection. Assert `ComponentGenerator` renders before `.component-ledger`, creating calls the site-scoped endpoint with an idempotency key, opening a retained conversation uses its detail/SSE client, and successful completion reloads `/components` exactly once and highlights `[data-component="hero"]`. Assert registry failure shows Retry Rebuild while preserving the changed-file conversation. Assert preview host has not started automatically.

Add a regression that unmounting the page closes conversation SSE and existing preview polling/activity subscriptions.

Mount the System page with each readiness state and assert it shows Codex binary/version, app-server, local account, `couchfusion-control`, and skill registration as read-only checks. Missing login directs the user to local Codex authentication; missing/broken Codex provides a verification command. It must not expose an Install Codex, Login, credential, or raw stderr action.

- [ ] **Step 2: Run and verify failure**

Run: `cd web && bun run test:run tests/site-detail.test.ts`

Expected: FAIL because generation is not integrated.

- [ ] **Step 3: Implement page ownership and registry highlight**

Load site/tasks/components/preview host/conversations concurrently. Keep agent errors scoped to the generator. Pass the generator as the first child of `ComponentRegistryPanel` through explicit props/events or a named slot; do not move preview/rebuild controls. Watch the active conversation's registry result transition and call `loadComponents`; after Vue renders, focus/scroll the matching row and add a temporary `component-row--generated` class removed after 4 seconds. If no component ID resolves, refresh without guessing. Add `AgentHealthCard` to System using `health.agent`; render capability details only from the safe readiness DTO.

- [ ] **Step 4: Run all web unit/type checks**

Run: `cd web && bun run test:run`

Run: `cd web && bun run typecheck`

Expected: PASS.

- [ ] **Step 5: Commit page integration**

```bash
git add web/components/sites/ComponentRegistryPanel.vue 'web/pages/workspaces/[workspaceId]/sites/[siteId].vue' web/components/system/AgentHealthCard.vue web/pages/system.vue web/tests/site-detail.test.ts web/tests/system.test.ts
git commit -m "feat: integrate generation into component registry"
```

### Task 15: Add Fake-server Browser Acceptance and Operator Documentation

**Files:**
- Modify: `internal/agent/appserver/testdata/fake-server.go`
- Create: `web/e2e/agent-component-generation.spec.ts`
- Create: `web/playwright.agent.config.ts`
- Modify: `web/package.json`
- Modify: `docs/ui-server.md`

**Interfaces:**
- Adds `bun run e2e:agent`.
- Fake server supports deterministic prompt, question, approval, file change, completion, crash, and retained-thread scenarios without contacting OpenAI.

- [ ] **Step 1: Write the failing Playwright flow**

The browser test must:

1. open a registered disposable fixture site with pre-existing tracked and untracked changes;
2. enter a component prompt and start generation;
3. answer a clarification question;
4. approve a safe file change for this generation;
5. observe a dependency request blocked until separate consent;
6. finish the turn and observe automatic registry refresh/highlight;
7. verify preview host remains stopped;
8. send a follow-up and verify the previous generation approval is not inherited;
9. restart CouchFusion/fake app-server, reopen the retained conversation, and verify it is recoverable with no pending approval replay.

- [ ] **Step 2: Run and verify failure**

Run: `cd web && bun run e2e:agent`

Expected: FAIL because the configuration/fixture flow is missing.

- [ ] **Step 3: Complete the deterministic test harness**

Add fake-server scenario selection through server-owned test environment only. It must speak real JSONL protocol envelopes and write only inside the disposable fixture. Configure Playwright to start the CouchFusion UI with the fake app-server executable and temporary CouchDB HTTP fixture. Never enable this override in production builds or browser input.

- [ ] **Step 4: Document manual use and repair**

Update `docs/ui-server.md` with:

- prerequisite `codex --version` and local Codex authentication;
- `couchfusion ui --dev` startup;
- System readiness interpretation;
- Create component, answer/approve, changed-file review, follow-up, registry retry, and manual preview steps;
- dirty-work/dependency/network/Git safety rules;
- `codex app-server --help` repair check when a launcher exists but cannot start;
- restart behavior and the `couchfusion-control` database purpose.

- [ ] **Step 5: Run complete verification**

Run: `gofmt -w internal/agent/protocol/*.go internal/agent/appserver/*.go internal/agent/model/*.go internal/agent/store/*.go internal/agent/baseline/*.go internal/agent/prompt/*.go internal/agent/conversations/*.go internal/mutationlease/*.go internal/app/*.go internal/web/*.go main.go main_test.go`

Run: `go test -race ./... -count=1`

Run: `go vet ./...`

Run: `cd web && bun run test:run && bun run typecheck && bun run build`

Run: `cd web && bun run e2e:agent`

Expected: every command PASS. The real authenticated Codex integration remains an opt-in manual check, not a required CI dependency.

- [ ] **Step 6: Commit acceptance coverage and docs**

```bash
git add internal/agent/appserver/testdata web/e2e/agent-component-generation.spec.ts web/playwright.agent.config.ts web/package.json docs/ui-server.md
git commit -m "test: cover agent component generation"
```

### Task 16: Verify Real Local App-server Compatibility

**Files:**
- Modify only if compatibility findings require corrections: `internal/agent/protocol/schema/`, `internal/agent/protocol/types.go`, and focused protocol tests.
- Create: `docs/implementation_results/2026-07-14-codex-component-generation.md`

**Interfaces:**
- Confirms the checked-in protocol contract against locally installed `codex-cli 0.144.4`.
- Records verification evidence without storing account data, prompts, transcripts, or source contents.

- [ ] **Step 1: Generate and review the installed schema**

Run:

```bash
schema_dir="$(mktemp -d)"
codex app-server generate-json-schema --experimental --out "$schema_dir"
```

Compare required method/field shapes against the checked-in compatibility fixture. Do not mechanically replace reviewed fixtures without updating protocol contract tests.

- [ ] **Step 2: Run an opt-in disposable-site smoke test**

Start CouchFusion against a temporary registered workspace, open one conversation, ask Codex to create a trivial fixture component, exercise one approval or question if requested, and confirm completion/registry rebuild. Inspect the fixture Git status to confirm no commit/branch/history operation occurred. Delete only the disposable workspace after inspection; never clean a user repository.

- [ ] **Step 3: Re-run security regressions after real-protocol corrections**

Run: `go test -race ./internal/agent/... ./internal/app ./internal/web -count=1`

Run: `cd web && bun run test:run && bun run e2e:agent`

Expected: PASS.

- [ ] **Step 4: Record implementation results**

Write the actual Codex version, schema generation command, verification commands/results, known compatibility range, CouchDB database, manual smoke-test result, and any deferred non-goals. Do not include credentials, transcript content, absolute user-site paths, or source diffs.

- [ ] **Step 5: Commit final compatibility evidence**

```bash
git add internal/agent/protocol docs/implementation_results/2026-07-14-codex-component-generation.md
git commit -m "docs: record Codex generation implementation"
```
