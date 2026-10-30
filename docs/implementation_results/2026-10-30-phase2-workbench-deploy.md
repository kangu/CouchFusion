# Phase 2 — workbench polish, deployed to hetzner1 (2026-10-30)

Contract: `docs/superpowers/specs/2026-10-30-phase2-workbench-contract.md` (with stream deviations). Install: `server/packaging/hetzner1/phase2/README.md`.

## Built (three parallel streams + integration fixes)
- **Server:** component source, trash list + restore (`O_EXCL`, never overwrites, trashed copy kept), registry rebuild through a new oneshot `cf-registry@` unit running as `cfdev` (repo code never runs as `cfcp`; Phase 1's registry fallback moved there too), read-only services list + journald logs (allowlist, redacted incl. `AuthSession=`), per-subscriber `dev.url` in site SSE, Codex prompt skips typecheck/build/lint/tests, token usage recorded, reload-guard marker file. 56 unit tests, 81/81 smoke.
- **SPA:** Components drawer (Registry: status, rebuild, list, Source, Delete; Trash: restore), source viewer (Escape closes it), "View source" in the delete modal, token usage in the agent drawer, Services page with logs, iframe origin guard. 127 tests, typecheck clean.
- **Content layer:** agent reload guard — while `COUCHFUSION_AGENT_MARKER` exists, Vite/Nuxt full reloads and component HMR for `app/components/content/**` and the registry files are held back (wraps the HMR channel; proven: 19 messages suppressed, 0 reloads; no-op without the env).

## Integration fixes made while verifying
- The guard marker is now held by **every** component job (generate, delete, restore, rebuild), not just generate.
- Jobs keep the marker until the registry files have been quiet for 4 s (max 15 s), so the watcher's queued second run is covered.
- Parent bridge: a `reload.ack {reloading:true}` from the builder replaces the 3 s force-reload fallback with a 90 s safety net. On this box the builder takes > 3 s to come back, so the fallback was causing a second load every time.

## Verified in the browser over the tailnet (dev-browser, signed in)
| Feature | Result |
|---|---|
| Services page | 9 units with state, since, memory, user; 10 s refresh; logs (journald, 200 lines) |
| Components drawer | Registry (14), status `ready`; Source viewer (path, sha, size, numbered lines) |
| Rebuild registry | `cf-registry@radustanciu` ran as cfdev (~2.9 s CPU, ~115 MB) |
| Trash → restore | File back, registry 15, entry "Already restored", builder focused on it |
| Generate (drawer) | **57 s** end to end (was ~205 s), token usage shown (307k in / 284k cached / 2.5k out), builder reloaded only at the end |
| Builder reloads per job | **1** for restore and delete (was 3–5): one `/builder/` load + its preview |

Smoke components (`ZzSmokeBadge`, `ZzUiSmoke`, `ZzGuardProbe`) are in trash; registry back to 14.

## Notes / follow-ups
- `[services]` is not in hetzner1's `server.toml`; the built-in default list applies (same units).
- The VPS repos (`layers`, `apps/radustanciu`) carry the synced files but not the laptop commits; sync git history before editing on the VPS in earnest.
- Codex still reads ~300k tokens per run (mostly cached); the skill/prompt could be slimmed further.
