# CouchFusion control plane in Rust on hetzner1: design

- **Status:** final design spec, ready for planning. Nothing is implemented yet.
- **Date:** 2026-10-29
- **Replaces:** the Go `couchfusion ui` backend in `cli-init`. Go's non-UI CLI commands stay as they are.
- **Reuses:** the `cli-init/web` Nuxt 4 SPA and its `/api/v1` contract.
- **Target host:** hetzner1. Debian 13 x86_64, 2 vCPU, 3.7 GiB RAM (about 1.6 GiB available), 4 GiB swap, 23 GB free disk.
- **Pilot app:** `apps/radustanciu` (Nuxt 4.1.2, bun).
- **Sources:** this spec merges three drafts (mvp, secure, parity) and the judges' corrections. All repo facts below were verified read-only on 2026-10-29.

---

## 1. Summary

`couchfusion-server` is one statically linked Rust binary (musl). It runs on hetzner1 as `couchfusion.service` under a dedicated system user, `cfcp`. It does four things:

1. Serves the existing SPA and an `/api/v1` API. The API is a strict subset of the Go contract at first and grows into a superset.
2. Supervises dev servers and Codex runs. These run as **root-owned systemd template units** under unprivileged users, so they keep running when the control plane restarts.
3. Exposes each dev site on its own **slot origin** (`https://hetzner1.tailb7471b.ts.net:918N`). It reverse-proxies HTTP and the HMR WebSocket to `nuxt dev` on loopback. Nothing is proxied under a sub-path.
4. Reads and restarts production units through a narrow polkit rule. Later phases add build, release, switch and rollback.

Access is tailnet-only through `tailscale serve`, with Tailscale identity headers trusted only on a listener that local processes cannot reach. An SSH tunnel with a CLI-issued login link is the fallback.

**The MVP (Phase 1) delivers the three pilot asks on radustanciu:**

1. **A full-screen dev session:** the builder inside an iframe, inside the control-plane SPA.
2. **Delete a component from the builder's picker.** The parent SPA confirms, the server moves the `.vue` file to trash, waits for the registry watcher to update the manifest, and the frame reloads. The dev server does not restart.
3. **Create a component with Codex.** The server runs `codex exec --json`, sandboxed with no approvals. A new `.vue` file appears, the registry updates, and the frame reloads with the new component highlighted.

The MVP also ships git status, diff, commit and push per repository. The full Codex app-server port, with approvals and consents, comes later (Phase 4).

---

## 2. Decisions

### 2.1 Binding user decisions

| # | Decision |
|---|---|
| U1 | One Rust server runs **on hetzner1** under systemd and handles both dev processes and production units. |
| U2 | The UI is a web site talking to the Rust backend. It is private: Tailscale (`hetzner1.tailb7471b.ts.net`) is primary, an SSH tunnel is the fallback. Port 9119 is taken by Hermes. |
| U3 | The VPS becomes the **primary dev machine**, so git commit and push are needed in the UI. |
| U4 | Rust **replaces** the Go control-plane backend. The SPA and `/api/v1` are reused where sensible. |
| U5 | The coding agent is Codex. The Go code targets codex-cli 0.144.x, which is not yet installed on the VPS. |
| U6 | The pilot is `apps/radustanciu`, with the three asks in §1. |
| U7 | Repo rules: layers read env params from CouchDB `_config` section `cf_env_[slug]`. Content-component props with two or more discrete options need `builderFieldMeta` selects. Docs go in the relevant `docs/` folder. |
| U8 | (2026-10-29) radustanciu production **moves to hetzner1**. Today it runs on a different VPS and ships by committing `.output` into a dist git repo that the server pulls (`git_output.sh` → `git-persistent/radustanciu` → `git2.kangu.ro/radu/new-radustanciu-dist`). That flow is retired: the **source lives on hetzner1, is built there, and the production server runs on its own loopback port** behind Caddy. No git is needed to deploy. |
| U9 | (2026-10-29) The git remote for radustanciu comes **later** (D2 deferred). Until then the UI does local commits only and hides Push. |
| U10 | (2026-10-29) D3 accepted: Codex device login as `cfagent` with the user's ChatGPT account, pinned to 0.144.x. D4 accepted: only the user's tailnet login may use the control plane, and prod builds run on the VPS behind the memory governor. |

### 2.2 Ground truth that shapes the design

| Verified fact | Consequence |
|---|---|
| The root `.gitignore` ignores `apps` and `layers`. `apps/radustanciu`, `layers` (remote `cf` → `kangu/CouchFusion-BaseLayers`), `cli-content` and `cli-init` each have their own `.git`. **`apps/radustanciu` has no remote.** | Git is modelled **per repository**. A push needs a remote created first (decision D2). |
| The radustanciu `dev` script is `PORT=7833 nuxt dev --host`. A bare `--host` binds all interfaces. | The server never runs `bun run dev`. It runs `node node_modules/nuxt/bin/nuxt.mjs dev --host 127.0.0.1 --port <p>` itself. |
| `layers/content/utils/component-registry-watch.ts` spawns bare `bun` (`--app=<name>`), unless `CONTENT_REGISTRY_WATCH=0`. It infers the app name from `rootDir` being `apps/<name>`. | The dev unit's `PATH` must include bun, or the watcher fails silently. The layout must keep `apps/<name>`. |
| `registry:build` runs `../../cli-content/generate-component-registry.mjs`, which forwards to `layers/content/scripts/generate-component-registry.mjs`. | The workspace has `apps/`, `layers/` and `cli-content/` side by side. The fallback calls the layer script directly. |
| radustanciu extends 7 layers, including `database`, `auth` and `content`. `dbLoginPrefix: 'rs-'` is hard-coded. CouchDB comes from `COUCHDB_URL` (default `http://localhost:5984`, `couch-config.ts:4`) and `COUCHDB_ADMIN_AUTH`. | Dev would hit **hetzner1's production CouchDB** with an admin credential. §9.4 resolves this. `COUCHDB_URL` lets dev point elsewhere without code changes. |
| Picker: `layers/content/app/components/builder/ComponentPickerDialog.vue` has header controls at about L25-35 and a global-component trash button at about L294-310. The unsaved-changes `beforeunload` guard is in `InlineLiveEditor.vue:183`. `server/utils/global-component-usage.ts` scans `page-*` minimark bodies. | These are the bridge hook points and the usage scan to generalise. |
| hetzner1: `kangu.service` and `clients-kangu.service` run as **`User=radu` with no hardening**, are internet-facing, and run `/home/radu/.bun/bin/bun /var/www/<site>/dist/start.mjs`. Unauthenticated loopback services are listening: Caddy admin `:2019`, Chrome DevTools `:9223`/`:9224`, CouchDB `:5984`, Webmin `:10000`, Hermes `:9119`, email-sender `:8096`. | Loopback is **not** a trust boundary. A compromised production app is a local attacker, so identity-header trust, polkit grants and secrets must not depend on "everything local is radu". |
| `/etc/caddy/sites/kangu.ro.caddy` holds an inline API key. | Lock the file down (0640 root:caddy). The control plane never reads Caddy files. |
| The laptop has codex-cli 0.144.4 and cargo 1.97.1. The VPS has no node, bun, codex or cargo on root's PATH. bun is at `/home/radu/.bun/bin`; node is at `/opt/clients-node/bin` (version unknown). | Cross-compile on the laptop. Install a pinned toolchain under `/opt/couchfusion/toolchain`. |

### 2.3 Design decisions

| # | Decision | Rationale |
|---|---|---|
| A1 | Code lives in `cli-init/server/`, a Cargo workspace next to `cli-init/web/`. | The SPA is embedded at build time, and `cli-init` is already its own repo. |
| A2 | One binary crate with module boundaries that match the planned split. Split in Phase 4. | Speed. There is no second consumer yet. |
| A3 | Dev, agent and build workloads run as **root-owned template units** (`cf-dev@`, `cf-agent@`, `cf-build@`). polkit allows only `cfcp`, only those name patterns, and only start/stop/restart. **No transient units.** | Units survive control-plane redeploys. Transient system units can set `User=root`, so granting them would grant root. Per-user transient units cannot work across dedicated users. |
| A4 | Three new users: `cfcp` (control plane, holds the push keys), `cfdev` (`nuxt dev` and builds), `cfagent` (Codex). `cfdev`, `cfagent` and `radu` share group `cfsrc` on the work tree. `.git` directories are **not** writable by `cfsrc`. | Agent-written or npm code must not reach the push keys, control-plane state, prod restart or `.git/config`. |
| A5 | Each dev site gets a **per-port slot** that Rust proxies (`9181..9184`) behind `tailscale serve`. There is no sub-path proxying. | Nuxt dev's absolute `/_nuxt/` and HMR paths break under a prefix. Rust adds the auth, cookie stripping, Host rewrite and boot page. |
| A6 | Identity headers are trusted only on unix-socket listeners that `tailscale serve` targets (`/run/couchfusion/ts-91NN.sock`, mode 0600 `cfcp`). Loopback TCP `127.0.0.1:918N` exists for the SSH tunnel and accepts the cookie only. | Local processes, including prod apps running as radu, cannot forge `Tailscale-User-Login`. If spike S3 shows `tailscale serve` cannot target a unix socket, use the TCP fallback in §4.2. |
| A7 | The MVP uses `codex exec --json` (no approvals, workspace-write sandbox, network off, fails closed). The app-server JSON-RPC port is Phase 4. | The pilot needs "prompt → `.vue` → reload". The app-server port is the largest and riskiest piece of Go. |
| A8 | The component usage scan goes through a **token-gated, dev-only Nitro route** in `layers/content`. | It reuses the layer's DB connection and minimark walker, so Rust holds no CouchDB credentials. |
| A9 | Delete is a soft delete: a `rename()` into a trash folder, with a sha256 compare-and-swap. Nothing is ever unlinked. | Deletes stay reversible, and races with Codex or the editor are detected. |
| A10 | Builder ↔ parent communication uses a versioned postMessage protocol. The **child only requests**; every destructive action is confirmed in the parent's UI. | A compromised dev page can at most open a dialog. |
| A11 | Persistence is sqlite (`rusqlite`, bundled) in `/var/lib/couchfusion/control.db`. CouchDB `couchfusion-control` is not used. | Control-plane state must not live in a database that dev credentials can modify. Mac Codex threads cannot resume on the VPS anyway. |
| A12 | `GET /api/v1/bootstrap` becomes a capability switch. | The same SPA keeps working against Go on the laptop. |
| A13 | Use the git, systemctl and journalctl CLIs (argv only, never a shell). `zbus` is Phase 3+ if needed. | The behaviour then matches the shell exactly, which keeps the code small. |
| A14 | One `nuxt dev` per site. The preview host (`COUCHFUSION_PREVIEW=1` plus a token) is folded into the dev session. | Two dev servers sharing `.nuxt` is a known Go bug, and there is about 1.6 GiB free. |

