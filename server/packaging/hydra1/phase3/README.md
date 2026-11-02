# hydra1 Phase 3 — production for radustanciu

Runs the production site on hydra1 next to dev (spec §7.2, §7.3, §9.4). Builds happen on the box from a snapshot of the VPS workspace; releases are switched with a symlink and rolled back automatically when the health check fails. Dev and production share hydra1's CouchDB, so builder content edits are live as soon as they are saved; code changes go live with a deploy.

Every step is idempotent and lists its undo command at the top. CouchDB, Caddy, dev, the control plane and the other services are not restarted.

## Layout

| What | Where | Owner / mode |
|---|---|---|
| Production unit | `cf-site@radustanciu.service` → `node /var/www/radustanciu/current/server/index.mjs` on `127.0.0.1:4501` | runs as `cf-site-radustanciu`, read-only releases, outside `couchfusion.slice`, `MemoryLow=150M`, `MemoryMax=900M` |
| Build unit | `cf-build@radustanciu.service` (oneshot) → `/usr/local/libexec/couchfusion/cf-build.sh /var/cache/cf-build/radustanciu/next` | runs as `cfdev` in `couchfusion.slice`, no secrets (placeholder `COUCHDB_ADMIN_AUTH`), `NODE_OPTIONS=--max-old-space-size=1400`, `MemoryMax=2300M`, `CPUQuota=180%` |
| Snapshots | `/var/cache/cf-build/radustanciu/<UTC ts>/{apps/radustanciu,layers,cli-content}` + `release.json`; `next` → the one being built | `cfcp:cfsrc 2770` (cfcp copies, cfdev builds); keep 2 |
| Releases | `/var/www/radustanciu/releases/<UTC ts>-<sha12>[-dirty]/` (the `.output` + `release.json`); `current` → `releases/<rel>` | `cfcp:cf-site-radustanciu 2750`, files `g+r`, no world access; keep 5 |
| Runtime env | `/etc/couchfusion/env/radustanciu.prod.env` | `root:cf-site-radustanciu 0640` (cfcp cannot read) |
| Build env | `/etc/couchfusion/env/radustanciu.build.env` (non-secret `NUXT_PUBLIC_*`, `PUBLIC_SITE_URL`) | `root:cfdev 0640` |
| polkit | `cfcp` may also start/stop/restart `cf-build@*` and `cf-site@radustanciu` | |
| nft guard | `4501-4509` added to the protected loopback ports (cfdev/cfagent cannot reach production) | |
| Slice | `couchfusion.slice.d/20-hydra1.conf`: `MemoryHigh=3G`, `MemoryMax=3584M` (dev and a build together) | |

## Steps (hydra1, root, in /root/couchfusion-p3/phase3)

| Step | What | Changes the box? |
|---|---|---|
| `00-preflight.sh` | control plane up, dev env, toolchain, ports 4501/9189 free, memory, disk, Caddy | no |
| `01-site-user.sh` | system user `cf-site-radustanciu` | yes |
| `02-env.sh` | prod and build env from the dev env, box to box (values never printed); adds `NITRO_HOST/PORT`, `PUBLIC_SITE_URL`, and the `NUXT_COUCH_*` runtime overrides for values baked as placeholders at build time | yes |
| `03-units.sh` | units, build script, polkit rule, tmpfiles, slice size, nft ports; polkit self-test | yes, nothing started |
| `04-release.sh deploy` | admission (≥1.2 GiB free memory, ≥3 GiB disk) → snapshot as cfcp → build as cfdev → copy release as cfcp → switch `current` → restart through polkit as cfcp → 3 × `GET /` < 500 → roll back on failure → prune | yes |
| `04-release.sh rollback [<rel>]` | switch to the previous (or named) release, restart, check | yes |
| `04-release.sh status` | releases, current, unit state | no |
| `05-preview.sh` | `tailscale serve` `https://hydra1.tailb7471b.ts.net:9189` → `127.0.0.1:4501` (tailnet only) | yes |
| `06-caddy.sh` | `/etc/caddy/sites/radustanciu.caddy` (`radustanciu.ro` → 4501; `www.radustanciu.ro`, `radustanciu.com`, `www.radustanciu.com` → 301 to `radustanciu.ro`), all with `tls internal`; Caddyfile becomes `skip_install_trust` + `import sites/*.caddy` (stock file backed up); validate, reload, local test | yes, reloads Caddy |
| `07-backups.sh` | nightly CouchDB backup: `couchdb-backup.timer` (03:30) → every database (`_all_docs` with docs and inline attachments) + `_config`, gzip, `/var/backups/couchdb/<UTC date>/` (root 0700/0600) with `manifest.json`, 14 days kept; runs one now and test-restores every non-system database into `restoretest-*` scratch copies (doc counts compared, then deleted) | yes |
| `08-offbox-pull.sh "<pubkey>"` | off-box copy: system user `cfbackup` (no password) whose only SSH key is forced to `tar -C /var/backups/couchdb -cf - .` with `restrict` (no shell, PTY or forwarding) and `from=` tailnet addresses; dumps become `root:cfbackup` 0750/0640; self-test (cfbackup reads dumps, not the prod env) | yes |
| `09-control-plane.sh` | deploy/rollback from the control UI (needs a `couchfusion-server` release with `prod-releases`, installed first with `phase1/02-release.sh`): drop-in `couchfusion.service.d/30-phase3.conf` (write access to `/var/cache/cf-build`, `/var/www/radustanciu`), `[sites.prod]` + `cf-site@`/`cf-build@` in `[services]` in `server.toml` (backup `server.toml.bak-phase3`), `doctor` with the new config (restores on failure), restart of the control plane | yes, restarts `couchfusion` |
| `10-agent-model.sh [model] [effort]` | pins the Codex model for every LLM run (New component, LLM request) in `server.toml` `[agent]` (default `gpt-5.6-sol`, the account default on 2026-11-02; backup `server.toml.bak-agent`), `doctor` (restores on failure), restart of the control plane. The dev bar shows `LLM · <model>` | yes, restarts `couchfusion` |

