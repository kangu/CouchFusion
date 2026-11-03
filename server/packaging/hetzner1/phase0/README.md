# hetzner1 Phase 0 setup

Prepares hetzner1 for the Rust control plane (spec: `cli-init/docs/superpowers/specs/2026-10-29-rust-vps-control-plane-design.md`, §12.3 and §15 Phase 0). No control-plane code runs yet. These steps set up the users, toolchain, sandboxed dev unit, guards and spikes that Phase 1 builds on.

**Existing services are never restarted or edited** (kangu, clients-kangu, clients-kangu-dev, mindarchy, CouchDB, Caddy, Hermes, email-sender). Each step is idempotent and lists its undo command at the top.

## Order

| Step | Where | What | Changes the box? |
|---|---|---|---|
| `00-preflight.sh` | hetzner1, root | Read-only report: ports, users, nft tables, tailscale serve, memory | no |
| `01-users.sh` | hetzner1, root | Users `cfcp`, `cfdev`, `cfagent`; group `cfsrc` (adds radu) | yes |
| `02-toolchain.sh` | hetzner1, root | node 22.23.3, bun 1.4.2, codex 0.144.4 in `/opt/couchfusion/toolchain` (checksums verified); apt build tools | yes |
| `03-workspace.sh` | hetzner1, root | `/srv/couchfusion/workspace` with group permissions; `.git` locked against `cfsrc` | yes |
| `sync-from-laptop.sh` | **laptop** | One-time seed of `apps/radustanciu`, `layers`, `cli-content` (with `.git`, without `node_modules`/`.env*`) | copies files |
| `03-workspace.sh` again | hetzner1, root | Fix ownership after the seed | yes |
| `04-deps.sh` | hetzner1, root | `bun install` as `cfdev`; spike S6 (better-sqlite3) | yes (node_modules) |
| `05-units.sh` | hetzner1, root | `couchfusion.slice`, `cf-dev@.service`, dev env template `/etc/couchfusion/env/radustanciu.dev.env` | yes, nothing started |
| `06-nft.sh` | hetzner1, root | Separate nft table `inet couchfusion` that only matches `cfdev`/`cfagent`; self-test | yes |
| `07-caddy-perms.sh` | hetzner1, root | `0640 root:caddy` on `/etc/caddy/sites/*.caddy`; no reload | yes |
| `08-tailscale-slot.sh` | hetzner1, root | `tailscale serve --https=9181 → 127.0.0.1:4401` (tailnet only) | yes |
| `09-codex.sh login` | hetzner1, root (interactive) | Codex device login as `cfagent`. **You** finish it in the browser. | yes |
| `09-codex.sh spike` | hetzner1, root | Spike S4: sandbox write inside allowed, outside denied | temp files only |
| `12-push-local-couchdb.sh` | **laptop** (interactive) | **D1 data**: push `rs--content`, `rs--logins`, `analytics_radustanciu` (+ `_security`) and `_config` `cf_env_radustanciu` from the laptop's local CouchDB into hetzner1's CouchDB over the tailnet. Asks for the hetzner1 admin login. Refuses to overwrite existing databases or keys without `--merge`. `--dry-run` first. | yes (new DBs on hetzner1) |
| fill env | hetzner1, you | `sudoedit /etc/couchfusion/env/radustanciu.dev.env` (CouchDB, ImageKit values). Values are never printed by these scripts. | yes |
| `10-dev-spikes.sh` | hetzner1, root | Start `cf-dev@radustanciu`; spikes S1 (registry watcher) and S5 (memory); S2 (HMR) is checked by hand in the browser | starts the dev unit |
| `11-spike-tailscale-unix.sh` | hetzner1, root + laptop curl | Spike S3: unix-socket target and header overwrite on port 9199, removed after 2 min | temporary |

Copy the folder to the box first:

```bash
rsync -a cli-init/server/packaging/hetzner1/phase0/ hetzner1:/root/couchfusion-phase0/
```

## Ports

| Port | Use |
|---|---|
| 4401 | `nuxt dev` for radustanciu (loopback) |
| 9181 | tailnet slot for the dev site |
| 9180 | control plane (Phase 1) |
| 4501 | radustanciu production (Phase 3, loopback, behind Caddy) |
| 9199 | spike S3 only, temporary |

## Exit criteria (from the spec)

- `/builder/` loads over the tailnet at `https://hetzner1.tailb7471b.ts.net:9181/k/`, with HMR or at least a manual reload.
- The nft self-test shows `cfdev` blocked on `:2019`, `:9119` and `:10000`, `cfagent` also blocked on `:5984`, and DNS still works.
- Codex is logged in as `cfagent`, and S4 shows the sandbox holds.
- S1, S3, S5 and S6 results are recorded in `cli-init/docs/implementation_results/`.
