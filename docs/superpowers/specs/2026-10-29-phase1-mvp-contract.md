# Phase 1 MVP — shared contract (server ⇄ SPA ⇄ layers/content)

Date: 2026-10-29. Parent spec: `2026-10-29-rust-vps-control-plane-design.md` (§4–§6, §8, §11). Phase 0 results: `docs/implementation_results/2026-10-29-hetzner1-phase0-progress.md`.

This file is the single source of truth for the three Phase 1 work streams. If something here conflicts with the parent spec, **this file wins** for Phase 1. Any stream that must deviate records the deviation at the bottom of this file under "Deviations".

| Stream | Owns | Must not touch |
|---|---|---|
| A — Rust server | `cli-init/server/**` (crate + `packaging/hetzner1/phase1/**`) | Go code in `cli-init` (branch `ui` has uncommitted user work), `web/`, `layers/` |
| B — SPA | `cli-init/web/**` (new page, composables, types, small existing-file edits) | Go code, `server/`, `layers/` |
| C — Content layer | `layers/content/**`, `layers/content/docs/**` | `cli-init/**`, apps |

## 1. Fixed facts (hetzner1)

- App: `radustanciu`. Workspace root: `/srv/couchfusion/workspace` (`apps/radustanciu`, `layers`, `cli-content`). Workspace id: `hetzner1` (fixed, from config). Site id: `radustanciu`.
- Dev unit: `cf-dev@radustanciu.service` (exists, Phase 0), `127.0.0.1:4401`, env `/etc/couchfusion/env/radustanciu.dev.env` (+ `/run/couchfusion/dev/radustanciu.env` written by the server, see §5).
- Control plane: port **9180**; dev slot: port **9181**. Tailnet origin `https://hetzner1.tailb7471b.ts.net:{9180,9181}`; tunnel origin `http://localhost:{9180,9181}`.
- Tailnet listeners are unix sockets `/run/couchfusion/ts-9180.sock`, `/run/couchfusion/ts-9181.sock` targeted by `tailscale serve --https=P unix:…`. On these, `Host` is `localhost`; the real host is `X-Forwarded-Host`; `Tailscale-User-Login` is set by tailscaled and **cannot be forged** (spike S3).
- Allowed login (D4): `r67rggkzzb@privaterelay.appleid.com` (configurable list).
- Registry regeneration on hetzner1 takes 2.3–3.6 s; Nuxt answers 503 while Vite warms up.

## 2. HTTP API (Stream A implements, Stream B consumes)

Prefix `/api/v1`. Envelope `{"data": …}` / `{"error":{"code","message","fields?"}}`. Error codes and statuses as Go (`validation|config`→422, `conflict|process`→409, `not_found`→404, `unavailable`→503, `git`→502, `filesystem|internal`→500, `unauthorized`→401, `forbidden`→403). Optional fields are **omitted**, never `null`. Times are RFC3339 UTC strings. Mutations: `Content-Type: application/json`, body ≤ 1 MiB, `{}` accepted (also on DELETE). All API responses `Cache-Control: no-store`.

### 2.1 Kept (subset of Go, same shapes as `web/types/api.ts`)
| Method | Path | Response |
|---|---|---|
| GET | `/bootstrap` | `{version:"1", backend:"rust", capabilities:["dev-sessions","component-delete","component-generate","git"], devOrigins:["<slot origin as seen by caller>"], workspaceId:"hetzner1", siteId:"radustanciu"}` |
| GET | `/onboarding` | `OnboardingState` with `status:"completed"` (so the SPA never redirects to `/getting-started`) |
| GET | `/workspaces` | `WorkspaceSummary[]` (one entry) |
| GET | `/workspaces/{ws}` | `WorkspaceDetail` (sites from config; `layers:[]`, `warnings:[]` allowed) |
| GET | `/workspaces/{ws}/sites/{site}/components` | `ComponentRegistryResponse` (Go manifest rules) |
| GET | `/jobs/{id}` | `JobSnapshot` |
| POST | `/jobs/{id}/cancel` | 202 `JobSnapshot` |
| GET | `/jobs/{id}/events` | SSE `event: job`, data `ActivityEvent`; resume `?after=` / `Last-Event-ID`; closes when terminal |