`04-release.sh` and the control plane's `release.deploy` / `release.rollback` jobs (Services page → Production) do the same steps with the same users, units and polkit rights; use either. The UI asks for the site id typed as confirmation, streams the build log, and shows the releases with "Roll back to this".

## Notes

- Local image uploads (`/api/content/local-images`) write into `public/` under the working directory. Production releases are read-only, so that upload is a dev-only feature; ImageKit uploads work in both.
- `nuxt build` is called directly, which skips the app's legacy `postbuild` (`git_output.sh`, dist repo). The old flow is retired after the DNS cut-over.
- TLS behind Cloudflare (user decision, 2026-10-31): Caddy's self-signed `tls internal` certificate on the origin, Cloudflare SSL mode **Full** (not "Flexible": Cloudflare would call port 80 and Caddy's HTTPS redirect would loop).

## Backups and restore

- Backup now (before risky work): `systemctl start couchdb-backup` (overwrites today's folder).
- Restore one database, keeping revisions: `set -a; . /etc/couchfusion/env/radustanciu.prod.env; set +a; /usr/local/libexec/couchfusion/couchdb-restore.sh /var/backups/couchdb/<date>/rs--content.json.gz <target-db>`. It refuses a non-empty target unless `--into-existing` is passed; restore into a new name first, check, then swap.
- `_config.json.gz` holds the node config including `chttpd_auth/secret` (keeps login cookies valid on a rebuilt server) and admin hashes: root only, never copy it anywhere shared.
- Off-box copy on the laptop (`laptop/`): key `~/.ssh/id_hydra1_backup`, script `~/Library/Application Support/couchfusion/pull-couchdb-backups.sh`, LaunchAgent `ro.kangu.couchfusion.hydra1-backup` daily at 10:00 local (runs on wake if missed), copies in `~/Backups/hydra1-couchdb/<date>/` (0700/0600, 60 days), log `~/Library/Logs/couchfusion-hydra1-backup.log`. It fails loudly (macOS notification) when the pull fails or the newest backup is more than 2 days old. Run by hand: `launchctl kickstart gui/$(id -u)/ro.kangu.couchfusion.hydra1-backup`.
- Install the laptop side: `mkdir -p ~/Library/Application\ Support/couchfusion && install -m 0755 laptop/pull-couchdb-backups.sh ~/Library/Application\ Support/couchfusion/`, `sed "s#__HOME__#$HOME#g" laptop/ro.kangu.couchfusion.hydra1-backup.plist > ~/Library/LaunchAgents/ro.kangu.couchfusion.hydra1-backup.plist`, `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/ro.kangu.couchfusion.hydra1-backup.plist`. Undo: `launchctl bootout gui/$(id -u)/ro.kangu.couchfusion.hydra1-backup` and remove those files.

## Cut-over (Cloudflare dashboard, by the user)

1. SSL/TLS → Overview: mode **Full** for `radustanciu.ro` (and for `radustanciu.com` if it is its own zone).
2. DNS `radustanciu.ro`: `A @ → 161.97.156.140`, proxied (orange cloud). Remove any old `AAAA @`. `www` keeps pointing at the apex.
3. DNS `radustanciu.com`: `@` and `www` → `161.97.156.140` (or CNAME to `radustanciu.ro`), proxied.
4. Check: `curl -sI https://radustanciu.ro/` shows `via: 1.1 Caddy` from hydra1 (`journalctl -u caddy`), then remove the preview (`tailscale serve --https=9189 off`).
5. Keep the old server a few days, then retire the dist-git flow (`git_output.sh`, `git-persistent/radustanciu`, the `postbuild` script).

## LLM request (2026-11-02)

The dev page has an "LLM request" drawer next to "New component" (contract `cli-init/docs/superpowers/specs/2026-11-02-llm-request-contract.md`). Codex may change app files (not dependencies) and proposes page operations (edit content, move with redirect, copy content from another page, title/SEO, publication state); page operations are written only when the user clicks Apply, through the content layer's token-gated control routes on the dev server (`/__couchfusion/control/pages*`), with page history kept. Needs a content layer with those routes in the VPS workspace and a dev restart after updating it. During a run the control plane writes the page context to `apps/<app>/.couchfusion/request/` (git-ignored, removed after the run, excluded from build snapshots).
