# hydra1 Phase 1 install — couchfusion-server

Installs the Rust control plane on hydra1 on top of Phase 0 (`../phase0`). Contract: `cli-init/docs/superpowers/specs/2026-10-29-phase1-mvp-contract.md`.

Every step is idempotent and lists its undo command at the top. Existing services (kangu, clients-kangu, clients-kangu-dev, mindarchy, CouchDB, Caddy, Hermes) are not touched.

## Before deploying

1. **Registry file mode (blocking).** `layers/content/scripts/registry-output.mjs:62` writes `component-definitions.{json,ts}` with mode `0600`. The registry watcher runs as `cfdev`, so `cfcp` (the control plane) cannot read the manifest, and component delete/generate fail with "Component registry metadata is unavailable." Write them `0640` or `0660` (group `cfsrc` via the setgid directory), sync `layers` to the VPS, and let the watcher regenerate once.
2. **Layer and SPA changes.** Sync the Stream C `layers/content` changes into `/srv/couchfusion/workspace/layers`; the VPS copy is primary, so review the diff first. Build the SPA (`cd cli-init/web && bun run generate`).

## Build (laptop)

```bash
cd cli-init/server
./release.sh                 # tests, static x86_64 musl binary, bundles web/.output/public as web/
rsync -a dist/couchfusion-server-*.tar.zst* packaging/hydra1/phase1/ root@hydra1:/root/couchfusion-phase1/
```

## Install (hydra1, root, in /root/couchfusion-phase1)

| Step | What | Changes the box? |
|---|---|---|
| `00-preflight.sh` | Phase 0 present, ports free, codex logged in, registry readable by cfcp | no |
| `01-groups.sh` | `cfcp` joins `cfsrc` and `systemd-journal` | yes |
| `02-release.sh <tarball>` | `/opt/couchfusion/releases/<rel>`, `current` symlink, keeps 3 | yes |
| `03-units.sh` | `/etc/couchfusion/server.toml`, `couchfusion.service`, `cf-agent@.service`, cf-dev drop-in, polkit rule, tmpfiles; polkit self-test | yes, nothing started |
| `04-git-ownership.sh` | each repo's `.git` → `cfcp`, ACL `u:radu:rwX`; `safe.directory` for radu | yes |
| `05-start.sh` | `doctor`, `systemctl enable --now couchfusion`, restart dev through the API (fresh control token) | starts the service, restarts `cf-dev@radustanciu` |
| `06-tailscale.sh` | `tailscale serve` 9180 → `unix:/run/couchfusion/ts-9180.sock`, 9181 → `unix:…/ts-9181.sock` (replaces the Phase 0 direct mapping) | yes |

Then open `https://hydra1.tailb7471b.ts.net:9180/`. The fallback is the SSH tunnel: `ssh -L 9180:127.0.0.1:9180 -L 9181:127.0.0.1:9181 hydra1`, then `ssh root@hydra1 sudo -u cfcp /opt/couchfusion/current/couchfusion-server login-url` and open the printed link.

## Upgrades

Run `release.sh`, copy the tarball, run `02-release.sh <tarball>`, then `systemctl restart couchfusion`. Dev and agent units keep running; the server re-adopts them on boot.

## Files

| Path on hydra1 | Owner/mode | Purpose |
|---|---|---|
| `/etc/couchfusion/server.toml` | root:cfcp 0640 | config (no secrets) |
| `/var/lib/couchfusion/control.db` | cfcp 0600 | sessions (hashed), jobs, dev session state, audit, trash index |
| `/var/lib/couchfusion/trash/<jobId>/…` | cfcp | deleted components (never unlinked) |
| `/var/lib/couchfusion/agent-out/<app>.jsonl` | cfcp | Codex JSONL (cf-agent stdout) |
| `/run/couchfusion/ts-91NN.sock` | cfcp 0600 | tailscale serve targets (identity headers trusted only here) |
| `/run/couchfusion/dev/<app>.env` | cfcp 0600 | per-start control token and origins (read by systemd) |
| `/run/couchfusion/agent/<app>.json` | cfcp 0644 | run spec read by `agent-runner` as cfagent |

## To verify on first deploy

- `cf-agent@`: `StandardOutput=append:` is opened by the service manager before `InaccessiblePaths=/var/lib/couchfusion` applies, so the agent can write its stdout while not being able to read control-plane state. Check with one generate run. If systemd rejects it, switch to `StandardOutput=journal` (the server would then need to tail `journalctl -u cf-agent@<app> -o cat`).
- `cfcp` can `systemctl start/stop/restart cf-dev@radustanciu` through polkit (`05-start.sh` exercises restart).