---

## 3. Architecture

```
laptop browser (tailnet)                     laptop browser (ssh -L 9180..9184 → 127.0.0.1:9180..9184)
  https://…ts.net:9180 ─┐                       http://localhost:9180 ─┐
  https://…ts.net:9181 ─┤ tailscale serve        http://localhost:9181 ─┤
                        ▼ (TLS, identity hdrs)                         ▼ (cookie only)
        unix:/run/couchfusion/ts-9180.sock …           tcp 127.0.0.1:9180 … 9184
                        └──────────────┬──────────────────────────────┘
                                       ▼
                 couchfusion-server  (User=cfcp, couchfusion.service)
                 ├─ :9180  SPA + /api/v1 + SSE
                 ├─ :9181-9184  slot proxies ──HTTP/WS──▶ 127.0.0.1:4401-4404  nuxt dev
                 ├─ systemctl start/stop cf-dev@<app> / cf-agent@<app> / cf-build@<app>  (polkit)
                 ├─ systemctl restart kangu.service … (polkit allowlist)
                 ├─ journalctl -o json -f (group systemd-journal) → SSE
                 ├─ git CLI per repo (push keys via LoadCredential)
                 ├─ /run/couchfusion/admin.sock  (login-url, doctor; 0600 cfcp)
                 └─ sqlite /var/lib/couchfusion/control.db

cf-dev@radustanciu   User=cfdev    node nuxt.mjs dev --host 127.0.0.1 --port 4401   (couchfusion.slice)
cf-agent@radustanciu User=cfagent  codex exec --json … → /var/lib/couchfusion/agent-out/radustanciu.jsonl
cf-build@<app>       User=cfdev    bun install --frozen-lockfile && nuxt build in a git worktree (Phase 3)
couchdb-dev.service  127.0.0.1:5985   dev-only CouchDB (recommended, D1)
```

**Workspace layout on the VPS**, owner `radu:cfsrc`, setgid directories, umask 002:

```
/srv/couchfusion/workspace/
  apps/radustanciu/   (own repo; .git owned cfcp, ACL u:radu:rwX, no cfsrc write)
  layers/             (clone of CouchFusion-BaseLayers; same .git rule)
  cli-content/        (clone)
```

The existing `/home/radu/layers` copy and the clients.kangu.ro trees are left alone.

**Crate layout** (`cli-init/server/`):

```
Cargo.toml                       # [workspace] members=["couchfusion-server"], resolver="3"
couchfusion-server/src/
  main.rs                        # clap: serve | login-url | doctor | fixture | check
  config.rs                      # /etc/couchfusion/server.toml
  http/{mod,auth,guards,sse,spa,errors}.rs
  proxy/{slot,ws,bootpage}.rs
  api/{bootstrap,workspaces,sites,dev,components,jobs,git,services,system}.rs
  dto.rs                         # serde mirrors of web/types/api.ts, Go time formats
  units/{mod,systemd,process_group,journal}.rs   # trait UnitRunner
  workspace/{inspect,manifest,components,usage,paths}.rs
  agent/{exec_runner,prompt,lint}.rs + SKILL.md (include_str!)
  jobs/{manager,lease,governor,redact}.rs
  git.rs  store.rs  audit.rs
tests/  fixtures/codex-exec/*.jsonl  packaging/{units,polkit,nft,tmpfiles}/  release.sh
```

The Phase 4 split is into `cf-dto`, `cf-units`, `cf-agent-appserver`, `cf-testkit` and `couchfusion-server`.

**Crates.** Pin exact versions in `Cargo.lock`.

| Area | Crates |
|---|---|
| HTTP and runtime | `axum 0.8.x`, `axum-extra 0.10` (cookie), `tower 0.5`, `tower-http 0.6` (trace, limit, set-header), `tokio 1.4x+` (full), `tokio-util 0.7` (`LinesCodec` with max length, `CancellationToken`), `hyper 1`, `hyper-util 0.1` (client-legacy, for the proxy) |
| Serialization | `serde 1`, `serde_json 1`, `toml 0.9` |
| Storage | `rusqlite 0.3x` (`bundled`) behind `spawn_blocking` |
| Static assets | `rust-embed 8` (feature `embed-spa`) |
| Security primitives | `sha2 0.10`, `subtle 2.6`, `rand 0.9`, `base64 0.22` |
| Other | `reqwest 0.12` (rustls, no OpenSSL), `notify 8`, `time 0.3`, `regex 1`, `nix 0.30` (killpg, `O_NOFOLLOW`), `clap 4.5`, `thiserror 2`, `anyhow 1`, `sd-notify 0.4` |
| Logging | `tracing 0.1`, `tracing-subscriber 0.3`, `tracing-journald 0.3` |
| Tests | `insta 1`, `tempfile 3`, `wiremock 0.6` |

---

## 4. Access and auth

### 4.1 Listeners and ports

- **Ports.** 9180 is the control plane and 9181–9184 are dev slots. Internal dev servers use 4401–4404 on 127.0.0.1. None of these appear in the list of ports in use (22, 80, 443, 556, 2004, 2019, 4310, 4311, 4369, 5984, 8087, 8096, 8443, 9119, 9223, 9224, 10000, 33287).
- **Tailnet.** For each port P, `couchfusion-server` listens on `unix:/run/couchfusion/ts-P.sock` (0600 `cfcp`; tailscaled runs as root). Root runs this once: `tailscale serve --bg --https=P unix:/run/couchfusion/ts-P.sock`.
- **Tunnel.** `tcp 127.0.0.1:P`. The user runs `ssh -L 9180:127.0.0.1:9180 -L 9181:127.0.0.1:9181 hetzner1`. The port numbers match on both ends, so slot URLs can be derived from `location`.
- **Admin socket.** `unix:/run/couchfusion/admin.sock` (0600 `cfcp`) serves `login-url` and `doctor`.

### 4.2 Authentication

1. **Tailnet (unix listener).** The request is authenticated when `Tailscale-User-Login` is in `auth.allowed_logins`. No cookie is set on this path, so nothing can leak to the dev slots.
   - A missing header (for example from a tagged device) returns 401 with an explanation.
   - Spike S3 must confirm two things: that `tailscale serve` supports `unix:` targets, and that it overwrites client-supplied `Tailscale-User-*` headers.
   - **Fallback if S3 fails:** `tailscale serve` targets `127.0.0.1:919N`, a separate TCP set used only for header trust. An nft rule `tcp dport 9190-9194 meta skuid != 0 reject` makes only root (tailscaled) able to connect. `doctor` self-tests the rule, and the server refuses header auth when the self-test fails.
2. **Tunnel (TCP listener).** Identity headers are stripped and ignored.
   - Run `ssh hetzner1 sudo -u cfcp couchfusion-server login-url`. It prints `http://localhost:9180/auth/exchange?token=<32B b64url>`, which is single-use, valid for 10 minutes, and stored hashed.
   - The exchange sets cookie `cf_cp_s` (`HttpOnly; SameSite=Strict; Path=/`; `Secure` is omitted because the tunnel uses http), then answers 303 → `/`.
   - Sessions are stored as SHA-256 hashes in sqlite, with a 7-day idle and 30-day absolute expiry, so they survive restarts. Any number of browsers can log in.
   - The SPA's `useApi` gets a small change: on 401 it shows "Run `couchfusion-server login-url` on the VPS".
3. **The fixture mode** keeps the deterministic all-zero launch token `AAAA…` on `/?token=`, so `control-plane.spec.ts` runs unchanged.

### 4.3 Request guards (every listener)

- **Host allowlist:** `hetzner1.tailb7471b.ts.net:{9180..9184}`, `localhost:{9180..9184}` and `127.0.0.1:{9180..9184}`. Anything else gets 403, which blocks DNS rebinding.
- **Mutations** (POST, PUT, PATCH, DELETE):
  - `Sec-Fetch-Site` must be absent, `same-origin` or `none`. **`same-site` is rejected as well as `cross-site`**, because a dev slot on `:9181` is same-site with `:9180` and SameSite=Strict does not separate them.
  - `Origin`, when present, must equal the request's own origin. The scheme comes from the listener kind: https on the tailnet, http on the tunnel.
  - The body must be `application/json`, at most 1 MiB, and `{}` is accepted, including on DELETE.
  - All API responses carry `Cache-Control: no-store`.
- **No CORS headers ever.** Slot listeners never route `/api/v1`.
- **SPA CSP:**
  ```
  default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none';
  frame-src https://hetzner1.tailb7471b.ts.net:9181 … :9184 http://localhost:9181 … :9184;
  connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'nonce-…'
  ```
  CSP cannot express a port range, so each slot is listed. The nonce is injected into `<script` tags exactly as in Go.
- **Step-up for production-affecting actions** (push, prod start/stop/restart, deploy, rollback): the body must carry `confirm: "<branch-or-unit>"`. Tunnel sessions older than 12 h get 401 `reauth_required`.
- **Audit:** every mutation is written to the `audit` table (login, via, method, path, target, outcome) and to journald.

---

## 5. Dev sessions

### 5.1 Process model

The `UnitRunner` trait has two implementations:

- **`systemd`**, used on hetzner1. `systemctl start|stop|restart`, `systemctl show -p ActiveState,SubState,MainPID,MemoryCurrent,Result,ExecMainStatus,ActiveEnterTimestamp`, `systemctl list-units 'cf-*' --output=json`, and `journalctl -u <unit> -o json -f --after-cursor`.
- **`process_group`**, used on macOS and in CI. tokio `Command` with `process_group(0)`; stop is `killpg(SIGINT)`, then SIGKILL after 5 s. Output is drained in chunks and long lines are truncated, which fixes the Go 64 KiB scanner stall.

