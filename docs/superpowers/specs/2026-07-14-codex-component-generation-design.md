# Codex-Assisted Content Component Generation Design

**Date:** 2026-07-14

## Summary

CouchFusion will add an integrated component-generation conversation to the existing build-time component registry panel. A user describes the desired Content-layer component, CouchFusion starts or resumes a dedicated Codex thread rooted at the selected Nuxt site's application directory, and Codex modifies files under that application only. Questions and approvals remain interactive inside CouchFusion. Successful generations automatically rebuild the component registry; preview remains an explicit, manual action.

The Go server owns a long-lived local `codex app-server` subprocess and speaks its JSON-RPC protocol over stdio. The browser never connects to app-server directly and cannot choose a working directory, executable, sandbox, model, skill path, or command. CouchFusion validates every selected workspace and site against its explicit registry before starting a conversation.

The design follows the official [Codex app-server protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md), including initialization, threads, turns, streamed notifications, server-initiated approvals, and user-input requests. CouchFusion uses the locally installed Codex authentication and configuration rather than collecting API credentials.

## Goals

- Create Content-layer-compatible Vue components from a natural-language prompt within a registered site's app directory.
- Reuse the locally installed Codex session and keep each generation available for follow-up turns.
- Surface streamed progress, clarification questions, command/file approvals, and results in CouchFusion.
- Offer approval for one request and approval for the current generation without silently granting permissions to later follow-ups.
- Preserve conversation associations across CouchFusion restarts in a dedicated CouchDB database.
- Work safely when the site already has uncommitted changes, clearly separating the baseline from Codex-created changes.
- Automatically rebuild the canonical component registry after a successful generation.
- Keep preview opt-in through the existing preview-host workflow.

## Non-goals

- Committing, pushing, branching, reverting, or cleaning the site's Git repository.
- Reviewing or committing generated work on the user's behalf.
- Hosting Codex remotely or exposing app-server to the network.
- Managing ChatGPT/API-key login inside CouchFusion.
- Allowing arbitrary terminal sessions, working directories, writable roots, models, or skill roots from browser input.
- Automatically starting the component preview host.
- Storing a duplicate Codex transcript or raw command output in CouchDB.
- Generating non-Content components in version 1.
- Supporting multiple concurrent mutating agents in the same site.

## Approved Product Decisions

- The generation composer is the first entry in the build-time component registry panel.
- A new component starts a new conversation and a fresh Codex thread.
- Follow-up prompts retain that conversation and thread.
- Completed conversations collapse; the active conversation expands inline while the component list remains visible.
- Questions and approvals appear inline in the conversation.
- **Approve once**, **Approve for this generation**, **Decline**, and **Cancel** are available where the protocol request permits them.
- Existing uncommitted work is allowed and is never automatically reverted.
- Dependency changes are prohibited by default and require explicit user consent within the current conversation.
- Network access starts disabled and must be explicitly approved if Codex requests it.
- A successful turn triggers a registry rebuild and component-list refresh.
- Preview remains manual.
- Conversation metadata is stored in a dedicated CouchFusion CouchDB database.
- Codex may modify files only. Git review and commit remain the user's responsibility.

## Architecture

```text
Browser UI
  | trusted CouchFusion conversation API + SSE
  v
Go web server
  |-- registered workspace/site resolver
  |-- shared workspace mutation lease
  |-- agent conversation manager
  |-- CouchDB conversation store
  |-- registry rebuild service
  v
one supervised `codex app-server` process
  | JSON-RPC 2.0 over stdio
  v
Codex thread rooted at the selected Nuxt app
```

### Trust boundaries

The browser sends only CouchFusion resource identifiers, prompt text, and responses to a currently pending interaction. It never sends or overrides:

- the Codex executable or arguments;
- a filesystem path or writable root;
- a thread ID;
- approval or sandbox policy;
- environment variables;
- a skill path;
- a model or provider;
- a command to execute.

The Go application resolves workspace and site IDs through the explicit registry, canonicalizes the site directory, confirms that it remains contained by the registered apps directory, and constructs all app-server requests from server-owned configuration. Every later turn revalidates the site binding before it is allowed to mutate files.

### Component boundaries

