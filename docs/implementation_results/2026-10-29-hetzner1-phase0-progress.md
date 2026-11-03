# hetzner1 Phase 0 — progress (2026-10-29)

Spec: `docs/superpowers/specs/2026-10-29-rust-vps-control-plane-design.md`. Scripts: `server/packaging/hetzner1/phase0/`.

## Done
| Step | Result |
|---|---|
| 12 data push (laptop) | `rs--content` 134 docs, `rs--logins` 0, `analytics_radustanciu` 5 copied to hetzner1 CouchDB (counts match; 854 revisions written for content); `_security` copied; `_config` `cf_env_radustanciu` 7 keys set. `analytics_radustanciu_raw_*` skipped. |
| 00 preflight | Ports 4401/9181/9180/4501 free. Note: `mindarchy-serve` listens publicly on `*:8087` (pre-existing, unchanged). |
| 01 users | `cfcp`, `cfdev` (uid 994), `cfagent`, group `cfsrc` (cfdev, cfagent, radu). |
| 02 toolchain | node 22.23.3, bun 1.4.2, codex-cli 0.144.4 in `/opt/couchfusion/toolchain` (checksums verified). Fix: npm needed node on PATH. |
| seed + 03 | ~77 MB source (radustanciu 36M, layers 39M, cli-content 1.8M) with `.git`. Fix: macOS `openrsync` lacks `--info`/`--human-readable`. |
| 04 deps | Installed as `cfdev`. bun 1.4.2 rewrote both lockfiles (laptop bun is 1.1.38). Fix: `nuxt prepare` needs `COUCHDB_ADMIN_AUTH` present → dummy placeholder for install only. **S6 pass** (better-sqlite3). |
| 05 units | `couchfusion.slice`, `cf-dev@.service`, `/etc/couchfusion/env/radustanciu.dev.env` template (0640 root:cfdev). Not started. |
| 06 nft | Table `inet couchfusion`. Self-test: cfdev blocked :2019/:9119/:10000, open :5984; cfagent blocked :2019/:5984; radu unchanged; DNS via MagicDNS (100.100.100.100) allowed and working. |
| 07 caddy | `/etc/caddy/sites/*.caddy` → 0640 root:caddy, `caddy validate` ok, no reload. |
| 08 slot | `tailscale serve --https=9181 → 127.0.0.1:4401` (tailnet only). |
| 09 codex | Device login as `cfagent` (ChatGPT). **S4 pass**: write inside the work dir ok, outside denied (`Read-only file system`). Events: `thread.started`, `turn.started`, `item.started`, `item.completed`, `turn.completed`. Codex prints "Reading additional input from stdin…" → the server must spawn it with stdin closed (`Stdio::null()`). |
| env | Dev env filled by the user (0640 root:cfdev). `COUCHDB_ADMIN_AUTH` verified: `radu`, server admin. `COUCHDB_COOKIE_SECRET` left empty (only needed for server-minted sessions such as magic links; password login works without it, the layer logs a warning). |
| 10 dev | `cf-dev@radustanciu` running on 127.0.0.1:4401. Startup: DBs initialised, ImageKit config loaded from `cf_env_radustanciu`, analytics engine scheduled. Nuxt answers **503 while Vite warms up**, then 200 (`/`, content page 200, `/k/` 302 → login, unknown 404). |
| S1 | **Pass.** Registry watcher adds/drops a component without a restart. On this box each regeneration takes **2.3–3.6 s** (laptop 0.3–0.4 s); a file event during a run queues one extra run, then it settles (no loop). Found: the generator **crashes with ENOENT if a `.vue` disappears mid-run** (it lists files, then reads them) — the next queued run recovers, but delete must wait for a clean "Completed" run, and the generator should skip vanished files (layers/content fix). |
| S5 | Dev unit memory ~585 MiB steady, **peak ~710 MiB**; box `MemAvailable` 1.2 GiB with dev running. |
| S2 | **Pass (connect).** `https://hetzner1.tailb7471b.ts.net:9181/` renders the full site from hetzner1 CouchDB; console shows `[vite] connected.` through `tailscale serve`, with `__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS`. A live edit round trip is still to be eyeballed. Two 404s on `/api/content/fonts/active/sans-{400,700}-normal.woff2` are pre-existing: radustanciu never applied runtime fonts (`content-settings:fonts` is absent locally too). |
| S3 | **Pass.** `tailscale serve --https=9199 unix:/run/…sock` works, and a client-sent `Tailscale-User-Login: forged@example.com` is **overwritten** with the real login (`r67rggkzzb@privaterelay.appleid.com`). Note: with a unix target, `Host` arrives as `localhost`; the real host is in `X-Forwarded-Host` (`hetzner1.tailb7471b.ts.net:9199`), so the Host allowlist must check `X-Forwarded-Host` on socket listeners. |

Live services checked after the changes: kangu, clients-kangu, clients-kangu-dev, mindarchy, caddy, couchdb all `active`; kangu.ro and clients.kangu.ro return 200.

## Findings for Phase 1 code
- Spawn Codex with stdin closed.
- Treat dev "ready" as HTTP 200 on `/` (not just an open port: 503 during warm-up).
- Delete acceptance on hetzner1: manifest updated within ~4 s (not 2 s), and only after a "Completed" registry run.
- Fix in `layers/content/scripts/generate-component-registry.mjs`: skip files that vanish between listing and reading.
- Identity: trust `Tailscale-User-*` only on the unix-socket listener; Host check uses `X-Forwarded-Host` there. Allowed login (D4): `r67rggkzzb@privaterelay.appleid.com`.

## Phase 0 exit criteria
- Site and builder route reachable over the tailnet (builder needs sign-in): **met**.
- Guard self-test: **met**. Codex logged in + sandbox: **met**. D1 answered: **met**.
