# Phase 2 — workbench polish: shared contract (server ⇄ SPA ⇄ layers/content)

Date: 2026-10-30. Builds on Phase 1 (`2026-10-29-phase1-mvp-contract.md`, including its Deviations, which still hold) and the parent spec (`2026-10-29-rust-vps-control-plane-design.md` §6–§7, §11). Phase 1 results: `docs/implementation_results/2026-10-30-phase1-mvp-deploy.md`.

This file is the single source of truth for the three Phase 2 streams; deviations go at the bottom.

| Stream | Owns | Must not touch |
|---|---|---|
| A — Rust server | `cli-init/server/**` (+ `packaging/hetzner1/phase2/**`) | `web/`, `layers/`, Go code |
| B — SPA | `cli-init/web/**` | `server/`, `layers/`, Go code, `internal/web/dist` |
| C — Content layer | `layers/content/**` | `cli-init/**`, apps, other layers |

Nothing is committed by the streams. Nothing touches hetzner1 (read-only ssh for facts is fine).

## Scope
1. **Components drawer** (dev page): registry list, view source, rebuild registry, trash list + restore.
2. **Services** (read-only): status and recent logs for an allowlist of systemd units. No start/stop/deploy (Phase 3).
3. **Codex runs**: faster (no type-check/build/lint runs), token usage reported, and no Vite full-page reloads of the builder while a generate job runs.
4. **Per-caller dev URL in site events** (Phase 1 deviation "A — site events carry the tailnet URL" is removed): tunnel callers must never be handed the tailnet origin.

Deferred (not in Phase 2): second slot, cookie prefixing, idle reaper, preview-host routes, tasks run, `/processes`, git pull, Playwright ports.

## 1. API additions (Stream A implements, Stream B consumes)
Envelope, errors, guards, omit-not-null: as Phase 1.

```ts
interface ComponentSource { componentId: string; path: string; sha256: string; size: number; content: string; truncated: boolean }
interface TrashEntry {
  trashId: string            // the delete job id
  componentId: string
  source: string             // "app/components/content/NetworkHero.vue"
  sha256: string
  git: 'tracked_clean' | 'tracked_modified' | 'untracked' | 'unknown'
  deletedAt: string
  restorable: boolean        // false when a file now exists at `source`, or the trashed file is missing
  problem?: string           // why not restorable
}
interface ServiceSnapshot {
  unit: string               // "kangu.service"
  description: string
  kind: 'adopted' | 'dev' | 'system'
  activeState: string        // active | inactive | failed | activating | …
  subState: string
  mainPid?: number
  memoryCurrentBytes?: number
  since?: string             // ActiveEnterTimestamp, RFC3339
  user?: string
}
// JobSnapshot.result for component.generate gains: usage?: { inputTokens: number; cachedInputTokens: number; outputTokens: number }
// JobSnapshot.result for component.restore: { componentId, trashId }
// JobSnapshot.result for component-registry.rebuild: { components: number }
```

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/workspaces/{ws}/sites/{site}/components/{componentId}/source` | – | `ComponentSource` (256 KiB cap → `truncated`) |
| GET | `/workspaces/{ws}/sites/{site}/components/trash` | – | `TrashEntry[]`, newest first |
| POST | `/workspaces/{ws}/sites/{site}/components/trash/{trashId}/restore` | `{}` | 202 `JobSnapshot` `type:"component.restore"`; 409 `conflict` if not restorable or lease busy |
| POST | `/workspaces/{ws}/sites/{site}/components/registry/rebuild` | `{}` | 202 `JobSnapshot` `type:"component-registry.rebuild"` |
| GET | `/services` | – | `ServiceSnapshot[]` (allowlist from `server.toml`, see §3) |
| GET | `/services/{unit}/logs?lines=200` | – | `ActivityEvent[]` (oldest first; `lines` 1–1000, default 200; redacted) |

`bootstrap.capabilities` gains `"component-trash"`, `"component-source"`, `"registry-rebuild"`, `"services"`.

Site SSE (`event: site`) gains no new types: restore and rebuild publish `job` and `registry.changed` like delete. **`SiteEvent.dev.url` is rewritten per subscriber** to the origin that subscriber used (tailnet → `https://…ts.net:9181`, tunnel → `http://localhost:9181` or `http://127.0.0.1:9181` matching its Host).