- **App-server supervisor:** starts one long-lived subprocess, performs the initialize/initialized handshake, correlates request IDs, routes notifications and server requests, applies bounded queues, and restarts with backoff after unexpected exit.
- **Conversation manager:** owns CouchFusion conversation IDs, active turns, pending questions and approvals, per-turn approval scope, event snapshots, and recovery.
- **Conversation store:** persists minimal metadata in CouchDB and performs optimistic concurrency updates.
- **Workspace mutation lease:** prevents component generation from overlapping another mutating generation, provisioning job, registry write, or managed operation for the same workspace/site.
- **Prompt builder:** supplies fixed product instructions, the first-party skill, and the distinct user prompt.
- **Registry completion hook:** starts the existing canonical registry rebuild after a successful Codex turn and publishes its result.
- **Web UI:** renders the composer, conversation, approvals, questions, changed files, and recovery actions without interpreting raw app-server messages.

## App-server Lifecycle and Protocol

At CouchFusion server startup, the supervisor locates the locally installed `codex` binary and records its version. App-server is started lazily on first agent use or by an explicit readiness check. It runs as a child of the Go process with stdin/stdout reserved for JSON-RPC and stderr captured as bounded diagnostic output.

The supervisor:

1. starts `codex app-server` using the local environment;
2. sends `initialize` and then the `initialized` notification;
3. reads account/auth readiness through the protocol;
4. registers the embedded CouchFusion skill root;
5. starts or resumes threads on demand;
6. dispatches thread/turn/item notifications to the owning conversation;
7. answers server-initiated requests only after the owning UI interaction is resolved;
8. interrupts active turns during graceful CouchFusion shutdown.

The implementation must generate or consume protocol types from the schema exposed by the installed Codex version. Startup rejects a version whose required methods or fields cannot be represented safely. It must not assume that a development checkout's protocol exactly matches every installed binary.

Only one app-server child is owned by a CouchFusion process. Multiple conversations may be retained, but version 1 allows one active mutating turn per site and uses the shared mutation lease to serialize conflicting work.

### Authentication and local configuration

CouchFusion reuses Codex's locally persisted account session. It does not read, display, proxy, or persist the underlying credential. If `account/read` indicates that the user is not authenticated, generation is disabled and the UI explains that the user must authenticate with the local Codex installation.

Thread creation omits browser-selectable model configuration and follows the local Codex default. CouchFusion may show the effective model returned by app-server as read-only diagnostic metadata.

### Process failure

If app-server exits unexpectedly, the supervisor:

- marks active turns interrupted rather than successful or failed-by-code-change;
- clears all ephemeral pending request IDs and approval grants;
- publishes a recoverable connection state;
- restarts app-server with bounded exponential backoff;
- allows retained conversations to resume their Codex threads after readiness returns.

An approval received for an obsolete request ID is rejected idempotently. CouchFusion never replays an approval after a process restart.

## Thread and Conversation Model

Each **new component** action creates:

- a new CouchFusion conversation document;
- a new Codex thread rooted at the selected site;
- a Git/file baseline for distinguishing later changes.

A follow-up prompt adds a new turn to the same Codex thread. A conversation has at most one active turn. Starting a new mutating conversation while the site lease is held returns a stable conflict response and links to the active conversation.

The Codex thread is the transcript authority. CouchFusion stores enough metadata to list, bind, and recover a conversation, but it does not duplicate transcript contents. When a conversation is opened, CouchFusion uses `thread/read` and, when required, `thread/resume` to recover the available transcript and current thread state.

### Dedicated CouchDB database

The database name is `couchfusion-control`. It uses the existing CouchFusion CouchDB connection and locally stored administrative credentials. The database is created lazily when the feature is first used, after credentials and connectivity are validated. Failure to create or access it disables generation without affecting ordinary workspace, site, task, registry, or preview functionality.

The database is reserved for CouchFusion control-plane documents. Agent conversation documents use an explicit type and schema version, for example:

```json
{
  "_id": "agent-conversation:<couchfusion-id>",
  "type": "agent-conversation",
  "schemaVersion": 1,
  "conversationId": "<opaque CouchFusion id>",
  "codexThreadId": "<server-owned id>",
  "workspaceId": "<registered workspace id>",
  "siteId": "<registered site id>",
  "siteRootFingerprint": "<hash of canonical binding>",
  "title": "Hero banner component",
  "status": "ready|active|waiting|interrupted|completed|failed",
  "generatedComponentId": "optional registry component id",
  "baseline": {
    "capturedAt": "RFC3339 timestamp",
    "files": [{"path": "relative/path", "status": "tracked|untracked", "sha256": "..."}]
  },
  "changedFiles": [{"path": "relative/path", "kind": "added|modified|deleted"}],
  "lastTurn": {"status": "completed", "summary": "optional bounded summary"},
  "createdAt": "RFC3339 timestamp",
  "updatedAt": "RFC3339 timestamp"
}
```

The baseline stores relative paths, status, and content hashes, not source contents. Sensitive-looking paths are filtered from UI summaries. Raw approval tokens, pending protocol request IDs, credentials, prompts, transcripts, source diffs, and command output are not persisted in CouchDB.

CouchDB `_rev` provides optimistic concurrency. A conflict causes the service to reload and retry only state transitions that remain valid; it never overwrites a newer active-turn or approval state.

### Site binding and restart recovery

The site-root fingerprint binds the conversation to the workspace ID, site ID, canonical application root, and registered apps-root relationship. On every resume, CouchFusion recalculates the fingerprint. A mismatch disables mutation and asks the user to inspect or create a new conversation; it never silently retargets an old Codex thread.

After a CouchFusion restart:

- retained conversations can be listed from CouchDB;
- their transcript is reloaded from Codex by thread ID;
- a turn that was active becomes `interrupted` and may be followed up or retried;
- pending approvals and clarification requests are gone and cannot be replayed;
- per-generation approval grants are gone;
- baseline and changed-file attribution remain available.

## Prompt and First-party Skill

CouchFusion ships a first-party `couchfusion-component-builder` skill derived from the existing node-editor component workflow. The skill is versioned with CouchFusion, embedded in the Go binary, and materialized into a private CouchFusion-managed state directory with restrictive permissions. The server registers that directory through the supported app-server skill-root mechanism. Users do not need to install a personal skill.

The skill directs Codex to:

- inspect the selected Nuxt app and its applicable `AGENTS.md` files;
- inspect existing Content components and shared registry conventions before editing;
- create or update application-local Vue Content components;
- define appropriate props, slots, and builder metadata;
- preserve the established visual system and application structure;
- run focused validation available in the existing project;
- rely on the canonical registry generator rather than hand-maintaining duplicate registry data.

Each initial generation turn contains four distinct context layers:

1. applicable repository instructions discovered naturally from the trusted working directory;
2. fixed CouchFusion developer instructions covering Content conventions and safety boundaries;
3. an explicit app-server skill input referencing `couchfusion-component-builder`;
4. the user's prompt as a separate user input item.

Fixed developer instructions require app-local writes, preservation of pre-existing dirty work, no Git history operations, no dependency changes without consent, no network without approval, focused checks, and registry rebuild expectations. User prompt text is never interpolated into the developer instructions or a shell command.

## Filesystem, Git, Dependency, and Network Policy

The thread working directory and sole writable root are the validated site application root. Network access starts disabled. The approval policy delegates interactive requests to CouchFusion's user reviewer.

CouchFusion records the baseline before the first turn. After every turn, it compares current file status and hashes against that baseline and returns only relative changed-file summaries. It does not stage, commit, branch, push, reset, checkout, clean, or restore files. Pre-existing modifications remain explicitly labelled as baseline work.

Writes outside the application root are denied. Symlink and canonical-path checks apply to server-owned file operations, while the Codex sandbox remains the primary execution boundary.

Package manifests and lockfiles form a protected dependency-change class. Product instructions tell Codex to ask first. The UI must record explicit dependency consent for the current turn before it will approve a file-change or command request that targets a recognized package or lock file. **Approve for this generation** cannot bypass this guard. Consent expires when the turn completes, is interrupted, or the server restarts.

Network permission follows the same explicit interaction model. A network grant is limited to the current turn and is cleared on completion, interruption, or restart.

## Approval and Clarification Semantics

App-server can issue server-initiated requests for commands, file changes, permissions, and user input. The Go server converts these into a small, versioned CouchFusion interaction model. The browser receives an opaque CouchFusion request ID, a sanitized description, affected relative paths where available, and the allowed decisions. It never receives a raw app-server request ID suitable for direct protocol use.

