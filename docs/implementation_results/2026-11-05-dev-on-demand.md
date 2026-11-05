# Dev server on demand, production always on (2026-11-05)

## How it stood

- `cf-dev@<app>` was already **not** enabled at boot. The control plane never starts it by itself: after its own restart it only re-adopts a dev unit that is still running. On hydra1, dev had simply been running since it was last started (2026-11-03 09:04).
- `cf-site@radustanciu` was enabled at boot. For a new site, only the manual `phase3/04-release.sh` enabled the unit. The control plane cannot enable units (polkit allows `cfcp` only start/stop/restart), so a site deployed only from the UI would not have come back after a reboot.

## Changes

- **Site page:** the Dev session panel is now a `DevSessionCard` (`web/components/sites/DevSessionCard.vue`).
  - It shows the status (Running / Starting… / Stopping… / Stopped / Failed), with when it started and memory use.
  - Buttons: **Start dev server**, **Stop…** (asks first: builder and dev site go offline, unsaved builder edits are lost, production keeps running) and **Restart**.
  - It polls every 2 s while starting or stopping, and every 15 s otherwise.
  - The dev site and builder links show "offline" while dev is stopped. Those links now use the page's own host, as the dev page does, so they also work through the SSH tunnel.
- **Server:** stop and restart are refused (409, with `activeJobId`) while a job that uses the dev server runs: `llm.request`, `llm.request.apply`, `component.generate`, `component.delete`, `component.restore`. Start is always allowed. This applies to the dev page buttons too.
- **Service view:** now reports `startsAtBoot`, from systemd's `UnitFileState`. The production panel shows "starts at boot", or a red notice with the `systemctl enable` command when production is not enabled.
- **Packaging:**
  - `phase3/files/cf-site@.service` gains `ConditionPathExists=/var/www/%i/current/server/index.mjs`, so an enabled site with no release yet is skipped at boot instead of restart-looping.
  - `phase3/03-units.sh` now runs `systemctl enable cf-site@$CF_APP`, and warns if `cf-dev@$CF_APP` is enabled.
- The polkit rule still names production sites explicitly (`cf-site@(radustanciu)`). A new site must be added there in its setup, which is deliberate.

## On hydra1

- Installed the new `cf-site@.service` (backup `cf-site@.service.bak-20261105`) and ran `daemon-reload`, without restarting production. The condition passes. Production has been active since 2026-11-03 11:22, with 0 restarts.
- Installed control plane `0.1.0-1ae10af-dirty202611050954`, restarted with no jobs running.
- **Checked in Canary from the site page:**
  - Stop… → confirm → Stopped within seconds. cf-dev inactive, about 570 MB freed.
  - Production unaffected throughout: same start time, HTTP 200, and the public site on the same build.
  - Start → Starting… → Running after 30 s, with the dev site at HTTP 200.
  - `cf-dev@radustanciu` stays disabled at boot; `cf-site@radustanciu` stays enabled.
- **Not tested live:** the 409 guard (it needs a running Codex job). The UI test covers how the refusal is shown.

## Tests

- Rust 80/80, including `startsAtBoot` parsing.
- Web 178/178 (new: `devSessionCard.test.ts` covers start → running, stop with confirmation, and a refused stop). The site-detail link expectation was updated for the aligned origin. Typecheck clean.