**`/etc/systemd/system/cf-dev@.service`** (root-owned):

```ini
[Unit]
Description=CouchFusion nuxt dev (%i)
After=network.target couchdb-dev.service
[Service]
Type=simple
User=cfdev
Group=cfdev
SupplementaryGroups=cfsrc
Slice=couchfusion.slice
UMask=0002
WorkingDirectory=/srv/couchfusion/workspace/apps/%i
EnvironmentFile=/etc/couchfusion/env/%i.dev.env     # root:cfdev 0640: COUCHDB_URL=http://127.0.0.1:5985, COUCHDB_ADMIN_AUTH (dev), ImageKit test keys
EnvironmentFile=/run/couchfusion/dev/%i.env         # cfcp-written: CF_DEV_PORT, tokens, origins (see below)
Environment=PATH=/opt/couchfusion/toolchain/node/bin:/opt/couchfusion/toolchain/bun/bin:/usr/bin:/bin
Environment=HOME=/var/cache/couchfusion/dev/%i NUXT_TELEMETRY_DISABLED=1 NODE_OPTIONS=--max-old-space-size=1024
ExecStart=/opt/couchfusion/toolchain/node/bin/node node_modules/nuxt/bin/nuxt.mjs dev --host 127.0.0.1 --port ${CF_DEV_PORT}
KillMode=control-group
KillSignal=SIGINT
TimeoutStopSec=10
Restart=no
MemoryHigh=900M
MemoryMax=1400M
MemorySwapMax=768M
CPUWeight=80
TasksMax=256
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectHome=yes
ProtectSystem=strict
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
CapabilityBoundingSet=
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
SocketBindAllow=tcp:4401-4404
SocketBindDeny=any
IPAddressDeny=169.254.0.0/16 100.64.0.0/10
ReadWritePaths=/srv/couchfusion/workspace/apps/%i /srv/couchfusion/workspace/layers /var/cache/couchfusion/dev/%i
InaccessiblePaths=-/srv/couchfusion/workspace/apps/%i/.git -/srv/couchfusion/workspace/layers/.git /etc/couchfusion/credentials /var/lib/couchfusion /etc/caddy /var/www
```

Notes on the unit:
- `${CF_DEV_PORT}` appears in **arguments only**; systemd does not expand variables in the executable path.
- Write access covers the whole app (Nuxt writes `.nuxt`, `node_modules/.cache` and `app/content-builder/`) and `layers` (layer `.nuxt` artefacts). Spike S1 confirms the minimal set. `ProtectSystem=strict` may be relaxed if `nuxi` needs more.
- `MemoryDenyWriteExecute` is deliberately absent, because the V8 JIT needs W+X pages.

`/run/couchfusion/dev/<app>.env` (written by `cfcp`, mode 0640 `cfcp:cfdev`) contains:

```
CF_DEV_PORT=4401
NUXT_HOST=127.0.0.1
NUXT_PORT=4401
PORT=4401
COUCHFUSION_PREVIEW=1
COUCHFUSION_PREVIEW_TOKEN=<32B b64url, per start>
COUCHFUSION_CONTROL_TOKEN=<32B b64url, per start>
COUCHFUSION_PUBLIC_ORIGIN=https://hetzner1.tailb7471b.ts.net:9181
NUXT_PUBLIC_CONTENT_CONTROL_PLANE_ORIGINS=https://hetzner1.tailb7471b.ts.net:9180,http://localhost:9180
```

`couchfusion.service` sets `RuntimeDirectory=couchfusion` with `RuntimeDirectoryPreserve=yes`, so these files survive control-plane restarts. tmpfiles.d creates `/run/couchfusion/{dev,agent}` with mode 0750.

**Readiness:**
- Poll every 250 ms for up to 120 s.
- First choice: `GET http://127.0.0.1:4401/__couchfusion/control/ping` with header `x-couchfusion-control-token`, expecting `200 {"ready":true}`.
- Fallback: any non-5xx response on `/`.
- No URL scraping.

**Logs:** journal lines are mapped to `ActivityEvent{id,time,stream,code,text}`, where `stream` comes from `PRIORITY` (≤3 → `stderr`). Lines are redacted (the Go rules plus exact token values) before they are buffered. Bursts are coalesced to at most 4 SSE frames per second per stream, because the SPA re-GETs the snapshot on every event.

**Reconciliation on boot:**
- sqlite `dev_sessions` holds site, unit, slot, internal port, token hashes and the journal cursor. It is compared against `list-units 'cf-*'`.
- An active unit is re-adopted, with logs replayed from its cursor.
- A row whose unit has gone is marked `stopped`, or `failed` with the unit's `Result` (for example `oom-kill`).
- Any job whose work ran in-process is marked `failed`, `interrupted`.

**Restart** keeps the same session and slot. `ProcessSnapshot` for `script:dev` maps onto the session; the process id is stable across restarts, which avoids Go's stale-preview bug.

### 5.2 Reachability (slot proxy)

`proxy/slot.rs` runs one axum service per slot. The route table maps slot → `{site, upstream}`. For each request it:

1. **Authenticates** with the same rules as §4.2 (header on the unix socket, cookie on TCP). Unauthenticated requests get 401 HTML with a login hint.
2. **Strips** `cf_cp_s` from `Cookie` and removes every `Tailscale-User-*` header before forwarding.
3. **Rewrites** `Host` to `127.0.0.1:4401`, which sidesteps Vite's `allowedHosts`. It sets `X-Forwarded-{Proto,Host,Port}` and keeps `Origin`.
4. **Passes WebSockets through.** On `Upgrade: websocket` it calls `hyper::upgrade::on` for both sides, then `tokio::io::copy_bidirectional`. Idle timeout is 300 s.
5. **Adjusts responses.** It streams bodies without buffering, removes `X-Frame-Options`, and sets `Content-Security-Policy: frame-ancestors <control origins>`. If upstream sent a CSP, the `frame-ancestors` directive is appended to it.
6. **Serves a boot page** when the upstream is down or starting: a small HTML page that polls `/__cf/slot-status` (served by the proxy) and reloads itself when the session is `ready`.

**HMR.** The Phase 0 spike S2 runs `tailscale serve` straight to `nuxt dev` before any proxy code exists, to learn what Vite needs. Any override goes in a **gated** `layers/content` module. When `COUCHFUSION_PUBLIC_ORIGIN` is set (dev only), it sets `vite.server.hmr = {protocol:'wss'|'ws', host, clientPort}` and `vite.server.allowedHosts=[host]`. Otherwise it does nothing.

For the tunnel, the browser's own `location` already gives the right host and port, so the override is only applied when needed. If HMR still misbehaves, the bridge's explicit reload (§5.4) covers the pilot asks.

**URLs per caller.** `ProcessSnapshot.urls[0]`, `PreviewView.url` and `DevSessionView.url` are rewritten on every request to the origin the caller used: `https://…ts.net:9181` on the tailnet, `http://localhost:9181` through the tunnel. The existing SPA link and iframe code then works unchanged. `useOnboarding.validatedDevURL` is extended to accept the slot origins listed in `bootstrap.devOrigins`.

**Cookie isolation between slots.** The MVP runs one slot, so there is no collision. Phase 2 adds per-slot cookie-name prefixing (`cf918N_`, `Domain=` dropped) in the proxy.

### 5.3 Full-screen embed (SPA)

The new page is `pages/workspaces/[workspaceId]/dev/[siteId].vue`:
- a full-viewport `<iframe src="{slotOrigin}{route ?? '/builder/'}">`;
- a 36 px top bar with a status pill (site SSE), Start/Stop/Restart, Reload frame, Open in new tab, a git badge (dirty count, opens the git drawer), a jobs drawer and a logs drawer;
- an agent drawer for creating components, which overlays the iframe.

`useBuilderBridge.ts` implements the parent side of the protocol. The bridge also works when the dev site is opened in its own tab, through `window.opener`.

### 5.4 postMessage protocol (v1)

Every message has the envelope `{cf:1, type, nonce, id, replyTo?, payload}`. Types are prefixed `cf:host.` so they cannot collide with the existing `InlineLiveEditor` and `useContentLiveUpdates` postMessage traffic.

| Direction | Type | Payload |
|---|---|---|
| C→P | `cf:host.ready` | `{app, route, builder:boolean, dirty:boolean}` |
| P→C | `cf:host.welcome` | `{nonce, capabilities:["component.delete","component.create","reload"]}` |
| C→P | `cf:host.dirty` | `{dirty}` |
| C→P | `cf:host.component.delete.request` | `{componentId}` |
| C→P | `cf:host.component.create.request` | `{promptHint?: string (≤2 KB)}` |
| P→C | `cf:host.request.result` | `{replyTo, status:"accepted"\|"cancelled"\|"failed"\|"done", message?, componentId?}` |
| P→C | `cf:host.reload.request` | `{reason, focusComponentId?}` |
| C→P | `cf:host.reload.ack` | `{reloading:boolean, reason?:"unsaved"}` |
| P→C | `cf:host.component.focus` | `{componentId}`, sent after the `ready` that follows a create-triggered reload |

Validation rules:
- **Child:** active only if `import.meta.dev`, the `origins` list is non-empty, and the page is framed or has a `window.opener`. It accepts a message only when `event.origin` is in `origins`, and pins that origin after `welcome`. Until `welcome` arrives, `connected` stays false and no buttons render.
- **Parent:** requires `event.source === iframe.contentWindow` (or the opened window), `event.origin === slotOrigin`, and the nonce from `welcome`. It allows at most 5 messages per second.
- **Requests are never authority.** A delete request opens the parent's own confirmation modal, showing path, sha, git state and usages. A create request opens the agent drawer with the prompt prefilled.
- **Reload:** if the child is clean, it calls `location.reload()`. If dirty, it shows its own confirm and answers `reloading:false, reason:'unsaved'`, and the parent shows "Reload when ready". If no `ready` arrives within 3 s of a reload, the parent reassigns `iframe.src`.