## 2. Server behaviour (Stream A)
- **Source:** resolve exactly like delete (manifest checks, `O_NOFOLLOW` walk, only `app/components/content/**.vue`); read-only.
- **Trash list:** from the `trash` table + `meta.json`; `restorable=false` with `problem` when `source` exists again or the trashed file is gone.
- **Restore job:** per-site lease → re-check → copy trashed file back to `source` with `O_CREAT|O_EXCL` (never overwrite), fsync, mode 0664 group `cfsrc` (setgid dir) → wait for the manifest to list the id again (≤ 15 s) → emit `component.restored` + site `registry.changed {added:[id]}` → mark the trash row restored (keep the trashed copy; never unlink).
- **Registry rebuild job:** must **not** run repository code as `cfcp` (agent/dev-writable code would then run as the push-key holder). Run it as `cfdev` through a new oneshot template `cf-registry@.service` (`User=cfdev`, same sandbox as `cf-dev@` minus the network listener, `ExecStart=/opt/couchfusion/toolchain/bin/bun /srv/couchfusion/workspace/layers/content/scripts/generate-component-registry.mjs --app=%i`, `WorkingDirectory=/srv/couchfusion/workspace`, `TimeoutStartSec=120`). Extend the polkit rule to allow `start` of `cf-registry@*.service`. Job waits for the unit to finish (`systemctl start` blocks for oneshot), reads its result, then re-reads the manifest.
- **Services:** `systemctl show <unit> -p Id,Description,ActiveState,SubState,MainPID,MemoryCurrent,ActiveEnterTimestamp,User` per allowlisted unit; logs via `journalctl -u <unit> -n <lines> -o json --no-pager` (cfcp is in `systemd-journal`). Unknown unit → 404. Redact like dev logs (plus `Authorization`, `api_key`, `Set-Cookie`, `AuthSession=`).
- **Codex runs:**
  - Prompt adds: "Validation for this run: run only the registry generator (`bun run registry:build`). Do not run `nuxi typecheck`, `vue-tsc`, `nuxt build`, lint, or tests; the control plane validates the registry itself." Keep everything else.
  - Record `turn.completed.usage` into `result.usage` and a final job event `codex.usage`.
  - While a generate job runs, create the marker file `/run/couchfusion/dev/<app>.agent-running` (mode 0640 `cfcp:cfdev`, readable by the dev server) and remove it when the job ends (any outcome, including server restart recovery). The path is passed to the dev unit as `COUCHFUSION_AGENT_MARKER=/run/couchfusion/dev/<app>.agent-running` via the runtime env file.
- Packaging: `packaging/hetzner1/phase2/` with an idempotent step script installing `cf-registry@.service`, the updated polkit rule and any `server.toml` additions (keep an existing `server.toml`; print what to add if keys are missing).

## 3. Config (Stream A)
`server.toml` gains:
```toml
[services]
units = ["kangu.service", "clients-kangu.service", "clients-kangu-dev.service", "mindarchy.service", "caddy.service", "couchdb.service", "email-sender.service", "cf-dev@radustanciu.service", "couchfusion.service"]
```
Missing section → default to exactly that list.

## 4. SPA (Stream B)
- **Components drawer** on the dev page (top-bar button "Components"):
  - Tab "Registry": status pill (`ready|stale|missing|invalid`), count, "Rebuild registry" (if capability) → job; list of components (label, id, source, propCount); per row "Source" and "Delete" (reuses the existing delete modal).
  - Source viewer: read-only, monospace, line numbers, sha + size, truncated notice, "Copy path". No syntax-highlighting dependency (plain `<pre>`, optional minimal token colouring in CSS is fine).
  - Tab "Trash": entries newest first (component, source, deletedAt, git state); "Restore" when `restorable`, else show `problem`. Restore → job → on success bridge reload with `focusComponentId`.
- **Delete modal:** add a "View source" link opening the source viewer.
- **Agent drawer:** show token usage from `result.usage` after a run.
- **Services page** `/services` (sidebar entry when capability `services`): table of `ServiceSnapshot` (unit, description, state pill, since, memory, user), auto-refresh every 10 s; clicking a row opens recent logs (`/services/{unit}/logs`) in a log viewer with a refresh button.
- Use `dev.url` from `GET …/dev` (per caller); for site SSE `dev.status` events also trust `dev.url` now (server rewrites per subscriber), but never switch the iframe origin to a different scheme/host than the page was loaded through — if it differs, keep the current slot origin and log a console warning.
- Tests: vitest for the drawer state logic (restore eligibility, rebuild flow), services refresh, and the origin guard. `bun run test:run`, `bun run typecheck`, `bun run generate` green.

## 5. Content layer (Stream C)
- **Suppress Vite full reloads during agent runs (dev only, gated):** in the existing gated module (`utils/register-control-plane-routes.ts`) add a Vite plugin, only when `nuxt.options.dev` and `COUCHFUSION_AGENT_MARKER` is set, that — while the marker file exists — prevents full-page reloads and HMR caused by **create/delete/update of files under `<app>/app/components/content/`** and the generated `app/content-builder/component-definitions.{ts,json}`. Use Vite 7's `hotUpdate` hook (environment API; returning `[]` stops default handling) and verify that file **create/delete** are also stopped (Nuxt/Vite may trigger full reloads from `import.meta.glob` invalidation or Nuxt's component template regeneration — investigate and handle those paths too, e.g. by intercepting `server.ws.send` full-reload payloads while the marker exists). When the marker disappears, do nothing special: the control plane's bridge reload brings the builder up to date. Must be a no-op when the env var is absent (all other apps) and in production.
- Prove it with a test or a scripted local check (a dev server of a fixture or radustanciu on a free port is acceptable, **not** port 7833), documenting exactly which reload paths were observed and stopped.
- Docs: update `docs/couchfusion-control-plane-bridge.md` + an implementation_results entry.