### Decisions

- **Approve once:** approves only the displayed request.
- **Approve for this generation:** remembers a narrowly classified approval in memory for the active turn and automatically approves later matching requests in that same turn.
- **Decline:** rejects the displayed request and allows Codex to continue if the protocol does.
- **Cancel:** rejects the request and interrupts the active turn.

`Approve for this generation` is a CouchFusion turn scope, not an indefinite Codex thread scope. If the installed app-server's `acceptForSession` lifetime exactly matches the active turn, it may be used. Otherwise CouchFusion emulates the behavior by answering the current request once and remembering the sanitized request class for later requests. The grant is always cleared at `turn/completed`, interruption, conversation switch, app-server restart, or CouchFusion restart. It never carries into a follow-up turn on the retained thread.

Approval classes are deliberately narrow: command signature and risk class, bounded file-path class, or a named permission. A generation-wide approval never grants arbitrary commands, dependencies, writes outside the app, Git history operations, or network access.

Clarification answers are accepted only for a currently pending question in the active conversation and turn. Field shapes and answer lengths are validated before forwarding. Duplicate, stale, or foreign answers receive a stable conflict response.

## HTTP and Event API

All endpoints use the existing CouchFusion authentication/origin protections and return the project's standard error envelope.

```text
GET  /api/v1/workspaces/{workspaceId}/sites/{siteId}/agent-conversations
POST /api/v1/workspaces/{workspaceId}/sites/{siteId}/agent-conversations

GET  /api/v1/agent-conversations/{conversationId}
POST /api/v1/agent-conversations/{conversationId}/turns
POST /api/v1/agent-conversations/{conversationId}/approvals/{requestId}
POST /api/v1/agent-conversations/{conversationId}/questions/{requestId}/answer
POST /api/v1/agent-conversations/{conversationId}/interrupt
GET  /api/v1/agent-conversations/{conversationId}/events
```

Creation accepts only a bounded prompt:

```json
{"prompt": "Create a responsive testimonial carousel..."}
```

Follow-up creation accepts the same prompt shape. The server derives workspace, site, root, skill, policies, and Codex thread association.

An approval accepts a fixed decision enum:

```json
{"decision": "approve_once|approve_generation|decline|cancel"}
```

A dependency or network consent request uses a distinct typed interaction and records explicit consent before the protected request can be approved. Generic approval payloads cannot manufacture consent.

The conversation detail response is a complete UI-safe snapshot containing metadata, transcript items normalized from Codex, active-turn state, pending interaction, changed-file summary, registry completion result, and capability/readiness flags. Unknown app-server item types are represented as generic informational items rather than causing response failure.

### SSE

The event endpoint streams normalized CouchFusion events, not raw JSON-RPC messages. Event types include:

```text
conversation.snapshot
turn.started
item.started
item.delta
item.completed
interaction.requested
interaction.resolved
turn.completed
registry.started
registry.completed
registry.failed
conversation.interrupted
conversation.failed
```

Events carry monotonically increasing conversation-local sequence numbers. The UI reconnects with the last sequence when retained events are available; otherwise the server sends a fresh snapshot. Bounded in-memory event history prevents unbounded logs. The persistent document remains a summary, not an event store.

Mutation endpoints are idempotent for the same resolved state. Duplicate prompts are not implicitly deduplicated because they intentionally create turns; the UI supplies an idempotency key for create-turn retries caused by network uncertainty.

## UI Design

The build-time registry panel begins with an integrated **Create component with Codex** entry. In its idle state it contains a concise prompt field, examples of useful detail, readiness status, and a primary Generate action. It does not displace registry rebuild or preview-host controls.

When generation starts, that entry expands into the active conversation:

- user and Codex messages appear chronologically;
- streamed progress is compact and bounded;
- clarification questions render as focused inline forms;
- approval cards show the proposed action, affected paths, risk notes, and permitted decisions;
- the changed-files section distinguishes **already modified before generation** from **changed by this conversation**;
- an interrupt action remains available during an active turn;
- a follow-up composer appears when the turn becomes ready;
- no commit or push action is offered.

Completed conversations collapse into compact rows with title, outcome, updated time, generated component link when known, and a Resume action. Opening one reloads its Codex transcript. A new component always starts a separate conversation.