### 5.5 `layers/content` edits (gated; other apps unaffected)

| File | Change |
|---|---|
| `nuxt.config.ts` | Adds `runtimeConfig.public.content.controlPlane.origins` (default `''`, filled from `NUXT_PUBLIC_CONTENT_CONTROL_PLANE_ORIGINS`). Registers the Nitro routes `server/routes/__couchfusion/control/**` **only when** `COUCHFUSION_CONTROL_TOKEN` is set in dev, following the `register-component-preview-pages.ts` pattern. Adds the gated Vite HMR override (§5.2). |
| `app/composables/useControlPlaneBridge.ts` (new) | The child side of §5.4. Exposes `connected`, `request()` (with a 120 s timeout) and `onReloadRequest()`. |
| `app/components/builder/ComponentPickerDialog.vue` | Header (about L25-35): a "New component" button, `v-if="bridge.connected"`. Each card (about L294-310): a trash button reusing `.delete-global-component-btn` styles, `v-if="bridge.connected && !isGlobalComponentDefinition(c) && !isPrimitiveComponent(c) && c.source?.startsWith('app/components/content/')"`. |
| `InlineLiveEditor.vue` (guard at L183) and `pages/builder/[[...target]].vue` | Report `dirty` and register `onReloadRequest`. |
| `server/utils/component-usage.ts` (new) | Generalises `global-component-usage.ts` to `findComponentPageUsage(id)`. It shares the `entryContainsAlias` walker over `page-*` `body.value` minimark tuples and walks localized bodies the way `i18n-body.ts` does. The global path delegates to it. |
| `server/routes/__couchfusion/control/ping.get.ts` and `component-usage.get.ts` | Require the `x-couchfusion-control-token` header (compared in constant time), otherwise 404. Usage returns `{pages:[{id,path,title}], scanned:number}`. |

On the `_config` rule (U7): the `COUCHFUSION_*` and `NUXT_PUBLIC_CONTENT_CONTROL_PLANE_ORIGINS` values are per-process supervisor injections, like `COUCHFUSION_PREVIEW*`. They are not app environment parameters, so `cf_env_[slug]` does not apply. This reasoning is recorded in `layers/content/docs/`. Layer app parameters on the VPS still come from `_config`, specifically `cf_env_radustanciu` on the dev CouchDB.

---

## 6. Components

### 6.1 Delete flow

**`GET …/sites/{site}/components/{componentId}/usage`** returns:

```json
{"data":{"componentId":"network-hero","source":"app/components/content/NetworkHero.vue",
 "sha256":"…","git":"tracked_clean|tracked_modified|untracked|unknown",
 "pages":[{"id":"page-en-/","path":"/","title":"Home"}],
 "codeReferences":[{"path":"app/components/content/Landing.vue","line":12}],
 "pageScan":"ok|unavailable"}}
```

- `pages` comes from the control route, called over loopback with the token.
- `codeReferences` comes from a Rust scan of `app/**/*.{vue,ts}` for `<PascalName`, `<kebab-name`, `resolveComponent('…')` and imports of the source path.
- If the dev unit is down, `pageScan` is `"unavailable"` and acknowledgement is required.

**`DELETE …/components/{componentId}`** takes `{"expectedSha256":"…","acknowledgeUsage":false}` and returns 202 `JobSnapshot` with `type:"component.delete"`. The job runs these steps, emitting an event for each:

1. **Lease.** Take the per-site lease, which is shared with generate, registry rebuild and commits on that app's repo. A conflict returns 409 `conflict` with `fields.activeJobId`.
2. **Resolve.** Check the id with `validResourceID`. Look it up in the manifest using the Go checks: version 1, the `typescriptSha256` link to `.ts`, sorted ids. Then walk the path from the app directory with `openat(O_NOFOLLOW)`. It must be a regular `.vue` file under `app/components/content/` with no symlink anywhere in the path. Layer and global components get 422 `validation`.
3. **Compare-and-swap.** Re-hash the file. A mismatch with `expectedSha256` returns 409 `conflict` ("file changed since you looked").
4. **Usage gate.** Re-run the usage check. If there are pages or code references, or `pageScan` is unavailable, and `acknowledgeUsage` is false, return 409 `conflict` with `fields.pages` and `fields.codeReferences`. The modal states that pages will render the raw tag (the renderer's fallback) and that Restore is available.
5. **Soft delete.** `rename()` the file to `/var/lib/couchfusion/trash/<jobId>/<relpath>` on the same filesystem (`/srv` and `/var` both live on the root fs; `doctor` checks this, with copy+fsync+unlink as the fallback). Record `meta.json` and a `trash` row with the git state. Nothing is unlinked; trash is kept for 30 days and pruned manually only.
6. **Wait for the registry.** Use `notify` on `app/content-builder/component-definitions.{json,ts}` plus a 100 ms poll, for up to 5 s, until the manifest is valid and no longer lists the id. On timeout, run `cf-registry` mode: `cf-build@<app>` with `CF_BUILD_MODE=registry`, which runs `bun layers/content/scripts/generate-component-registry.mjs --app=<app>` from the workspace root. Then re-check. If that fails too, the job fails with `filesystem`, the file stays in trash, and the UI offers Restore or Rebuild registry.
7. **Publish.** Emit `component.deleted` with result `{componentId, trashId}`, and send site events `registry.changed {removed:[id]}`. The parent sends `cf:host.reload.request`.
8. **Health check.** Read the unit's journal for 3 s. On a Nitro or Vite fatal error, or if the unit exited, restart once and report it.

### 6.2 Create flow (MVP: `codex exec`)

**`POST …/sites/{site}/components/generate`** takes `{"prompt":"…"}` (1–16384 bytes after trim) and a **required** `Idempotency-Key` header (1–128 characters, persisted for 10 minutes). It returns 202 `JobSnapshot` with `type:"component.generate"`.

1. **Lease, queue, baseline.** Take the per-site lease, then wait in the governor queue (§9.3). Record the baseline: `git -C <app> status --porcelain=v2 -z --untracked-files=all -- .` run by `cfcp` with the hardening in §8, plus SHA-256 of `app/**`, `nuxt.config.*`, `package.json` and lockfiles. Running git inside the app's own repo fixes Go's monorepo-prefix `baselineDirty` bug.
2. **Write the run spec.** `/run/couchfusion/agent/<app>.json` holds `{jobId, prompt, threadId?}`. Truncate `/var/lib/couchfusion/agent-out/<app>.jsonl`, which `cfcp` creates and owns, before starting. PID 1 opens it for append, so `cfagent` writes only through its stdout file descriptor.
3. **Start `cf-agent@<app>`.** Its `ExecStart` is `couchfusion-server agent-runner`, a small exec-only mode of the same binary. It re-validates the spec and then `execve`s:
   ```
   /opt/couchfusion/toolchain/codex/bin/codex exec --json --skip-git-repo-check \
     -C /srv/couchfusion/workspace/apps/<app> --sandbox workspace-write \
     -c approval_policy="never" -c sandbox_workspace_write.network_access=false \
     --output-last-message /var/lib/cf-agent/runs/<app>/last.md  [resume <threadId>]  -
   ```
   The prompt goes in on stdin. It consists of the developer instructions (ported verbatim from `prompt.go`, plus one VPS line: "The dev server is managed by CouchFusion; never start `nuxt dev` or `bun run dev`") and the embedded `SKILL.md` text, followed by the user prompt.

   `--skip-git-repo-check` is required because `.git` is inaccessible to the agent.

   Unit `cf-agent@.service`:
   - `User=cfagent`, `SupplementaryGroups=cfsrc`, `Slice=couchfusion.slice`, `UMask=0002`.
   - `Environment=CODEX_HOME=/var/lib/cf-agent/.codex`.
   - `ReadWritePaths=/srv/couchfusion/workspace/apps/%i /var/lib/cf-agent`.
   - `InaccessiblePaths=-/srv/couchfusion/workspace/apps/%i/.git /etc/couchfusion /var/lib/couchfusion /etc/caddy /var/www /home`.
   - `IPAddressDeny=169.254.0.0/16 100.64.0.0/10`, plus the nft loopback guard (§9.2).
   - `MemoryMax=1G`, `CPUQuota=150%`, `RuntimeMaxSec=1800`, `KillSignal=SIGINT`.
   - `StandardOutput=append:/var/lib/couchfusion/agent-out/%i.jsonl`.
   - No dev secrets.
   - `RestrictNamespaces` is not set, because the Codex Linux sandbox may need user namespaces (spike S4).
4. **Stream events.** Tail the JSONL by byte offset (persisted, so tailing survives restarts) with a bounded `mpsc` and awaited sends.

   | Codex event | Job event |
   |---|---|
   | `thread.started` | store `threadId` |
   | `item.*` with `agent_message` | `info` / `codex.message` |
   | `reasoning` | `debug` / `codex.reasoning`, summary only |
   | `command_execution` | `codex.command`, using the Go label rules ("Running a project command"); the raw command is never shown |
   | `file_change` | `codex.files` |
   | `turn.failed` / `error` | `error` |

   Unknown types are ignored.
5. **Check the result**, in a separate task, never on the reader:
   - **Fail closed:** if the run reports that the sandbox could not be applied, the job fails, with no retry and no fallback to `danger-full-access`.
   - Diff against the baseline. A change to `package.json` or a lockfile is a **failed policy check**. Nothing is reverted automatically; the UI shows the diff and the user decides.
   - At least one new `app/components/content/*.vue` is required. `generatedComponentId` is its kebab-case name.
   - Wait for the registry as in §6.1 step 6 (15 s), then confirm the id is in the manifest.
   - **`builderFieldMeta` lint:** if a prop typed as a union of two or more string literals has no `builderFieldMeta` entry with `type:'select'` and `options` (in a non-setup `<script lang="ts">` block), add a warning event and set `result.lint[]`. This enforces U7.
   - **`needsRestart`:** if changed files touch `nuxt.config.*`, `app/pages/**`, `package.json` or `layers/**`, set `DevSessionView.needsRestart={reason}`.
6. **Finish.** The result is `{componentId, changedFiles[], threadId, lint[], needsRestart?}` and the event is `component.generated`. The parent sends `cf:host.reload.request {focusComponentId}`, then `cf:host.component.focus`, and shows a "Commit…" shortcut.

**Follow-up:** `POST …/components/generate/{jobId}/followup` takes `{prompt}` and an `Idempotency-Key`. It starts a new job with `parentJobId`, running `exec resume <threadId>`.

**Cancel:** `POST /jobs/{id}/cancel` runs `systemctl stop cf-agent@<app>`, which sends SIGINT and then SIGKILL after 10 s.

**Codex readiness:** `codex --version`, run as `cfagent`, must match `^codex-cli 0\.144\.` (configurable). Readiness is `unauthenticated` if `auth.json` is missing; the UI then shows the exact login command (§12.3). `check --agent` runs a trivial read-only sandboxed exec to prove that Landlock or bubblewrap works under the unit.

### 6.3 Reload semantics

| Change | Action |
|---|---|
| Content `.vue` added or deleted | Wait for the registry, then a **full page reload** in the iframe. The builder registry is module-level, so HMR is not enough. **No dev restart.** |
| Edits to an existing `.vue` | HMR. The bridge reload is the fallback. |
| New pages, `nuxt.config.*`, `package.json`, `layers/**` | `needsRestart` is set, and the UI offers a one-click `POST …/dev/restart`. |
| Unsaved builder changes | The child defers the reload (`reload.ack {reloading:false}`). |

---

## 7. Production

### 7.1 Services model

| Kind | Units | MVP | Later |
|---|---|---|---|
| Adopted | `kangu.service`, `clients-kangu.service`, `mindarchy.service` (unchanged, `User=radu`) | status, logs (Phase 1 if time allows, else Phase 3) | restart (Phase 3), migration to hardened users (D6) |
| Managed | `cf-site@radustanciu.service` | – | deploy, rollback, releases (Phase 3) |

polkit rule, `/etc/polkit-1/rules.d/50-couchfusion.rules`. The subject is **`cfcp` only**; radu-owned processes, including the internet-facing prod apps, get nothing:

```js
polkit.addRule(function (action, subject) {
  if (subject.user !== "cfcp" || action.id !== "org.freedesktop.systemd1.manage-units") return polkit.Result.NOT_HANDLED;
  var unit = action.lookup("unit") || "", verb = action.lookup("verb") || "";
  if (["start","stop","restart"].indexOf(verb) < 0) return polkit.Result.NOT_HANDLED;
  if (/^cf-(dev|agent|build)@[a-z0-9][a-z0-9-]{0,39}\.service$/.test(unit)) return polkit.Result.YES;
  if (/^cf-site@(radustanciu)\.service$/.test(unit)) return polkit.Result.YES;
  if (["kangu.service","clients-kangu.service","mindarchy.service"].indexOf(unit) >= 0) return polkit.Result.YES;
  return polkit.Result.NOT_HANDLED;
});
```

`manage-unit-files`, `reload-daemon` and transient units are never granted. A CI test in a Debian 13 systemd container asserts that `cfcp` can start `cf-dev@x` and **cannot** start `ssh.service`, run `systemd-run` or call `daemon-reload`. The app-slug regex in the server is the same as the polkit regex, and a unit test enforces that.

**Endpoints:**
- `GET /api/v1/services` returns `ServiceSnapshot[]`.
- `GET /services/{unit}`.
- `POST /services/{unit}/{start|stop|restart}` with `{confirm}` returns 202 `ServiceSnapshot`.
- `GET /services/{unit}/events` is SSE event `process` with `ActivityEvent` data, so `LogViewer.vue` is reused.

### 7.2 Releases (Phase 3)

Job `release.deploy`, started by `POST /services/{unit}/deploy {source: "worktree" | {commit}, confirm}`:

1. **Admission.** `MemAvailable` must be at least 1.2 GiB and disk at least 3 GiB free. Otherwise return 409, and the UI offers "stop dev during build".
2. **Source snapshot (U8: no git required).**
   - **Default `source:"worktree"`:** build exactly what is on disk in the VPS workspace, including uncommitted edits. As `cfcp`, copy `apps/<app>`, `layers/` and `cli-content/` into `/var/cache/cf-build/<app>/<UTC ts>/` with `rsync -a --delete`, excluding `node_modules`, `.nuxt`, `.output`, `.env*`, `.git` and `test-results`. Keep the `apps/<name>` layout so the layer scripts still infer the app. Record each repo's `HEAD` sha and a `dirty` flag in `release.json`, so every release can be traced even without commits.
   - **Optional `{commit}`:** when the app repo has a local commit to deploy, use `git worktree add --detach /var/cache/cf-build/<app>/<sha12> <sha>`. No remote or push is needed. Layers are still taken as a worktree snapshot, recorded the same way.
   - The snapshot gives the build its own copy, so editing (or Codex) during the build cannot change what ships.
3. **Build.** `cf-build@<app>` runs as `cfdev` with no secrets, `MemoryMax=1800M`, `CPUQuota=180%` and `Nice=10`: `bun install --frozen-lockfile`, then `node node_modules/nuxt/bin/nuxt.mjs build`. Running `nuxt build` directly skips radustanciu's `postbuild` legacy `git_output.sh`. Build-time `NUXT_PUBLIC_*` values come from the non-secret `/etc/couchfusion/env/<app>.build.env`.
4. **Release.** Copy `.output` to `/var/www/<app>/releases/<UTC ts>-<sha12>/` with a `release.json`.
5. **Switch.** Create symlink `current.new`, then `rename()` it over `current`.
6. **Restart and check.** `systemctl restart cf-site@<app>`. The service must reach `active` within 30 s, then three consecutive `GET http://127.0.0.1:<port>/` probes must return status below 500.
7. **Roll back on failure.** Restore the previous symlink, restart, and fail the job with `prod.rolled_back`. Keep 5 releases. `POST /services/{unit}/rollback {releaseId, confirm}` runs steps 5–7 directly.

**`cf-site@.service`.** The runtime is fixed per site through a drop-in, because systemd allows no variables in the executable path:

```ini
[Service]
User=cf-site-%i
EnvironmentFile=/etc/couchfusion/env/%i.prod.env      # root:cf-site-%i 0640; cfcp cannot read
WorkingDirectory=/var/www/%i/current
ExecStart=/opt/couchfusion/toolchain/node/bin/node /var/www/%i/current/server/index.mjs
Restart=on-failure
MemoryMax=512M
MemoryLow=160M
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadOnlyPaths=/var/www/%i
# cf-site@radustanciu.service.d/runtime.conf may override ExecStart= with the bun path
```

**Ports.** Production gets its own loopback port per site, separate from the dev slots: radustanciu prod is `127.0.0.1:4501` (`NITRO_HOST=127.0.0.1`, `NITRO_PORT=4501` in `radustanciu.prod.env`). Dev stays on `4401` behind slot `9181`. Prod and dev never share `.nuxt` or `.output`, because prod builds run in the snapshot directory.

### 7.2a Moving radustanciu from the old VPS

1. Copy the source to hetzner1 (`rsync` from the laptop: `apps/radustanciu`, `layers`, `cli-content`; no `node_modules`), then `bun install` there.
2. Push the data from the **laptop's local CouchDB** (the old VPS is not used as a source): `rs--content`, `rs--logins`, `analytics_radustanciu` and `_config` `cf_env_radustanciu` go into hetzner1's CouchDB (`phase0/12-push-local-couchdb.sh`). radustanciu has no `rs-*` users in `_users`; builder login on hetzner1 uses an existing CouchDB account there.
3. Build and deploy through §7.2 and probe `http://127.0.0.1:4501/` on the box.
4. Preview over the tailnet before DNS: a temporary `tailscale serve` slot pointing at 4501, or `curl --resolve radustanciu.ro:443:<hetzner1 IP>` once Caddy has the site.
5. Add `/etc/caddy/sites/radustanciu.caddy`, then cut DNS over. Keep the old VPS running until hetzner1 has served traffic for a few days, then retire the dist repo flow (`git_output.sh`, `git-persistent/radustanciu`, and radustanciu's `postbuild` script).

### 7.3 Caddy

- Through Phase 3, **root edits Caddy by hand**: `/etc/caddy/sites/radustanciu.caddy` → `reverse_proxy 127.0.0.1:4501`.
- The control plane never reads Caddy files and never uses the admin API on `:2019`.
- Phase 0 fixes: `chmod 0640 root:caddy /etc/caddy/sites/*.caddy`, and the nft guard blocks `:2019` for untrusted uids.
- Phase 4 (D7) may add `cf-priv`, a root helper reached through one sudoers line. Its `write-caddy <slug>` subcommand refuses files without `# managed-by: couchfusion`, renders to a temporary file, runs `caddy validate`, then `systemctl reload caddy`, and restores the old file on failure.

---

## 8. Git flow

**Repository discovery.** Every directory under the workspace with its own `.git` is a repo: root (if present), `apps/*`, `layers` and `cli-*`. The repo id is the relative path, URL-encoded (for example `apps%2Fradustanciu`).

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/v1/workspaces/{ws}/repos` | – | `[{id, path, branch, upstream?, ahead, behind, dirty, hasRemote}]` |
| GET | `…/repos/{repo}/status` | – | `{branch, upstream?, ahead, behind, head:{sha,subject}, files:[{path,index,worktree,untracked}]}` |
| GET | `…/repos/{repo}/diff?path=&staged=` | – | `{path, diff, truncated, binary}` (256 KiB cap) |
| POST | `…/repos/{repo}/commit` | `{message, paths[], expectedHead, trailerCodex?}` | 202 job `git.commit` |
| POST | `…/repos/{repo}/pull` | `{}` | 202 job `git.pull` (`--ff-only`) |
| POST | `…/repos/{repo}/push` | `{branch, confirm}` | 202 job `git.push` |

**Rules:**
- `paths` must be a subset of the current status list. There is never `add -A`.
- Paths matching `**/.env*`, `*.pem`, `*.key`, `**/auth.json`, `**/node_modules/**` or `.output/**` are refused.
- `expectedHead` must equal HEAD, otherwise 409.
- Commits on an app repo take that site's lease.

**Hardening.** Git runs as `cfcp`, the push-key holder, in trees that agent code can write.
- `.git` is owned by `cfcp`, with ACL `u:radu:rwX` so the human can still commit over SSH, and no write for `cfsrc`. Dev and agent units also have `InaccessiblePaths` on it.
- Every invocation uses `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`, `-c core.hooksPath=/dev/null -c core.fsmonitor=false -c safe.directory=<repo>`.
- **Before each run**, `git config --local --list` is checked against an allowlist. The repo is refused if it defines `filter.*`, `diff.*.textconv`, `diff.external`, `core.sshCommand`, `core.pager`, `core.editor`, `include*`, `credential.*` or `uploadpack.*`.
- Push uses `GIT_SSH_COMMAND="ssh -F /dev/null -i $CREDENTIALS_DIRECTORY/git-<repo-slug> -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/couchfusion/known_hosts"`.
- **Never force.** Only branches in the per-repo `push_branches` (default `["main"]`). If the remote is ahead, fail with 502 `git` and the hint "pull first".
- Hooks are disabled for UI commits (D5). radu's own CLI commits as `radu` keep cli-githooker.

**Identity.**
- Author and committer: `Radu Stanciu <radu@kangu.ro>`, from `server.toml`.
- Trailer `Committed-Via: couchfusion-cp`. With `trailerCodex`, also `Co-Authored-By: Codex <noreply@openai.com>`.
- Push keys are per-repo deploy keys with write access, delivered with `LoadCredential=` (D2).
- radustanciu needs a remote before push means anything. Until then, the UI shows `hasRemote:false` and hides Push.

Every commit and push is written to `audit`.

---

## 9. Isolation and resource limits

### 9.1 Users

| User | Runs | Can write | Cannot reach |
|---|---|---|---|
| `cfcp` | control plane | `/var/lib/couchfusion`, `/run/couchfusion`, `.git` dirs, `/var/cache/cf-build`, `/var/www/<managed>/releases` (Phase 3) | prod env files, dev env files (except its own `/run` files), Caddy |
| `cfdev` | `cf-dev@`, `cf-build@` | app tree, `layers`, its cache | `.git`, credentials, control DB, `/var/www`, `/home`, protected loopback ports |
| `cfagent` | `cf-agent@` | app tree, `/var/lib/cf-agent` (its `CODEX_HOME`) | as `cfdev`, plus the dev env file |
| `radu` | human over SSH or VS Code Remote; legacy prod units | everything it owns; group `cfsrc` | the `cfcp` state and credentials |

`couchfusion.service`:
```ini
[Service]
Type=notify
User=cfcp
Group=cfcp
SupplementaryGroups=cfsrc systemd-journal
ExecStart=/opt/couchfusion/current/couchfusion-server serve --config /etc/couchfusion/server.toml
Restart=on-failure
RestartSec=2
RuntimeDirectory=couchfusion
RuntimeDirectoryPreserve=yes
StateDirectory=couchfusion
LoadCredential=git-apps-radustanciu:/etc/couchfusion/credentials/git-apps-radustanciu
LoadCredential=git-layers:/etc/couchfusion/credentials/git-layers
MemoryMax=256M
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
CapabilityBoundingSet=
ReadWritePaths=/srv/couchfusion/workspace /var/lib/couchfusion /var/cache/cf-build
```

### 9.2 Loopback egress guard (`/etc/nftables.d/couchfusion.nft`)

```nft
table inet couchfusion {
  set untrusted { type uid; elements = { cfdev, cfagent } }
  set protected { type inet_service; elements = { 2019, 4369, 5984, 8087, 8096, 8443, 9119, 9180-9194, 9223, 9224, 10000, 4310, 4311, 2004 } }
  chain output { type filter hook output priority 0; policy accept;
    meta skuid @untrusted ip  daddr 127.0.0.0/8 tcp dport @protected reject
    meta skuid @untrusted ip6 daddr ::1         tcp dport @protected reject
  }
}
```

- **D1 (dev shares prod CouchDB):** `cfdev` must reach CouchDB, so it gets its own rule that accepts `:5984` for `cfdev` **before** the reject. `cfagent` stays blocked from `:5984`. In practice that is a second set: `untrusted_dev = { cfdev }` with `protected` minus 5984, and `untrusted_agent = { cfagent }` with the full `protected` set.
- `4401-4404` remain reachable.
- `IPAddressDeny=100.64.0.0/10` in the units blocks the route through the box's own tailnet IP to tailscale-served CouchDB, Webmin and Hermes.
- `doctor` and `/system/health` run a self-test: `cf-dev@selftest` must fail to connect to `:2019`.
- **Pre-existing exposure:** Phase 0 checks whether 4369, 556 and 33287 are publicly reachable. If no Hetzner Cloud Firewall exists, it recommends an nft `input` policy. This is reported, not changed without consent.

### 9.3 Memory and CPU

- **Slice.** `couchfusion.slice`: `MemoryHigh=1700M`, `MemoryMax=2G`, `MemorySwapMax=1G`, `CPUWeight=50`. Drop-ins add `MemoryLow=250M` on `couchdb.service` and `MemoryLow=150M` on each prod unit, so pressure reclaims from dev first.
- **Governor:**
  - `heavy` semaphore (1 permit): codex runs, `bun install`, builds, registry fallback.
  - `dev`: at most 1 running dev session in the MVP (2 from Phase 2).
  - Admission reads `MemAvailable` and `/proc/pressure/memory` (`some avg10`) against per-kind estimates: dev 900 MiB, agent 400 MiB, build 1.2 GiB.
  - Work that can't start stays `queued` with event `resources.waiting` ("needs ~1.2 GiB, 0.7 GiB available"). New starts are refused with 503 `unavailable` when `MemAvailable` is below 400 MiB, or PSI is above 20 %.
- **Idle reaper** (Phase 2): stop a dev unit after 2 h with no slot requests.
- **Measure in Phase 0 (S5):** radustanciu `nuxt dev` RSS, Codex RSS and `nuxt build` peak. The VS Code Remote server (300–500 MB) also counts.

### 9.4 Data isolation (dev vs prod CouchDB)

- **Decided (D1, 2026-10-29): dev shares the production CouchDB on hetzner1.** radustanciu's databases (`rs--content`, `rs--logins`, `analytics_radustanciu`) are pushed from the laptop's local CouchDB into hetzner1's CouchDB (`127.0.0.1:5984`), and both `cf-site@radustanciu` and `cf-dev@radustanciu` use it. Builder edits in a dev session are live content edits. That is the intended workflow, since the VPS is the primary editing machine.
  - The dev env file (`/etc/couchfusion/env/radustanciu.dev.env`, root:cfdev 0640) holds the CouchDB credential. `cfagent` can neither read it nor reach `:5984` (§9.2), and Codex runs with network off. The agent never touches data, only `.vue` files.
  - Use a **per-app CouchDB user** with access to only the `rs-*` databases plus `_config` read, not the server admin, where the layers allow it. Check this in Phase 0 against how `layers/database` uses `COUCHDB_ADMIN_AUTH`. If admin is required, record that as an accepted risk.
  - **Backups become mandatory:** a nightly replication or `couchdb-dump` of `rs-*` to `/var/backups/couchdb/` with 14 days kept, plus a manual "snapshot now" before risky sessions (Phase 2 UI button).
  - The earlier alternative (separate `couchdb-dev.service` on `:5985`) stays documented as an option if live editing from dev becomes a problem.
- **No `.env` files in the VPS workspace.** `doctor` and the unit runner refuse to start dev or the agent if `apps/<app>/.env*` exists. Otherwise Nuxt or c12 would load it and Codex could read it, putting production secrets into model context.

---

## 10. Persistence and observability

**sqlite.** `/var/lib/couchfusion/control.db`, WAL mode, 0600, migrations embedded.

| Table | Contents |
|---|---|
| `sessions`, `login_tokens` | hashes only |
| `slots` | `port ↔ ws/site`, stable; radustanciu gets 9181 |
| `dev_sessions` | unit, port, token values (the DB is `cfcp`-only), journal cursor |
| `jobs`, `job_events` | 200 jobs, 200 events each; `result` JSON |
| `idempotency` | 10 minutes |
| `agent_runs` | jobId, threadId, output offset |
| `trash` | soft-deleted components |
| `audit` | every mutation |

On boot, in-process `running` jobs become `failed` with code `interrupted`. Agent runs whose unit is still active resume tailing; otherwise they are finalised from the JSONL tail.

**Logs and SSE:**
- `tracing` writes to journald (`SYSLOG_IDENTIFIER=couchfusion`) with fields `job_id`, `site`, `unit` and `login`, redacted before emission.
- SSE keeps the Go framing (`id`, `event`, `data`; resume with `Last-Event-ID` or `?after=`). It adds a `:keepalive` comment every 15 s, has no write timeout, and sends `X-Accel-Buffering: no`.
- Fan-out uses `tokio::sync::broadcast`. A lagged subscriber gets a fresh snapshot event instead of silent loss.

**Health:**
- `GET /api/v1/system/health` extends Go's `Health` with `host{memAvailableBytes, swapUsedBytes, psiMemSome10, diskFreeBytes}`, `units[]`, `governor{heavy:{holder?,queued}}`, `nftGuard: ok|missing`, `tailscaleServe: ok|missing` and `codex` readiness.
- `couchfusion-server doctor` checks permissions, the absence of `.env` files, polkit, nft, the toolchain and Codex versions, slot sockets, same-filesystem trash, and `.git` ownership.

---

## 11. API

All paths are under `/api/v1`. Envelopes, error codes, time formats (including Go's zero time `0001-01-01T00:00:00Z` and the 9-digit `WorkspaceSummary` format) and omit-not-null optionals are kept byte-compatible. Error codes are mapped as in Go; new conditions reuse `conflict`, `validation` and `unavailable`, so the SPA's `ErrorCode` union holds. The union gains `forbidden`, `unsupported_media_type`, `request_too_large`, `invalid_request` and `reauth_required`.

### 11.1 Kept Go routes (by number in the cli-init map)

| Routes | Phase | Notes |
|---|---|---|
| 0 `bootstrap` | 1 | Becomes `{version:"1", server:"rust", capabilities:["dev-session","component-delete","component-generate","git","services"], devOrigins:[…]}` |
| 1 `GET /workspaces`, 4 `GET /workspaces/{w}` | 1 | One workspace, configured in `server.toml`. There is no registry file on the VPS. |
| 10, 11 health | 1 | Extended as in §10 |
| 12, 13 onboarding | 1 | Persisted in sqlite. `GET` returns `completed` by default so the global middleware doesn't loop. |
| 17, 18, 19 site, tasks, components | 1 | |
| 26, 27, 32–35 processes | 1 | `script:dev` maps onto the dev session. URLs are rewritten per caller. |
| 28, 29, 31 jobs | 1 | |
| 20 registry rebuild | 2 | |
| 22–24 preview-host | 2 | Mapped onto the dev session. `url = <slotOrigin>/__couchfusion/components/preview?token=…`. IDs are validated (fixes gotcha 9). |
| 25 tasks run | 2 | Finite scripts run as `cf-build@` jobs in `CF_BUILD_MODE=script` |
| 36–44 agent conversations | 4 | Return 503 `unavailable` until then. The SPA's generator panel switches on the `component-generate` capability. |
| 2, 3, 5–9, 14–16, 30 | – | 503 `unavailable` until Phase 4. 14 (`dependency install`) and 3 (`initialize`) return 422 "not supported on server" permanently. |
| 21 `components/{id}/open` | – | Replaced by `GET …/components/{id}/source` → `{path, sha256, content}` (256 KiB cap), Phase 2 |

### 11.2 New routes

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/auth/exchange?token=` | – | 303 + cookie (not under `/api`) |
| GET | `/api/v1/session` | – | `{login, via:"tailscale"\|"token", expiresAt?}` |
| POST | `/session/logout` | `{}` | 204 |
| GET | `/workspaces/{w}/sites/{s}/dev` | – | `DevSessionView` |
| POST | `…/dev/start` / `stop` / `restart` | `{}` | 202 `DevSessionView`. 409 `conflict {activeSiteId}` when no slot is free; 503 under memory pressure |
| GET | `…/sites/{s}/events` | – | SSE `event: site` |
| GET | `…/components/{c}/usage` | – | `ComponentUsageView` (§6.1) |
| DELETE | `…/components/{c}` | `{expectedSha256, acknowledgeUsage}` | 202 Job `component.delete` |
| GET | `…/components/trash` | – | `TrashEntry[]` (Phase 2) |
| POST | `…/components/trash/{t}/restore` | `{}` | 202 Job `component.restore` (Phase 2) |
| POST | `…/components/generate` | `{prompt}` + `Idempotency-Key` | 202 Job `component.generate` |
| POST | `…/components/generate/{jobId}/followup` | `{prompt}` + `Idempotency-Key` | 202 Job |
| GET | `/jobs?siteId=&type=&limit=50` | – | `JobSnapshot[]` with `events:[]` |
| GET | `/processes?siteId=&active=true` | – | `ProcessSnapshot[]` with `events:[]` (Phase 2) |
| * | `/workspaces/{w}/repos/**` | §8 | §8 |
| * | `/services/**` | §7 | §7 |
| GET | `/audit?limit=` | – | `AuditEntry[]` |

### 11.3 DTOs (TypeScript notation, added to `web/types/api.ts`)

```ts
type DevSessionView = { status: 'stopped'|'starting'|'ready'|'failed'; processId?: string; slotPort: number;
  url?: string; builderUrl?: string; previewUrl?: string; needsRestart?: { reason: string };
  startedAt?: string; memoryCurrentBytes?: number; problem?: string }
type SiteEvent = { id: number; time: string; type: 'dev.status'|'registry.changed'|'component.deleted'|
  'component.generated'|'reload.requested'|'git.changed'; data: Record<string, unknown> }
type ComponentUsageView = { componentId: string; source: string; sha256: string;
  git: 'tracked_clean'|'tracked_modified'|'untracked'|'unknown';
  pages: { id: string; path: string; title?: string }[]; codeReferences: { path: string; line: number }[];
  pageScan: 'ok'|'unavailable' }
type JobResult = { componentId?: string; trashId?: string; changedFiles?: string[]; threadId?: string;
  lint?: { path: string; prop: string; message: string }[]; needsRestart?: { reason: string }; sha?: string }
  // JobSnapshot gains optional `result?: JobResult` and `parentJobId?: string`
type ServiceSnapshot = { unit: string; kind: 'adopted'|'managed'; siteId?: string; activeState: string;
  subState: string; mainPid?: number; memoryCurrentBytes?: number; since?: string; publicUrl?: string;
  release?: { id: string; sha: string; createdAt: string } }
type RepoSummary = { id: string; path: string; branch: string; upstream?: string; ahead: number;
  behind: number; dirty: number; hasRemote: boolean }
```

---

## 12. Build, release and install on hetzner1

### 12.1 Build (laptop, macOS arm64)

```bash
# cli-init/server/release.sh
(cd ../web && bun install --frozen-lockfile && bun run test:run && bun run typecheck && bun run generate)
cargo zigbuild --release --target x86_64-unknown-linux-musl --features embed-spa
tar --zstd -cf couchfusion-server-$VER-x86_64-linux-musl.tar.zst -C target/x86_64-unknown-linux-musl/release couchfusion-server
shasum -a 256 couchfusion-server-*.tar.zst > SHA256SUMS
```

- This needs `cargo-zigbuild 0.20.x` and zig 0.13+. It produces a static binary because rustls is used (no OpenSSL) and `rusqlite` is bundled.
- `build.rs` asserts that `web/.output/public/index.html` contains "CouchFusion Workbench".
- An optional GitHub Actions job (ubuntu, same target) produces the release of record. The VPS never runs cargo.

### 12.2 Release

1. `scp` the tarball and checksums to `/opt/couchfusion/releases/$VER/`.
2. As root: verify the checksum and unpack.
3. Run `couchfusion-server doctor --binary <new>`.
4. `ln -sfn` the new release to `/opt/couchfusion/current`.
5. `systemctl restart couchfusion`.
6. If `/run/couchfusion/admin.sock` doesn't answer `health` within 20 s, revert the symlink.

Dev and agent units keep running throughout, so the API blips for about 1 s. Three releases are kept. There is no self-update from the UI.

### 12.3 One-time install (Phase 0; `packaging/install.sh`, run as root, idempotent)

1. **Users and groups:** `cfcp`, `cfdev`, `cfagent`, group `cfsrc`. Add radu to `cfsrc`.
2. **Toolchain** under `/opt/couchfusion/toolchain/{node-22.x,bun-1.x,codex-0.144.x}` (root-owned, read-only). Node 22 LTS, because Nuxt 4.1 needs 20.19 or later. Codex is the official linux-musl release binary. Run `apt install build-essential python3` for `better-sqlite3`.
3. **Workspace.** `/srv/couchfusion/workspace` with setgid directories. Copy `apps/radustanciu`, `layers` and `cli-content` from the laptop with `rsync` (U8/U9: no remote yet; `.git` directories come along so local history and commits keep working; `node_modules`, `.nuxt`, `.output` and `.env*` are excluded). Set `.git` ownership and ACLs. Run `bun install` as `cfdev`.
4. **Units and rules:** `couchfusion.slice`, `couchfusion.service`, `cf-dev@`, `cf-agent@` and `cf-build@` templates, the polkit rule, tmpfiles.d, the nft guard, and the `MemoryLow` drop-ins.
5. **Env and credentials:** `/etc/couchfusion/env/radustanciu.dev.env`, `/etc/couchfusion/credentials/git-*` (0400 root), `/etc/couchfusion/known_hosts`.
6. **Data (D1):** push radustanciu's databases from the laptop's local CouchDB into hetzner1's CouchDB (`12-push-local-couchdb.sh`), create a per-app CouchDB user if the layers allow it, set `cf_env_radustanciu` in `_config`, and enable the nightly backup.
7. **Tailscale:** `tailscale serve --bg --https=P unix:/run/couchfusion/ts-P.sock` for P in 9180–9184.
8. **Codex login:** `sudo -u cfagent env CODEX_HOME=/var/lib/cf-agent/.codex /opt/couchfusion/toolchain/bin/codex login --device-auth`.
9. **Caddy file mode** fix.

---

## 13. Testing

| Layer | Coverage |
|---|---|
| Rust unit | Manifest checks (port Go's `components_test` cases). Path containment: symlink at every depth, `..`, case, unicode. Redaction. SSE framing and resume. Leases. Governor admission. Go time formats. Git config allowlist. Slug regex parity with polkit. Codex JSONL normalisation against recorded transcripts. `builderFieldMeta` lint. |
| Contract | Run Go `internal/web/testserver` once and record golden JSON for every kept route. `insta` asserts key-for-key parity, including omissions and time formats. |
| Integration (axum `oneshot`) | `process_group` runner with `fake-dev` (a tiny bin that serves `/__couchfusion/control/{ping,component-usage}`, logs lines, and echoes headers so tests can assert cookie and `Tailscale-*` stripping). A `fake-codex` script replaying `fixtures/codex-exec/*.jsonl` that writes a `.vue` (one fixture also edits `package.json` to check the policy failure). Delete and generate end to end on a temporary app tree with a fake watcher. |
| Security | Wrong Host → 403. `same-site`/`cross-site` mutations → 403. The slot listener never routes `/api`. Identity headers are ignored on TCP. A postMessage from the wrong origin or source is ignored (Vitest in the layer). |
| Playwright (reuse `cli-init/web/e2e`) | `webServer.command = couchfusion-server fixture --port 4178 --deterministic` in all five configs; port `control-plane.spec.ts` first. New `dev-session.spec.ts` uses the `web/e2e/fixtures/component-preview` Nuxt app with the bridge env: open the dev page, `frameLocator` into the picker, delete, confirm in the parent, then assert the trash entry, the manifest update and the frame reload. Also create with `fake-codex`, then check focus. `hmr.spec.ts` goes through the slot proxy. `agent` specs stay on Go until Phase 4. |
| systemd (CI, privileged Debian 13 container) | Template units, the polkit rule (positive and negative), reattach after `systemctl restart couchfusion`, OOM of a fixture dev unit, deploy and rollback with a failing health check (Phase 3). |
| VPS smoke | `couchfusion-server check --smoke`: start radustanciu dev, probe through the slot, delete a throwaway component and restore it, generate with a canned prompt, confirm `git status` is dirty, stop. |

---

## 14. Migration from Go

- The Go `couchfusion ui` keeps running unchanged on the laptop in Phases 1–3. The SPA hides new features unless `bootstrap.capabilities` advertises them, so one SPA build serves both backends.
- Unported routes return 503 `unavailable`, which the SPA already renders.
- Phase 4:
  - port the app-server client (routes 36–44, byte-compatible: no `"jsonrpc"` field, numeric ids, both sandbox spellings, approvals, grants, consents, backpressure instead of `ErrOverloaded`, resume only the opened conversation);
  - add `--local` mode (`process_group` runner) so the laptop can run Rust;
  - `import-go-state` for `~/.couchfusion/{workspaces.json,onboarding.json,config.yaml}`;
  - delete `internal/web/dist` and `internal/web/testserver` once all Playwright configs pass on Rust.
- Go's scaffolding commands (init, new, create-layer) stay in Go (D8).
- The Codex skill moves to `cli-init/shared/skills/couchfusion-component-builder/SKILL.md`. Rust uses `include_str!` and Go embeds a copy, so both read one source.

---

## 15. Phased plan

### Phase 0: VPS prep and spikes (about 1–1.5 days, no product code)

**Work:** run §12.3 steps 1–9 by hand from the scripts, write `cf-dev@radustanciu` and start it with `systemctl`.

**Spikes:**

| ID | Question | Pass condition |
|---|---|---|
| S1 | Does `nuxt dev` plus the registry watcher work under the `cf-dev@` sandbox? | The manifest updates within 1 s of adding a `.vue` |
| S2 | Does HMR work through `tailscale serve :9181` straight to 4401, and later through the proxy? What Vite settings does it need? | A `.vue` edit appears without a reload |
| S3 | Does `tailscale serve` support `unix:` targets, and does it overwrite client-sent `Tailscale-User-*` headers? | Both yes, or switch to the §4.2 fallback |
| S4 | Does the Codex workspace-write sandbox work under `cf-agent@`? Does 0.144.x support `exec --json`, `--skip-git-repo-check`, `exec resume` and `--output-last-message`? | A trivial run writes a file in the app and is denied outside it |
| S5 | What are the RSS figures for dev, Codex and build? | Recorded; governor estimates set |
| S6 | Does `better-sqlite3` install on Node 22 linux-x64? | `bun install` succeeds |

**Exit criteria:**
- `/builder/` loads over the tailnet with HMR or a manual reload.
- The `cf-dev` nft self-test is blocked on `:2019`, `:5984` and `:9180`.
- Codex is logged in and the sandbox works.
- D1 is answered (D2 is deferred; D3 and D4 are accepted).

### Phase 1: MVP, the pilot asks on radustanciu (about 5–7 working days)

| Step | Deliverable | Acceptance |
|---|---|---|
| 1 | Skeleton: config, sqlite, both auth paths, guards, embedded SPA with nonce CSP, bootstrap capabilities, health, `doctor`, unit file, release script | The SPA loads at `https://…ts.net:9180` with no token on the tailnet, and at `http://localhost:9180` after `login-url` |
| 2 | `UnitRunner` (systemd + process_group), dev session API, journal SSE, slot proxy with WebSocket and boot page, per-caller URL rewrite, SPA full-screen dev page | **Ask 1:** the builder works full screen in the frame, and `systemctl restart couchfusion` leaves dev running and the UI reattaches |
| 3 | Layer bridge and control routes (ping, usage); usage, DELETE job, trash, registry wait and fallback; reload protocol including unsaved deferral | **Ask 2:** delete in the picker → confirm in the parent → file in trash → manifest updated in under 2 s → frame reloads without the component and without a dev restart. Deleting a used component shows its pages first. |
| 4 | `component.generate` via `cf-agent@`, follow-up, cancel, dependency policy, `builderFieldMeta` lint, `needsRestart`; `GET /jobs`; site events | **Ask 3:** prompt → new `.vue` → registry → frame reloads with the new card focused |
| 5 | Repos, status, diff, commit and push with hardening and step-up; audit; Playwright `control-plane` and `dev-session` specs on the Rust fixture | Commit the generated component from the UI, and push it once the remote exists |

**Exit criteria:**
- All three asks pass from the laptop over the tailnet, and Ask 1 also passes through the SSH tunnel.
- A forged postMessage from the iframe cannot delete anything.
- `cfagent` cannot read `/etc/couchfusion/credentials` or `.git`, or reach `:2019`.
- Docs: `cli-init/docs/` (server, ops runbook), `layers/content/docs/` (bridge and control routes), and `apps/radustanciu/docs/implementation_results/<ts>-vps-control-plane-pilot.md`.

### Phase 2: site parity (about 1 week)

- Preview-host routes mapped onto the dev session.
- Registry rebuild, tasks run, `GET /processes`.
- Trash list and restore, the source viewer.
- Per-slot cookie prefixing, a second slot, the idle reaper.
- Prod service status and logs (read-only), `git pull --ff-only`.
- The remaining Playwright configs, except agent.

**Exit:** the Go-era site page works fully against Rust on the VPS. Two apps can run dev concurrently, or the second is refused cleanly.

### Phase 3: production

- Prod restart with step-up.
- `cf-site@radustanciu`, the build, release, switch and rollback pipeline with admission control.
- radustanciu's first prod deploy behind a hand-written Caddy file.
- A recommendation (D6) to move kangu and clients-kangu onto hardened users.

**Exit:** deploy, then a failing-health deploy that rolls back on its own, then a manual rollback, all from the UI.

### Phase 4: full replacement

- Codex app-server port (routes 36–44) with approvals, consents and questions.
- Crate split, `--local` laptop mode, `import-go-state`, Go `ui` retired.
- Optional `cf-priv` for Caddy and unit installs (D7), optional `systemd-oomd`, and Tailscale Services hostnames per slot.

**Exit:** all five Playwright configs pass against Rust, and Go `internal/web` is deleted.

---

## 16. Risks

1. **HMR through `tailscale serve` and the proxy** (Vite `allowedHosts`, HMR host and port, ws origin and token). *Mitigation:* spike S2 before proxy code, a gated layer override, and the bridge reload as fallback.
2. **`tailscale serve` details** (`unix:` targets, header overwrite, SSE flushing on non-443 ports). *Mitigation:* spike S3 and the TCP + nft-uid fallback, which the server refuses to use if its self-test fails.
3. **Memory.** Dev (about 1 GiB), Codex with vue-tsc (0.4–0.9 GiB), builds (1.5 GiB or more) and VS Code Remote all compete for about 1.6 GiB. *Mitigation:* the slice, the governor, PSI admission and swap. Builds may need dev stopped, or CI builds (D4).
4. **Codex 0.144.x flag and event drift** (`exec --json` event names, `exec resume`, `--skip-git-repo-check`). *Mitigation:* spike S4, recorded-transcript tests per upgrade, and a version gate.
5. **The Codex Linux sandbox under systemd** may need user namespaces, or may not be enforceable. If it isn't, generate fails closed.
6. **Sandboxed `nuxt dev`** may need more writable paths, such as the layer `.nuxt` directories or the bun cache (spike S1). Relax only what is needed and document it.
7. **Agent-written code reaches production** through a human commit. Review is the control: the diff view plus the dependency policy. There is no SAST in the MVP.
8. **Legacy prod units running as radu** stay a local-attacker risk until D6. They can no longer forge control-plane identity or restart units, but they can still reach the protected loopback services.
9. **The usage scan** covers only `page-*` bodies. Components referenced from globals, design docs or translations may be missed. It is advisory, and trash makes deletes reversible.
10. **Concurrent edits** from radu over SSH, Codex and the laptop. CAS checks and baselines detect races but do not prevent them. Pull is ff-only and push is never forced; conflicts are resolved by hand.
11. **Disabled hooks on UI commits** bypass cli-githooker for those commits.
12. **Disk** (23 GB free). Worktrees, releases, trash and node_modules grow. Prune from day one.
13. **SSH-tunnel HTTP/1.1** limits each origin to six connections, which SSE uses up. Coalesce streams; a multiplexed `/stream` comes later.
14. **The radustanciu content source.** Where its `rs-*` databases live today, and how they reach the VPS, still needs answering (part of D1).

---

## 17. Decisions needed from the user

| # | Decision | Recommended default |
|---|---|---|
| D1 | **Decided (2026-10-29):** move `rs-*` to hetzner1's CouchDB; dev shares prod data (§9.4). | – |
| D2 | **Deferred (U9).** A git remote for `apps/radustanciu` and push credentials. Until then, local commits only. | When it comes: a private GitHub repo, per-repo deploy keys, push to `main` only |
| D3 | **Accepted (U10):** device login as `cfagent`, pinned 0.144.x until the Phase 4 port. | – |
| D4 | **Accepted (U10):** your tailnet login only; prod builds run on the VPS behind the governor. | – |
| D5 | Disable git hooks for UI commits? Running them would execute repo scripts as the push-key holder. | Disable; your own SSH commits keep hooks |
| D6 | Move `kangu.service` and `clients-kangu.service` off `User=radu` onto hardened per-site users in Phase 3. This changes production units. | Yes, in Phase 3 with a maintenance window |
| D7 | May the control plane ever write Caddy site files and reload Caddy (through `cf-priv`, Phase 4), or does Caddy stay hand-managed? | Hand-managed until Phase 4, then `cf-priv` with managed markers |
| D8 | Scope of "replace Go": only `couchfusion ui`, or also init, new and create-layer? | Only `ui`; scaffolding stays in Go |
| D9 | Ports: 9180 (control), 9181–9184 (slots), 4401–4404 (internal). | Accept |
| D10 | Codex in the MVP runs with no in-flight approvals (sandboxed, network off, dependency changes flagged as failures). Approvals return in Phase 4. Acceptable? | Accept |