## Deviations
(append here: stream, what, why)
- **B — origin guard aligns instead of ignoring.** When a dev URL (from `GET …/dev`, start/stop/restart responses, or a site `dev.status` event) has a different scheme or host than the control page, the SPA keeps the slot's port but moves it onto the page's own scheme and host (`alignSlotUrl`, with a console warning), rather than only keeping the previous URL. This also covers the first load, when there is no previous URL. It is a client-side safety net; Stream A's per-subscriber rewrite remains the source of truth.
- **B — delete from the Components drawer.** A delete started from the drawer (no builder request) opens the same confirm modal and keeps the Components drawer open while the job runs (it refreshes on success); builder-initiated deletes still switch to Activity.
- **B — rebuild reload.** A successful `component-registry.rebuild` sends a bridge reload (`reason: "registry.rebuilt"`) because the builder's registry is module-level.
- **B — Services placement.** `/services` is a page in the default layout with a sidebar entry "05 Services" shown only with the `services` capability; there is no link from the full-screen dev page.
- **C — interception at the HMR channel, not `hotUpdate`.** Observed reload sources are Vite's glob invalidation (`full-reload *`), Vite propagation of the registry `.ts` (pathless `full-reload`), `@vitejs/plugin-vue` (`custom file-changed`, sent directly via `server.ws.send`), `prune` and a Tailwind CSS `update`. Several bypass plugin `hotUpdate` hooks, so the guard wraps `server.ws.send` (in Vite 7 the same object as the client environment's `hot` channel) and drops these only while the marker exists. Pathless messages are attributed by a 2.5 s window opened by a guarded watcher event (listener prepended ahead of Vite's). An unrelated reload inside that window during an agent run is also dropped; the end-of-job bridge reload resyncs. Plugin added to the **client** Vite config via `vite:extendConfig`.
- **C — gating.** Enabled when `nuxt.options.dev && COUCHFUSION_AGENT_MARKER` (non-empty); independent of `COUCHFUSION_CONTROL_TOKEN`. Stream A must put `COUCHFUSION_AGENT_MARKER=/run/couchfusion/dev/<app>.agent-running` in the dev runtime env file and the file must be readable/stat-able by `cfdev` (existence check only; `/run/couchfusion/dev` must be traversable by `cfdev`).
- **A — `/run/couchfusion/dev` is `2710 cfcp:cfdev`** (was `0700 cfcp:cfcp`), so `cfdev` can stat the agent marker (answers C's requirement). setgid makes new files group `cfdev`; the dev env file stays `0600` (group bits off), so `cfdev` still cannot read the control token. Installed by `packaging/hetzner1/phase2/01-install.sh` (tmpfiles); the Phase 1 copy of the tmpfiles/polkit files was updated to match so re-running Phase 1 does not revert it.
- **A — registry fallback moved off `cfcp`.** Phase 1's "watcher late" fallback ran the layer generator as `cfcp`; it now uses the same `cf-registry@<app>` oneshot as "Rebuild registry". `SiteConfig.registry_unit` (optional, default `cf-registry@<id>.service`) is validated like the other units; `check_unit_name` and the polkit rule allow `cf-registry@`.
- **A — restore semantics.** `restorable` is false (with `problem`) when the entry was already restored, the trashed copy is missing, the recorded source is not under `app/components/content/`, or anything (file or symlink) exists at `source`. Restore re-verifies the trashed copy's sha256 against the delete record, creates the file with `O_CREAT|O_EXCL|O_NOFOLLOW` through an `O_NOFOLLOW` directory walk, mode `0664`, and marks the row restored (new `trash.restored` column, added by a tolerant `ALTER TABLE`). The trashed copy is kept. `componentId` comes from the trash `meta.json`, else kebab case of the file stem.
- **A — services.** `systemctl show <unit> -p Id,Description,ActiveState,SubState,MainPID,MemoryCurrent,ActiveEnterTimestamp,User --timestamp=unix` per unit (concurrently); `kind` is `dev` for `cf-*`, `system` for caddy/couchdb/couchfusion/tailscaled/polkit/ssh, else `adopted`. Logs: `journalctl -u <unit> -n <lines> -o json`, `time` from `__REALTIME_TIMESTAMP`, `stream` stderr for priority ≤ 3. Services always use `toolchain.systemctl/journalctl` (also with the process_group runner, which the smoke test uses with fakes). The redactor additionally masks `AuthSession=<value>`.
- **A — `lines` validation.** `?lines=` outside 1–1000 (or non-numeric) → 422 `validation`, not clamped.
- **A — marker lifecycle.** Created (`0640`) at the start of every generate job by an RAII guard, removed on every outcome; all sites' markers are deleted when the server starts (generate jobs from a previous process are failed on boot). `COUCHFUSION_AGENT_MARKER` is written into the dev runtime env on every dev (re)start, so an already running dev server picks it up only after a restart.
- **A — usage.** `result.usage` is set whenever `turn.completed` carried usage, including on failed jobs; the final job event is `codex.usage` ("Tokens: in N (M cached), out K.").
