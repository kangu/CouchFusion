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

## Network diagnosis (why the tailnet is slow)

The laptop is on Wi-Fi: 5 GHz 802.11ac, −71 dBm signal / −88 dBm noise, 130 Mbit/s link rate. `tailscale netcheck` shows UDP ok, an easy NAT (direct path, no relay) and no UPnP/NAT-PMP port mapping. Pings at 5/s, idle and during a 12 MB tailnet transfer (1.15 MB/s):

| Target | Idle | During the transfer |
|---|---|---|
| Home router (Wi-Fi hop only) | 0% loss, avg 25 ms, max 136 ms | 1.7% loss, avg 86 ms, max 292 ms |
| hydra1 public IP (plain internet) | 0% loss, avg 74 ms | 3.3% loss, avg 91 ms |
| hydra1 tailnet IP (WireGuard/UDP) | 0% loss, avg 87 ms | 1.7% loss, avg 77 ms |

The loss and delay already show up on the hop to the router, and the public path suffers equally. So this is not a Tailscale/UDP problem but the Wi-Fi link (weak signal, plus queueing in the router). TCP inside the tunnel cannot go faster than about 1–2 MB/s with that loss at 60–90 ms RTT, while a nearby server (Cloudflare edge, short RTT) still reaches about 10 MB/s.

Fixes in order of effect: Ethernet or a stronger Wi-Fi position/AP; SQM (fq_codel/cake) on the router against bufferbloat; fewer dev modules (`optimizeDeps.include`). Updating the laptop's Tailscale (1.102.4 → 1.104.x) and enabling UPnP/NAT-PMP help connection stability, not speed.

### Retest after router changes (same day)

- Wi-Fi unchanged (−71 dBm, 130 Mbit/s). Idle ping to the router improved (avg 25 → 10 ms), but during downloads it still reaches avg 97–122 ms with 0–5% loss. The tailnet transfer ran at 0.27–0.93 MB/s, and Cloudflare at 3.7–10 MB/s (very variable).
- hydra1 builder in Canary: warm 4.7–5.3 s (before 4.6–5.0 s), cache cleared 11 s (before 7.7 s). No real change.
- A warm load still makes 742 requests: ~580 come from cache, but ~88 revalidations (304) and ~70 uncached modules go to the server. ES module imports are discovered level by level, so the warm time is about RTT × import depth, not bandwidth. Better Wi-Fi mostly helps cold loads. Warm loads improve with fewer round trips: deeper dependency pre-bundling (`optimizeDeps.include` for the layer packages), or fewer small source modules on the builder's startup path.

### Where the warm builder load goes (Nuxt 4.1.2, Vite 7.3.6)

The 742 requests break down as: raw `node_modules` 324 (nuxt 117, unhead 30, vue-router 28, …; versioned and immutable, so only 8 go to the server warm), layer source 237 (all go to the server; `no-cache` + ETag), app source 46, virtual/Vite modules 44, API 40, fonts/images 29, pre-bundled deps 15.

- The import waterfall is 14 levels deep (`/builder/` → entry → plugins → routes → `pages/builder/[[...target]].vue` → InlineLiveEditor → ContentAdminWorkbench → Workbench → … → minimark). Modules finish at 3.3 s; API calls and the inline preview document run until 4.7 s; everything settles at 5.4 s.
- Nuxt deliberately excludes vue, nuxt, unhead, vue-router etc. from `optimizeDeps` (chunk-conflict reloads). Those files are immutable-cached anyway, so pre-bundling them would only help cold loads.
- Debug-safe levers, from best ratio: (1) a dev-only modulepreload list for the builder route, built from Vite's module graph, which collapses the 14 waves into about 1–2 with the same files, source maps and HMR; (2) lazy-load builder panels that are not visible at start (also helps production); (3) `optimizeDeps.include` for non-core deps (cold-only gain); (4) Vite 8/Rolldown full-bundle dev mode later, once Nuxt supports it; (5) content-only editing on the production builder after Phase 3.

### Dev module preload prototype (same day)

`layers/content/utils/dev-module-preload.ts` (dev only) preloads each page's module graph in one go, from a list the page reports after each load. Same files, source maps and HMR. On hydra1 over the tailnet, a warm builder load's modules finish in 0.95 s instead of 2.4–2.7 s on a clean link, and 3–9 s instead of 14–26 s during loss bursts; the page settles about 0.5 s sooner (the rest is API calls and the inline preview). Details: `layers/content/docs/implementation_results/2026-10-31T13-00-00+0200_dev-module-preload.md`. The three layer files were copied to hydra1's workspace for the test (not committed).
