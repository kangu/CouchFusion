# Phase 1 MVP — deployed to hetzner1 (2026-10-29/30)

Spec: `docs/superpowers/specs/2026-10-29-rust-vps-control-plane-design.md`. Contract: `docs/superpowers/specs/2026-10-29-phase1-mvp-contract.md` (with each stream's deviations). Install: `server/packaging/hetzner1/phase1/README.md`.

## What was built (three parallel streams against the contract)
- **A — Rust server** (`server/`): `couchfusion-server` 0.1.0, static x86_64 musl binary (6.9 MB). Tailnet auth via `tailscale serve` → unix sockets (`Tailscale-User-Login` allowlist), SSH-tunnel auth via `login-url` cookie, host/origin/mutation guards, SPA serving with nonce CSP, dev sessions through systemd + polkit, slot proxy 9181 → 4401 with WebSocket passthrough and boot page, site SSE stream, component usage/delete/generate jobs, git status/diff/commit. 45 unit tests, 51-check local smoke script.
- **B — SPA** (`web/`): full-screen dev page `/workspaces/hetzner1/dev/radustanciu` (top bar, git/activity/agent drawers, delete confirm modal, needs-restart banner), parent side of the postMessage bridge. 101 tests, typecheck clean.
- **C — Content layer** (`layers/content`): gated control routes (`ping`, `component-usage`), child bridge composable, "New component" + per-card delete in the component picker, dirty/reload handling, registry generator skips vanished files. Inert unless the server injects its env.

## Deployed on hetzner1
- `polkitd` installed (was missing; needed for `cfcp` to start/stop `cf-dev@`/`cf-agent@`). Rule allows only those units; self-test: `cfcp` cannot manage `ssh.service`.
- `couchfusion.service` active; `tailscale serve` 9180/9181 → `unix:/run/couchfusion/ts-91NN.sock`. Each repo's `.git` is owned by `cfcp` with ACL for `radu`.
- Release `0.1.0-fc69f2b-dirty202610300601` in `/opt/couchfusion/releases`, `current` symlink.

## Verified over the tailnet (from the laptop)
| Check | Result |
|---|---|
| Control UI at `https://hetzner1.tailb7471b.ts.net:9180/` | Loads; Tailscale identity accepted with no extra login; workspace `hetzner1` listed |
| Dev page | Top bar shows `READY`, memory, controls; iframe loads the slot (builder sign-in page). In the built-in browser pane the framed slot's subresources were `ERR_BLOCKED_BY_CLIENT` (pane restriction on a second origin in a frame); the slot itself serves assets with 200 and `frame-ancestors` for 9180 — to confirm in a normal browser. |
| Dev restart through the API | Works via polkit; dev re-adopted across a control-plane restart |
| Usage | `phil` used on `/` + 20 locale pages; `network-hero` on `/homepage-redesign-network-content`; page scan `ok` |
| Delete guards | Wrong sha → 409 "file changed"; in-use without acknowledgement → 409 |
| **Create (Codex)** | Job `component.generate` succeeded: `ZzSmokeBadge.vue` written by Codex under `cf-agent@` (sandboxed), registry updated, `builderFieldMeta` select for `tone`, `label` localized, prop-id markers, lint clean |
| **Delete** | Job `component.delete` succeeded: file moved to `/var/lib/couchfusion/trash/<job>/…`, registry dropped the id in 3 s, no dev restart |

## Fixes made during deploy
- `layers/content/scripts/registry-output.mjs`: registry files were written `0600`, unreadable by `cfcp` → now `0o664` (umask applies).
- Delete: `rename()` returned `EXDEV` because `ReadWritePaths=` bind-mounts workspace and trash separately → copy + fsync + `unlinkat` fallback that only unlinks if the name still points at the opened inode (2 new tests).
- Codex couldn't find `bun`: Codex uses a login shell and Debian `/etc/profile` resets PATH → `/var/lib/cfagent/.profile` re-adds the toolchain (installed by `03-units.sh`).
- Phase 1 scripts 04/05 now `cd /` (sudo -u radu/cfcp tripped on `/root` as cwd); `release.sh` strips macOS xattrs and stamps uncommitted builds `-dirtyYYYYMMDDHHMM`.

## Notes
- A Codex generate used ~418k input tokens for a tiny component (skill + repo reading); worth trimming the prompt/skill later.
- `ZzSmokeBadge.vue` is in trash (job `c4afa0c6015915e8`); the registry files are back to 14 components.

## In-browser verification (2026-10-30, dev-browser Chromium over the tailnet, signed in by the user)
Reported problem: after signing in, the dev page did not open the node editor.

Root causes and fixes:
1. **Builder's own preview iframe was blocked.** The node editor (on the slot origin `:9181`) embeds a live preview iframe of the same site. The slot proxy sent `frame-ancestors` with only the control-plane origins, and CSP checks every ancestor, so the nested preview was refused (`ERR_BLOCKED_BY_RESPONSE`). Fix: `frame-ancestors 'self' …` (`server/couchfusion-server/src/http.rs`).
2. The "Login successful. Redirecting…" pause was the first, slow Vite compile of the builder bundle on this box, not a hang.
3. Bridge noise: the child posted `ready` to every configured origin; Chrome warned for the mismatched ones. Fix: `narrowToParentOrigin()` uses `location.ancestorOrigins` (`layers/content/app/composables/useControlPlaneBridge.ts`, 2 new tests).

Also fixed: `layers/database/utils/couchdb.ts` logged the full `_session` response, including the `AuthSession` Set-Cookie (a live session token), on every login — now logs status only. Phase 0 `03-workspace.sh` no longer re-owns `.git` (it had undone Phase 1's `cfcp` ownership when re-run).

Verified in the UI:
| Flow | Result |
|---|---|
| Builder in the frame | Node editor + live preview render; top bar shows `bridge ●` |
| Picker | "New component" in the header; delete button on the 14 project components |
| Delete round trip (cancel) | Trash in the builder → confirm modal in the control plane (file, sha, git state, 1 using page, "Delete anyway" required) → "Keep component" → builder shows "cancelled" |
| Create from the drawer | Codex run succeeded (~205 s), `ZzUiSmoke.vue`; builder reloaded and opened the picker on the new card with its live thumbnail |
| Delete from the picker | No usage → moved to trash; registry back to 14 in 4.4 s; card gone after reload; no dev restart |

Follow-ups noticed:
- When Codex adds the `.vue`, Vite does its own full-page reload of the builder mid-run (3× here), before the bridge's controlled reload. With unsaved builder edits this would hit the unload guard; consider deferring Vite's full reload for `components/content` while an agent job runs, or warning in the drawer.
- Codex spent ~2.5 min running Nuxt's type checker; the prompt could ask for registry validation only.
- Pre-existing, unrelated: client warnings about `runtimeConfig.content`/`imagekit` access and the runtime-font 404s.

## Slowness over Tailscale (2026-10-30)
- Measured: Tailscale connects **direct** (no DERP) at ~46 ms, same as the public path. But the WireGuard UDP flow loses **37–45% of packets in both directions**, while ICMP/HTTPS to the same server's public IP, the local gateway and 1.1.1.1 lose 0%. hetzner1 reports no UDP errors → loss is in transit on the user's network/ISP path for UDP (laptop public IP 78.97.199.189; 10% loss to `indux-prod` too). With HTTP/2 multiplexing everything on one connection, each loss stalls all of Vite's ~370 module requests ("pending" in DevTools); single requests stalled 4–18 s.
- **SSH tunnel** (TCP over the clean public path) is the workaround: same 179 KB module 0.12–0.87 s, no stalls. `ssh -fN -L 9180:127.0.0.1:9180 -L 9181:127.0.0.1:9181 hetzner1` + `couchfusion-server login-url`.
- **Slot compression** added (`tower-http` gzip/br at fastest level; skips 101 upgrades, SSE, images, tiny bodies): Vite modules ~70% smaller (179 KB → ~52 KB). Tailnet transfers shorter, but stalls (one 11 s TTFB in 6 tries) remain until the UDP loss is fixed. Smoke suite 52/52 (new check: `slot compresses`).
- Root-cause fix is outside the server: test from another network (phone hotspot); check the router's UDP flood/DoS protection.

## Remaining for Phase 1 sign-off
- Your own check in your normal browser (same URL), then commit the work: `cli-init` (branch `ui`: `server/`, `web/` additions, docs), `layers` (content bridge, registry fixes, database log fix), radustanciu docs.