The component list remains below the generator. After successful generation and registry rebuild, the resulting component is highlighted and scrolled into view when its registry ID can be resolved. The user may then start the preview host and open the existing fullscreen desktop/mobile preview manually.

### UI state model

```text
unavailable -> idle -> starting -> active
                                  |-- waiting_for_question
                                  |-- waiting_for_approval
                                  |-- interrupting
                                  `-- completing -> rebuilding_registry

rebuilding_registry -> ready
                    `-> ready_with_registry_warning

active -> interrupted -> ready_for_follow_up
active -> failed      -> ready_for_follow_up
```

`unavailable` includes missing Codex, unsupported protocol, missing local authentication, inaccessible control database, or changed site binding. Each condition has specific repair guidance and does not disable the rest of the site page.

## Registry Rebuild and Completion

A successfully completed Codex turn releases its active agent execution state but keeps the site mutation lease while CouchFusion invokes the existing canonical component-registry rebuild. No second prompt is accepted for that site until the rebuild finishes.

If the rebuild succeeds:

- the conversation records a successful registry result;
- the component list refreshes automatically;
- CouchFusion identifies and highlights a newly added or changed registry component when possible;
- preview remains stopped unless the user explicitly starts it.

If the rebuild fails, generated files remain untouched. The conversation becomes ready with a registry warning and offers the existing Rebuild Registry action. The user can inspect or correct the files with a follow-up turn. A registry failure never masquerades as a failed Codex file generation.

If Codex makes no file changes, CouchFusion still reports turn completion but skips rebuild unless registry source inputs are detected as changed.

## Error and Recovery Behavior

- **Codex binary missing:** generation is unavailable with installation/path guidance.
- **Unsupported app-server protocol:** generation is unavailable and reports installed versus supported version information.
- **Local session missing:** generation is unavailable with local Codex login guidance.
- **CouchDB unavailable:** agent features are unavailable; existing UI control-plane features continue.
- **Site moved or re-registered:** old conversation becomes read-only until its binding is restored; a new conversation may be created for the new binding.
- **Workspace lease conflict:** creation returns a conflict pointing to the active job or conversation.
- **SSE disconnect:** UI reconnects and consumes missed bounded events or a full snapshot.
- **App-server crash:** active turn becomes interrupted; no pending approval survives; retained thread can receive a follow-up after restart.
- **CouchFusion restart:** active turn becomes interrupted during recovery; transcript is reloaded from Codex.
- **Approval race:** first valid resolution wins; later resolutions receive the already-resolved state.
- **Turn interruption timeout:** server terminates/restarts app-server if a graceful interrupt does not settle, without replaying pending actions.
- **Registry failure:** files remain and the UI offers retry.
- **Malformed/oversized output:** bounded normalization truncates diagnostic display and preserves server stability.

## Observability and Privacy

The system page adds agent readiness for Codex binary/version, app-server lifecycle, local account readiness, control database, skill registration, and protocol compatibility. It does not display credentials.

Structured server logs use CouchFusion conversation and turn IDs. They redact local session data, CouchDB credentials, raw approval identifiers, prompt bodies, source contents, diffs, and token-like URL values. Command output shown during an active conversation is bounded and not persisted in CouchDB. The authoritative transcript remains in the locally managed Codex thread.

## Implementation Shape

The implementation should introduce focused packages rather than adding protocol state to `internal/web` handlers:

- `internal/agent/protocol`: version-matched JSON-RPC types, codec, and normalization;
- `internal/agent/supervisor`: app-server process lifecycle and request routing;
- `internal/agent/conversations`: state machine, approval scope, and recovery;
- `internal/agent/store`: dedicated CouchDB documents and migrations;
- `internal/agent/prompt`: embedded skill and fixed instruction construction;
- a shared workspace mutation lease used by existing jobs and the agent manager;
- thin HTTP/SSE handlers in `internal/web`;
- typed web client/composables and focused generator components in `web`.

The first-party skill source lives with the Go implementation, is embedded at build time, and is materialized deterministically. The web UI consumes normalized CouchFusion types; generated Codex protocol structures do not leak into Vue components.

## Testing Strategy

### Protocol and supervisor

