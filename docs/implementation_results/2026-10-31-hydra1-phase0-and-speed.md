# hydra1: Phase 0–2 done, dev speed compared with hetzner1 (2026-10-31)

## Phase 0 on hydra1

- Steps 01–09 done (see `server/packaging/hydra1/phase0/README.md`).
- Step 12: CouchDB copied from hetzner1 (`12-copy-couchdb-from-hetzner1.sh`). Doc counts match: rs--content 134, rs--logins 0, analytics_radustanciu 8; `_security` copied; all 7 `cf_env_radustanciu` keys set.
- Dev env: hydra1's CouchDB now has the same `radu` admin as hetzner1, so `COUCHDB_ADMIN_AUTH` is the same value on both boxes (copied file-to-file, never printed). The other secret keys are empty on both, as ImageKit comes from `_config`.
- Step 10: `cf-dev@radustanciu` answers HTTP 200. S1: the registry updates 0.6–1.5 s after a component is added or deleted. S5: dev peak 705 MiB, 5.3 GiB still available.
- First failure: the env value was not base64 (`Auth Layer Environment Error`, CouchDB 400). It was fixed by using the same encoded `radu` value as hetzner1.
- hetzner1 `cf-dev@radustanciu` is stopped again (it was started for the measurements only).

## Speed: hydra1 vs hetzner1

Hardware: hetzner1 has 2 vCPU Skylake and 1.5 GiB available; hydra1 has 4 vCPU EPYC and 5.5 GiB available.

The test crawls the page plus every module it imports, like a browser does (6 at a time): 487–489 requests and 29 MiB uncompressed, including the builder and admin bundles.

| Measurement | hetzner1 | hydra1 |
|---|---|---|
| On the box (loopback), cold | 17.5 s | 9.9 s |
| On the box, warm | 5.0–7.3 s | 1.5–3.4 s |
| Page HTML (SSR) | 320–835 ms | 105 ms |
| From the laptop over the tailnet, 9181 | 18–24 s | 42–54 s |
| 7.5 MB apexcharts module (test file), as received | 1.7 MB gzip, 5–8 s | 7.5 MB raw, 27–60 s |
| Raw 8 MB over SSH/tailnet | 2.0 MB/s | 0.57 MB/s |
| Server upload to the internet | 49 MB/s | 47 MB/s |
| Laptop download from the internet | 9.9 MB/s | 9.9 MB/s |

Findings:

1. The server side is 2–3× faster on hydra1.
2. What you feel in the browser is dominated by the network. The laptop ↔ server path over Tailscale (direct WireGuard UDP, about 60 ms RTT) carries only 0.3–2 MB/s, while both ends can do 10–47 MB/s. This matches the UDP loss under load seen earlier on the user's network.
3. hydra1 has no compression yet: Phase 0 serves 9181 straight from `tailscale serve` to nuxt dev. On hetzner1 the control plane's slot proxy compresses (gzip/br, about 75% smaller), so hydra1 sends about 4× more bytes until Phase 1 is installed.
4. The crawl also follows lazy `import()` calls, so it is an upper bound on what a browser loads at first. For example, `apexcharts` (7.5 MB) is already lazy in `layers/analytics/admin/components/TimeChart.vue`, and the browser only fetches it when a chart mounts.

## Phase 1 + 2 on hydra1 (same day)

- Release `0.1.0-850be70-dirty202610301402` (the build hetzner1 runs; no server/web source change since) was copied with `phase1/` and `phase2/` to `/root/couchfusion-p2/`.
- Steps 00–06 ran, plus `phase2/01-install.sh --self-test`. The polkit self-test passed, `cf-registry@radustanciu` ran as cfcp, `doctor` was all ok, and `couchfusion.service` is active with every capability incl. `git-remote`. Dev was restarted through the API and is `ready`. `tailscale serve` 9180/9181 now point to the unix sockets.
- Preflight warned that `:9181` was in use: that was Phase 0's tailnet-side serve mapping, which step 06 replaced. It was harmless.
- `02-release.sh` takes the tarball path relative to the cwd; scripts run from `/`, so pass the absolute path.
- The `radu` editor user (`_users/org.couchdb.user:radu`, role `admin`, pbkdf2 hash) was copied from hetzner1 to hydra1 (from box to box, without `_rev`, never printed), so the same builder login works on both.
- The Claude browser pane cannot sign in to the framed builder (`ERR_BLOCKED_BY_CLIENT` on every framed 9181 script, so the login form never hydrates). Chrome/Canary works.
- Git remote: `phase2/02-git-remote.sh` ran (origin already matched the Forgejo URL from the laptop seed). The user made a new Forgejo token for hydra1 and filled `/etc/couchfusion/credentials/git-apps-radustanciu` with sudoedit. After `systemctl restart couchfusion`, the API fetch succeeded (main = origin/main = e9d4430, 0/0) and the push of main succeeded (authenticated, nothing changed on the remote).

## Builder load in a real browser (Chrome Canary over the tailnet)

The builder page (`https://<host>:9181/builder/`, which the workbench frames) was loaded top-level through CDP. A load counts as finished once the network has been quiet for 2 s. Each load is 742 requests and 27.4 MB decoded; with compression, 5.9 MB is transferred cold and 1.1 MB warm.

| Load | hetzner1 | hydra1 |
|---|---|---|
| Browser cache cleared | 11.5–13.4 s | **7.7 s** |
| Warm (cache kept) | 5.8–6.4 s | **4.6–5.0 s** |
| Workbench shell (9180) | 0.1–0.6 s | 0.1–0.4 s |
| First builder load after a dev start | — | 69 s (one-off) |

- The 69 s one-off happened because Vite discovered new dependencies on hydra1's first builder load (`minimark/hast`, `@noble/hashes/*`), re-bundled them and forced a reload. That also changed the `?v=` hash, so the next "warm" load re-downloaded 5.7 MB (28 s). It is stable afterwards. It happens again whenever the Vite deps cache is rebuilt (dependency changes, wiped `node_modules/.cache`).
- With the cache cleared, hydra1 is about 35% faster; warm, about 20% faster. The rest is about 742 requests over a ~60 ms tailnet link, which faster hardware cannot remove.

## Next

- Phase 3 (production) on hydra1. Once hetzner1 is retired, revoke its Forgejo token.
- Network: fix UDP handling on the user's router/network, or use a TCP path.
- Fewer dev requests: the builder loads 742 unbundled modules in dev. Pre-bundling more of the layer's dependencies (`optimizeDeps.include`) would cut both the request count and the first-load re-optimization.