### 2.2 New
```ts
type DevStatus = 'stopped' | 'starting' | 'ready' | 'stopping' | 'failed'
interface DevSessionView {
  siteId: string
  unit: string                 // "cf-dev@radustanciu.service"
  status: DevStatus
  url: string                  // slot origin as seen by THIS caller, e.g. "https://hetzner1.tailb7471b.ts.net:9181"
  startedAt?: string
  memoryBytes?: number
  needsRestart?: { reason: string }
  problem?: string
}
interface ComponentUsage {
  componentId: string
  source: string               // "app/components/content/NetworkHero.vue"
  sha256: string
  git: 'tracked_clean' | 'tracked_modified' | 'untracked' | 'unknown'
  pages: { id: string; path: string; title: string }[]
  codeReferences: { path: string; line: number }[]
  pageScan: 'ok' | 'unavailable'
}
interface RepoSummary { id: string; path: string; branch: string; head?: { sha: string; subject: string }; dirty: number; hasRemote: boolean }
interface RepoStatus  { id: string; branch: string; head?: { sha: string; subject: string }; hasRemote: boolean;
                        files: { path: string; index: string; worktree: string; untracked: boolean }[] }
interface RepoDiff    { path: string; diff: string; truncated: boolean; binary: boolean }
interface SiteEvent   { seq: number; type: 'dev.status' | 'log' | 'registry.changed' | 'job'; time: string;
                        dev?: DevSessionView; log?: ActivityEvent; registry?: { added: string[]; removed: string[] }; job?: JobSnapshot }
```
`JobSnapshot` gains an optional `result?: Record<string, unknown>` (`component.delete` → `{componentId, trashId}`; `component.generate` → `{componentId?, changedFiles: string[], threadId?, lint: string[], needsRestart?: {reason}}`; `git.commit` → `{sha}`).

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/workspaces/{ws}/sites/{site}/dev` | – | `DevSessionView` |
| POST | `…/dev/start` · `…/dev/stop` · `…/dev/restart` | `{}` | 202 `DevSessionView` |
| GET | `/workspaces/{ws}/sites/{site}/events` | – | **SSE `event: site`**, data `SiteEvent`, `id: seq`; resume `?after=`; `:keepalive` comment every 15 s. One stream per site carries dev status, coalesced dev logs (≤ 4 frames/s), registry changes and job updates for that site. |
| GET | `…/sites/{site}/components/{componentId}/usage` | – | `ComponentUsage` |
| DELETE | `…/sites/{site}/components/{componentId}` | `{expectedSha256: string, acknowledgeUsage: boolean}` | 202 `JobSnapshot` `type:"component.delete"`; 409 `conflict` with `fields` on sha mismatch / unacknowledged usage / lease busy (`fields.activeJobId`) |
| POST | `…/sites/{site}/components/generate` | `{prompt}` (1–16384 bytes trimmed) + header `Idempotency-Key` (1–128) | 202 `JobSnapshot` `type:"component.generate"` |
| GET | `/workspaces/{ws}/repos` | – | `RepoSummary[]` (`apps/radustanciu`, `layers`, `cli-content`; id is the relative path URL-encoded, e.g. `apps%2Fradustanciu`) |
| GET | `/workspaces/{ws}/repos/{repo}/status` | – | `RepoStatus` |
| GET | `/workspaces/{ws}/repos/{repo}/diff?path=…` | – | `RepoDiff` (256 KiB cap) |
| POST | `/workspaces/{ws}/repos/{repo}/commit` | `{message, paths: string[], expectedHead: string}` | 202 `JobSnapshot` `type:"git.commit"` |

No push in Phase 1 (U9: no remote yet). Job events use codes such as `component.resolve`, `component.trash`, `registry.wait`, `registry.updated`, `codex.message`, `codex.command`, `codex.files`, `codex.done`, `policy.dependency_change`, `lint.builder_field_meta`, `git.commit`.

## 3. Auth (Stream A)
- **Unix-socket listeners (tailnet):** authenticated iff `Tailscale-User-Login` ∈ `auth.allowed_logins`; else 401. No cookie is set.
- **TCP listeners `127.0.0.1:9180/9181` (SSH tunnel):** `Tailscale-*` headers are ignored/stripped. Login via `couchfusion-server login-url` → `http://localhost:9180/auth/exchange?token=…` (single use, 10 min) → cookie `cf_cp_s` (`HttpOnly; SameSite=Strict; Path=/`) → 303 `/`. Sessions persist (sqlite, hashed).
- Host allowlist: on unix listeners check `X-Forwarded-Host` ∈ `hetzner1.tailb7471b.ts.net:{9180,9181}`; on TCP check `Host` ∈ `localhost:{9180,9181}`, `127.0.0.1:{9180,9181}`.
- Mutations: reject `Sec-Fetch-Site: cross-site|same-site`; `Origin`, when present, must equal the caller's own origin (https on tailnet, http on tunnel).
- The slot (9181) authenticates the same way, then strips `cf_cp_s` and `Tailscale-User-*` before proxying.