- Fake stdio app-server tests for handshake, request correlation, notifications, server requests, malformed messages, stderr, exit, restart, and shutdown.
- Contract fixtures generated from the supported installed protocol schema.
- Compatibility tests that fail closed when required methods or decision semantics are absent.
- Race tests for concurrent notifications, approval resolution, interrupt, and process exit.

### Conversation manager and security

- State-transition tests for new conversation, follow-up, question, approval, completion, failure, and interruption.
- Tests proving generation approvals expire at turn completion, interruption, conversation switch, and restart.
- Tests proving a retained-thread follow-up receives no approval inherited from the previous turn.
- Dependency and network guards that cannot be bypassed by generic approval.
- Registered-site containment, site-fingerprint mismatch, browser-field rejection, and writable-root tests.
- Shared mutation-lease tests against provisioning, registry rebuild, and other agent work.
- Dirty-baseline attribution tests for tracked, untracked, added, modified, deleted, and unchanged files.

### CouchDB store

- Lazy database creation, schema validation, optimistic conflict, list/query, and restart-recovery tests.
- Tests proving transcript, prompts, source contents, raw output, credentials, and pending protocol IDs are not stored.
- Migration behavior for unknown/newer schema versions fails read-only rather than corrupting documents.

### API and SSE

- Route ownership and validation tests for every endpoint.
- Stable conflict behavior for stale approvals, questions, duplicate resolutions, active turns, and idempotency keys.
- Snapshot, reconnect, bounded event history, and normalized unknown-item tests.
- Standard error-envelope and authentication/origin regression tests.

### Web UI

- Composer idle/readiness states and prompt validation.
- Active transcript, compact streaming progress, questions, and all approval decisions.
- Dependency/network consent flow and generation-scope expiry.
- Baseline-versus-generated changed-file grouping.
- Collapsed retained conversations and follow-up resume.
- Automatic registry refresh/highlight and manual preview behavior.
- Missing Codex, auth, database, site mismatch, crash, restart, disconnect, and registry-warning states.

### End-to-end acceptance

A fake deterministic app-server drives browser tests for prompts, questions, approvals, restart, and failures. An opt-in local integration test uses an authenticated Codex installation against a disposable fixture repository. It verifies app-local changes, absence of Git operations, dirty-work preservation, registry rebuild, retained-thread follow-up, restart recovery, and no approval leakage into the next turn.

## Rollout and Compatibility

The feature is capability-gated. CouchFusion starts normally when Codex or CouchDB agent storage is unavailable; only the generator entry is disabled. Existing CLI, workspace, site, task, registry, and preview APIs remain compatible.

Version 1 supports the app-server protocol range explicitly covered by generated schemas and contract fixtures. Protocol support expands deliberately after fixture updates. No browser endpoint exposes experimental app-server fields directly.

The control database schema is versioned from its first document. Unknown newer documents are left untouched and shown read-only. Skill materialization is content-addressed so a CouchFusion upgrade can register the new skill without mutating an active generation.

## Acceptance Criteria

- A registered site can start a new component-generation conversation from the first registry-panel entry.
- CouchFusion uses a locally authenticated, supervised `codex app-server`; the browser cannot select privileged execution parameters.
- The Codex thread runs with the canonical site app as its only writable root and network disabled initially.
- The embedded Content component skill and fixed product instructions accompany every initial generation.
- Questions, approvals, decline, cancel, and interrupt work inline in CouchFusion.
- **Approve for this generation** expires before any follow-up turn and after interruption or restart.
- Dependency and network changes require distinct explicit consent and cannot be covered accidentally by a broad generation approval.
- Sites with uncommitted changes are supported, baseline work is distinguished from new changes, and no work is reverted automatically.
- CouchFusion never commits, pushes, branches, stages, cleans, or rewrites Git history.
- Separate component tasks use separate Codex threads; follow-ups retain the original thread.
- Conversation associations survive CouchFusion restart through `couchfusion-control`, while transcripts remain owned by Codex.
- Active work recovers as interrupted, and stale approvals are never replayed.
- Successful changes automatically rebuild and refresh the canonical component registry.
- Registry failure preserves generated files and offers retry; component preview remains manual.
- Missing Codex, auth, compatible protocol, or control database disables only the generator with actionable guidance.
- Focused unit, race, API, UI, fake-server browser, and opt-in local integration tests cover the security and recovery boundaries.
