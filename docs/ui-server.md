# CouchFusion UI server

## Prerequisites

Component generation uses your locally installed and authenticated Codex session. Verify it before starting CouchFusion:

```bash
codex --version
codex login
```

The login command is only needed when Codex reports that local authentication is missing. CouchFusion does not collect or display Codex credentials.

From an installed release, run:

```bash
couchfusion ui
```

From this source checkout, use UI development mode for Vue and CSS HMR:

```bash
cd web
bun install
cd ..
go run . ui --dev
```

The equivalent installed command is `couchfusion ui --dev` when the checkout's `web` dependencies are available. Open the tokenized loopback URL printed in the terminal.

Register an existing workspace by its exact absolute path, or initialize a new one. CouchFusion never scans the disk for projects. Use `--no-open` to prevent automatic browser launch and `--port 4400` to request a fixed Go server port.

## Generate a content component

1. Open **System** and confirm **Codex generation** reports the binary, app server, local account, `couchfusion-control`, and component skill as ready.
2. Open a workspace site and find **Create component with Codex** at the top of **Local content components**.
3. Describe the component's content, behavior, responsive layout, and visual tone, then select **Generate**.
4. Answer clarification questions and review each proposed command or file change. **Approve for this generation** applies only to the current turn.
5. Dependency-file changes and network access always require their own explicit **this generation only** consent before approval.
6. Review **Already modified before generation** separately from **Changed by this conversation**. CouchFusion automatically rebuilds the component registry after a successful turn.
7. If that rebuild fails, the generated files remain in place. Select **Retry rebuild** after correcting the reported problem.
8. Use the follow-up field to continue the retained Codex thread. A follow-up does not inherit approval from the previous turn.

CouchFusion allows existing tracked and untracked changes and never reverts them automatically. Agent work is restricted to the registered site. The workflow does not stage, commit, branch, push, reset, clean, or otherwise change Git history. Review and commit all changes yourself.

## Preview a registered component

1. Open a workspace, then a site with a ready build-time component registry.
2. Select **Start preview host** in **Local content components**.
3. Select **Preview** beside a component.
4. Use the desktop/mobile controls at the top of the fullscreen preview. Mobile uses a 375 px render viewport.
5. Edit the site's component source; the preview updates through the site's Nuxt HMR connection.
6. Close the fullscreen view when finished. Select **Stop preview host** to terminate its dedicated Nuxt process.

The preview host is explicit, separate from the site's normal development task, and bound only to loopback. If it fails to start, confirm the site has an exact `dev` package script and installed dependencies. Rebuild a missing or stale registry, and inspect the reported host problem or Nuxt terminal output for compile errors. Stop CouchFusion with Ctrl-C; it also stops UI-managed child processes.

Component generation never starts the preview host automatically. Start it manually only after the registry is ready.

## Readiness and repair

- **Missing:** `codex --version` must resolve from the environment that starts CouchFusion.
- **Unauthenticated:** run `codex login` in a local terminal, then rerun System checks.
- **Unsupported:** update the local Codex CLI to the version range reported by CouchFusion.
- **Launch failed:** run `codex app-server --help`. If the launcher exists but its packaged native process cannot start, repair or reinstall that local Codex installation.
- **Control database unavailable:** restore CouchDB administrative access and rerun checks.

Conversation associations and recovery summaries live in the dedicated `couchfusion-control` database. Prompts, transcripts, source contents, credentials, and raw approval identifiers are not stored there; the locally installed Codex session remains the transcript authority.

After a CouchFusion or Codex app-server restart, an active turn is recovered as interrupted and pending approvals are discarded. Reopen the retained conversation and send a follow-up when ready. Nothing is replayed or approved automatically.