## 4. Slot proxy (Stream A)
- 9181 → `127.0.0.1:4401`. Rewrite `Host` to `127.0.0.1:4401`; set `X-Forwarded-{Proto,Host,Port}`. Pass WebSocket upgrades (Vite HMR) through bidirectionally.
- Response: drop `X-Frame-Options`; set/append `Content-Security-Policy: frame-ancestors https://hetzner1.tailb7471b.ts.net:9180 http://localhost:9180`.
- Boot page when upstream is down/starting (or answers 503 during warm-up for `text/html` navigations): small HTML polling `/__cf/slot-status` (JSON `{status}`) every 1 s and reloading on `ready`.
- Slots never route `/api/v1` to the control plane.

## 5. Dev runtime env (Stream A writes, Stream C reads)
Before starting `cf-dev@<app>`, the server writes `/run/couchfusion/dev/<app>.env` (0640 `cfcp:cfdev`):
```
CF_DEV_PORT=4401
COUCHFUSION_CONTROL_TOKEN=<32B b64url, new per start>
COUCHFUSION_PUBLIC_ORIGIN=https://hetzner1.tailb7471b.ts.net:9181
NUXT_PUBLIC_CONTENT_CONTROL_PLANE_ORIGINS=https://hetzner1.tailb7471b.ts.net:9180,http://localhost:9180
```
and the unit gets a drop-in `EnvironmentFile=-/run/couchfusion/dev/%i.env` (Stream A packaging). `__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS` stays in the static dev env file.

Readiness = `GET http://127.0.0.1:4401/__couchfusion/control/ping` with header `x-couchfusion-control-token: <token>` → `200 {"ready":true}`; fallback: HTTP 200 on `/`. Poll 250 ms, up to 180 s.

## 6. Control routes in the dev server (Stream C implements, Stream A calls)
Registered **only** when `import.meta.dev`/`nuxt.options.dev` and `COUCHFUSION_CONTROL_TOKEN` is set. Every request must carry `x-couchfusion-control-token` equal to the env token (constant-time compare), otherwise **404**.
| Route | Response |
|---|---|
| `GET /__couchfusion/control/ping` | `{"ready":true}` |
| `GET /__couchfusion/control/component-usage?id=<kebab-id>` | `{"pages":[{"id","path","title"}],"scanned":<number>}` — scans `page-*` docs in the content DB (all locales) for minimark tags equal to the id (and its PascalCase alias). |

## 7. postMessage bridge v1 (Stream B = parent, Stream C = child)
Envelope: `{cf:1, type, nonce?, id, replyTo?, payload}`. `id` is a random string per message. Types:
| Dir | Type | Payload |
|---|---|---|
| C→P | `cf:host.ready` | `{app, route, builder: boolean, dirty: boolean}` |
| P→C | `cf:host.welcome` | `{nonce, capabilities: ["component.delete","component.create","reload"]}` |
| C→P | `cf:host.dirty` | `{dirty}` |
| C→P | `cf:host.component.delete.request` | `{componentId}` |
| C→P | `cf:host.component.create.request` | `{promptHint?: string /* ≤ 2 KB */}` |
| P→C | `cf:host.request.result` | `{status: "accepted"|"cancelled"|"failed"|"done", message?, componentId?}` with `replyTo` = request id |
| P→C | `cf:host.reload.request` | `{reason, focusComponentId?}` |
| C→P | `cf:host.reload.ack` | `{reloading: boolean, reason?: "unsaved"}` |
| P→C | `cf:host.component.focus` | `{componentId}` |
Rules:
- Child posts `cf:host.ready` to `window.parent` (or `window.opener`) using `targetOrigin` = each configured origin from `runtimeConfig.public.content.controlPlane.origins` (comma list). Child is active only if `import.meta.dev`, origins non-empty, and framed/opened. It accepts only messages whose `event.origin` is in the list, pins that origin on `welcome`, and includes the nonce in every later message. Until `welcome`, `connected=false` and no host buttons render.
- Parent accepts only `event.source === iframe.contentWindow` (or the opened window) and `event.origin === slotOrigin`; after `welcome`, requires the nonce; ≤ 5 msgs/s.
- Requests are never authority: delete opens the parent's confirm modal (path, sha, git state, usages) and only then calls `DELETE`; create opens the parent's agent drawer prefilled.
- Reload: child reloads if clean; if dirty, it confirms itself and acks `{reloading:false, reason:"unsaved"}`. Parent: if no `ready` within 3 s after a reload request, reassign `iframe.src`. After a create-triggered reload, parent sends `cf:host.component.focus` once the new `ready` arrives; child opens the picker scrolled to that component (best effort).

## 8. Out of scope for Phase 1 (do not build)
Push, production units/deploys, Codex app-server approvals, preview-host routes, trash restore UI (trash is written; restore is Phase 2), multi-slot, cookie prefixing, idle reaper.

