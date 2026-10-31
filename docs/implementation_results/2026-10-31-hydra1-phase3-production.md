# hydra1 Phase 3: production for radustanciu (2026-10-31)

Production now runs on hydra1 next to dev, built on the box. Scripts: `server/packaging/hydra1/phase3/` (README there). Not yet public: the Caddy site and the DNS cut-over are the last step.

## What runs where

- `cf-site@radustanciu` (user `cf-site-radustanciu`) serves `/var/www/radustanciu/current` on `127.0.0.1:4501`. Read-only releases, outside `couchfusion.slice`, `MemoryLow=150M`, `MemoryMax=900M`. Memory after start: ~58 MB.
- `cf-build@radustanciu` (user `cfdev`, no secrets) builds a snapshot of the VPS workspace: `bun install --frozen-lockfile` in `layers` and the app, then `nuxt build` (skips the legacy `postbuild`/dist repo).
- Dev and production share hydra1's CouchDB (spec D1). The content on hydra1 is the production content (user decision, 2026-10-31: no merge from the old server).
- Tailnet preview: `https://hydra1.tailb7471b.ts.net:9189/` → `127.0.0.1:4501` (`05-preview.sh`; remove after the cut-over).

## Install log

1. `00-preflight.sh`: all ok (MemAvailable 5160 MiB, 129 GB free).
2. `01-site-user.sh`: `cf-site-radustanciu` (uid 992).
3. `02-env.sh`: `radustanciu.prod.env` (root:cf-site-radustanciu 0640, 14 keys) and `radustanciu.build.env` (root:cfdev 0640), from the dev env box to box, values never printed. Adds `NITRO_HOST/PORT`, `PUBLIC_SITE_URL=https://radustanciu.ro` and the `NUXT_COUCH_URL` / `NUXT_COUCH_ADMIN_AUTH` runtime overrides (the auth layer bakes `couchUrl`/`couchAdminAuth` into runtimeConfig at build time, where only a placeholder exists).
4. `03-units.sh`: units, build script, polkit (`cf-build@*`, `cf-site@radustanciu` for cfcp; ssh still refused), tmpfiles, `couchfusion.slice` raised to `MemoryHigh=3G`/`MemoryMax=3584M` on hydra1 (8 GB), nft guard now also protects `4501-4509` (cfdev → 4501 blocked).
5. `04-release.sh deploy`:
   - First try: `nuxt build` died with a V8 heap OOM at ~795 MB (Node derives its heap limit from the unit's cgroup). Fix: `NODE_OPTIONS=--max-old-space-size=1400`, unit `MemoryMax=2300M`.
   - Second try: build ok in 46 s, but the script stopped after copying the release: on the very first deploy there is no `current` link, and `readlink` failing under `pipefail` ended it. Fixed (`current_release` tolerates a missing link); added `activate <rel>`, which switched to the built release: 3 × HTTP 200.
   - Third run, end to end: snapshot, build 41 s, release, switch, 3 × 200.

## Checks

- Routes on 4501: homepage 200 with the real title, `/sitemap.xml` 200, `/builder/` 302 to login, `/api/login` 401 without a session, unknown page 404. Log: all layers initialize, ImageKit from `cf_env_radustanciu`, analytics storage up.
- Production renders exactly like dev (same font settings, same layout). It differs from the live `radustanciu.ro` because hydra1's workspace has newer code than the old build (e.g. base font Lato instead of Inter, hero text position). Going live publishes what dev shows today; the release is marked `-dirty` because the app repo has uncommitted changes.
- Spec exit checks, by script (the UI part is still to build):
  - normal deploy: ok;
  - failing health: a release whose server exits at once was activated; the health check failed and the script switched back to the good release on its own (`prod.rolled_back`);
  - manual rollback: `rollback` to the previous release and `activate` back to the latest, both health-checked.

## Caddy (cut-over prep, `06-caddy.sh`)

- TLS behind Cloudflare: user chose Caddy's self-signed `tls internal` + Cloudflare SSL mode "Full". Hostnames: `radustanciu.ro` (served), `radustanciu.com` and `www.radustanciu.com` (301 to `radustanciu.ro`); `www.radustanciu.ro` keeps its existing redirect so it doesn't break when DNS moves.
- Today `www.radustanciu.ro` answers 302 to the apex and `radustanciu.com` doesn't answer over HTTPS at all.
- Checks: on the box 200 / 301 / 301 / 301 and `http://` → 308; from the laptop to the public IP `161.97.156.140`: `radustanciu.ro` 200 in 0.5 s with the real title, `radustanciu.com` 301. ufw already allowed 80/443.

## Backups (`07-backups.sh`)

- `couchdb-backup.timer` every night at 03:30 (±10 min, catches up after downtime) runs `couchdb-backup.service` (root, loopback only): every database, `_all_docs?include_docs=true&attachments=true`, gzip, plus `_config`, into `/var/backups/couchdb/<UTC date>/` with a `manifest.json` (doc counts, CouchDB version); 14 days kept. First run: 7 databases, 1.9 MB.
- Restore: `couchdb-restore.sh <dump> <db>` (`_bulk_docs` with `new_edits=false`, batches of 500; refuses a non-empty target unless `--into-existing`).
- Tested: every non-system database restored into a scratch copy with matching doc counts (analytics 10, raw 105, photon 1, rs--content 134, rs--logins 0); `rs--content` restored again with identical ids and revisions; scratch databases deleted.

## Off-box copy to the laptop (`08-offbox-pull.sh`, `laptop/`)

- hydra1: user `cfbackup`; its only key is forced to stream `/var/backups/couchdb` as a tar (`restrict`, tailnet `from=`). Dumps are now `root:cfbackup` 0750/0640 (the backup script sets the group each night).
- Laptop: new key `~/.ssh/id_hydra1_backup`, LaunchAgent `ro.kangu.couchfusion.hydra1-backup` daily at 10:00 (runs on wake if missed) into `~/Backups/hydra1-couchdb/` (60 days, 0700), notification on failure or when the newest backup is older than 2 days.
- Tested: first pull ok (3.9 MB, every gzip valid, every dump's row count matches its manifest); with the backup key a remote command still only returns the tar, a shell is refused ("PTY allocation request failed"), port forwarding to CouchDB is refused ("administratively prohibited"); the user's normal hydra1 key is refused for `cfbackup`; cfbackup cannot read the prod env file. (Port 22 is not reachable on the public IP anyway: ufw allows SSH only on tailscale0.)

## Deploy and rollback in the control UI

- Server (`src/release.rs`): `GET /api/v1/workspaces/{ws}/sites/{site}/prod` (unit state, releases newest first with app/layers commits and dirty flags, current, admission, active job), `POST …/prod/deploy {confirm}` → job `release.deploy`, `POST …/prod/rollback {releaseId, confirm}` → job `release.rollback`. Both need the site id typed as `confirm` and take the site lease (no deploy during a component job). Capability `prod-releases`. Config `[sites.prod]` (units pinned to `cf-site@<id>`/`cf-build@<id>`, port, dirs, keep counts, build timeout, admission thresholds, probe path). The routes are site-scoped instead of the spec's `/services/{unit}/deploy`, like the dev routes.
- Deploy job: admission (409 if memory/disk short) → snapshot in Rust (same excludes as the script; dirs `0770`/files `0660`, group from the setgid parent) with `release.json` (each repo's HEAD + dirty) → `cf-build@` via polkit, build log streamed into the job (per-file size listings filtered) → copy `.output` to `releases/<ts>-<sha12>[-dirty]` (dirs `0750`, files `0640`, group `cf-site-radustanciu`) → switch `current` atomically → restart `cf-site@` → 3 × HTTP < 500 within 60 s → on failure switch back to the previous release and fail with the reason → prune (keep 5 releases, never current/previous; 2 snapshots).
- UI (Services page → Production panel): unit state and memory, Deploy… with typed confirmation and a note that uncommitted changes ship, live job log, error or "is live" outcome, releases table with commits and "Roll back to this". A release job already running when the page opens is picked up; another site job blocks the buttons. Idle `cf-build@`/`cf-registry@` units no longer count as "not running".
- Tests: server 64 (5 new: ids, snapshot excludes and modes, release copy, switch/list/prune, admission), web 140 (6 new for the production composable + service tones); typecheck clean.
- Problems hit on hydra1 and fixed:
  1. The unit runner's own allow-list refused `cf-build@`/`cf-site@` ("refusing to manage unit") → added both families (polkit still limits `cf-site@` to radustanciu).
  2. `couchfusion.service` has `RestrictSUIDSGID=yes`, so a chmod that repeats the inherited setgid bit is refused; the fallback chmod then cleared it, new subdirectories got group `cfcp`, and cfdev could not read the snapshot ("no app in …"). Directories are now created with the right mode and never chmodded when they carry the inherited setgid bit.
  3. The failure log tail showed earlier runs' lines → `journal_since` limits it to this build.
- Verified from the UI in Chrome Canary over the tailnet:
  - deploy: snapshot, build 43 s, release, switch, restart, 3 × 200, done in 51 s; pruned 1 snapshot;
  - manual rollback to the previous release: 7 s, health-checked; and forward again to the newest;
  - a planted broken release (server exits at once) chosen with "Roll back to this": health check failed after 60 s, the control plane switched back on its own and the good release answered 200 again; the test release was removed afterwards. (A failing *deploy* uses the same switch-back code; it was not provoked from the UI because that would need breaking the workspace code.)
- Release files are readable by `cf-site-radustanciu` (setgid kept, group `cf-site-radustanciu`).

## Still to do

1. **DNS cut-over** (user, Cloudflare): SSL mode "Full", `radustanciu.ro` (and `radustanciu.com`) → `161.97.156.140` proxied, old `AAAA` removed. Then remove the 9189 preview; keep the old server a few days and retire `git_output.sh`, `git-persistent/radustanciu` and the `postbuild` script.
4. `lnvps2` reported a changed SSH host key during the search for the old server; it was not bypassed. Check whether that server was reinstalled.