## Deviations
(append here: stream, what, why)
- **C — picker delete eligibility.** The builder's `ComponentDefinition` (from the generated `.ts`) has no `source`. The trash button is therefore shown for definitions that are neither global aliases nor primitives. The picker's options are exactly primitives, global aliases and the app's project registry, so this equals "project component". **Stream A must enforce** the manifest `source` under `app/components/content/` and answer 422 otherwise (the parent reports that back as `failed`).
- **C — no HMR override.** The gated module only appends the `COUCHFUSION_PUBLIC_ORIGIN` hostname to `vite.server.allowedHosts`. It never sets `vite.server.hmr`, because Vite derives the socket from the page URL, which works for both tailnet and tunnel. Forcing a host would break the other path. **Stream A's slot proxy must pass WebSocket upgrades for `/_nuxt/` HMR.**
- **C — usage `title`.** `component-usage` returns `title: ""` when a page has no title (contract type is `string`).
- **C — ready retries.** The child re-sends `cf:host.ready` every 1 s, up to 10 times, until `welcome`. The parent should treat repeated `ready` before `welcome` as idempotent, answering each with the same nonce or ignoring duplicates after its first `welcome`.
- **C — focus path.** On `cf:host.component.focus` the Workbench opens the root picker (also when it mounts later). The picker then shows "All", clears the search, scrolls to and highlights the card.
- **B — one nonce per bridge session.** The parent answers every `cf:host.ready` with the same nonce until the frame is re-mounted (forced reload) or `reset()`, so Stream C's `ready` retries are idempotent. Origin and source checks still apply to every message.
- **B — job following.** The dev page follows jobs through site SSE `job` events, plus a 2 s `GET /jobs/{id}` poll while a job it started is non-terminal. It does not open `/jobs/{id}/events` streams, which keeps one SSE connection per page (HTTP/1.1 tunnel limit).
- **B — request results.** A create request is answered `accepted` as soon as the agent drawer opens, then `done` (with `componentId`) or `failed` when the job ends. A delete request is answered `accepted` after the DELETE is sent (`cancelled` if the user closes the modal), then `done`/`failed`.
- **B — 409 handling.** On a `conflict` from DELETE the SPA re-reads `…/usage` and resets the acknowledgement. It does not parse `fields.pages`/`fields.codeReferences`, so Stream A may send `fields` in any shape.
- **B — repo ids.** The SPA accepts repo ids either raw (`apps/radustanciu`) or already encoded (`apps%2Fradustanciu`) and always sends exactly one level of encoding in the path.
- **B — `useApi`.** `ErrorCode` gains `forbidden`. 401/`unauthorized` messages get the sign-in hint appended. `$fetch` is called through a plain signature to fix a pre-existing `nuxt typecheck` failure (TS2321, excessive stack depth), which also occurred without any Stream B change.
- **A — registry file mode (needs a layers/content change).** `layers/content/scripts/registry-output.mjs:62` creates `component-definitions.{json,ts}` with mode `0600`. On hetzner1 the watcher runs as `cfdev`, so `cfcp` cannot read the manifest (verified: `-rw-------+ cfdev cfsrc`, ACL mask `---`). Delete/generate then fail with "Component registry metadata is unavailable." Required: write them `0640`/`0660`. Stream A does not work around it (no `CAP_DAC_READ_SEARCH`).
- **A — `cf-agent@` network.** `IPAddressDeny=100.64.0.0/10` from spec §6.2 is dropped: DNS on hetzner1 goes to Tailscale MagicDNS `100.100.100.100`, so it would break Codex. The nft guard (Phase 0) already blocks the tailnet for `cfagent` while allowing DNS. `IPAddressDeny=169.254.0.0/16` stays.
- **A — agent stdin and prompt.** `agent-runner` does not `execve`: it spawns codex, writes the prompt to stdin and closes it, then waits and exits with codex's status (Phase 0: codex blocks on an open stdin). The prompt adds one "non-interactive run" line that pre-answers the repo's "stop and ask" instructions.
- **A — no fixture/zero-token mode and no Go-only routes.** Only the routes in §2 exist; other Go routes return 404 `not_found`. Login is `couchfusion-server login-url` (no `/?token=` launch token).
- **A — site events carry the tailnet URL.** `SiteEvent.dev.url` is always the tailnet origin; `GET …/dev` and `bootstrap.devOrigins` are per caller (tunnel callers get `http://localhost|127.0.0.1:9181`).
- **A — `cf-dev@` restart policy.** The drop-in sets `Restart=no`: the control plane owns restarts and reports failures (Phase 0 unit had `Restart=on-failure`).
- **A — macOS socket paths.** When `run_dir` makes a socket path ≥ 100 bytes, the server binds by relative name inside `run_dir` (sun_path limit). Never triggers on hetzner1.
