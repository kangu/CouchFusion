# LLM request and model label on the dev page (2026-11-02)

Contract: `docs/superpowers/specs/2026-11-02-llm-request-contract.md` (user decisions are listed there).

## What it does

- **Dev bar:** `LLM · <model>` shows the model every Codex run uses (New component and LLM request). The model is pinned in `server.toml` `[agent] model` (and optional `reasoning_effort`); the server passes `-m` / `-c model_reasoning_effort=…` to `codex exec`. hydra1: `gpt-5.6-sol` (the account default found in cfagent's last Codex session).
- **"LLM request" drawer** next to New component: prompt, "Page in focus" (preset from the builder's `/builder/<path>` route, list from the dev server), live run log, summary, changed files, proposed page changes with before/after, problems, "Apply N page changes" / "Discard" / "Commit files…".
- Codex may change **any file in the app** except dependency manifests/lockfiles (policy check fails the job, nothing reverted). Page changes are **only proposed**; `llm.request.apply` writes them after the user clicks Apply, through the content layer's helpers (page history kept for content edits). Revisions are checked, so a page edited in the builder meanwhile blocks the apply.
- Out-of-scope requests (servers, git, other apps, credentials) are declined by the prompt.

## Server (`couchfusion-server`)

- `src/request.rs`: jobs `llm.request` and `llm.request.apply`, routes `GET {site}/pages`, `POST {site}/requests` (Idempotency-Key), `POST {site}/requests/{job}/apply` (once per request), capability `llm-request`.
- Context for Codex is written to `apps/<app>/.couchfusion/request/` (`context.json`, `pages/NNN.json`), Codex writes `operations.json` there; the folder is removed after the run, `.couchfusion/.gitignore` is `*`, and `.couchfusion` is excluded from file snapshots and production build snapshots.
- The app dir is writable by Codex and the dev server, so every folder operation goes through directory fds without following symlinks (`mkdir_contained`, `remove_tree_contained`, `open_contained` now also `O_NONBLOCK` so a planted FIFO cannot hang the job). Tests plant symlinks to outside files/folders and check nothing outside is read or removed.
- Proposals are stored in `/var/lib/couchfusion/requests/<job>.json` (0600) and renamed to `.applied.json` / `.partial.json` after an apply. Job results carry only short operation descriptions and the check preview, never page bodies.
- Control-route answers are size-capped (`control_get_limited`, `control_post`); an HTML answer (Nuxt's catch-all page when the layer lacks the routes) is reported as "update the content layer".
- File changes for requests use a whole-app snapshot (`snapshot_all`; the component snapshot only covered `app/`, `server/`, `public/` and root files).
- Tests: server 71 (new: proposal parsing, operation descriptions, folder write/remove with symlinks, symlinked proposal refused, model flags + request prompt, `[agent]` config validation, whole-app snapshot), smoke 92/92.

## Content layer (`layers/content`, see its results note `2026-11-02T11-46-32+0200_llm-request-page-operations.md`)

Token-gated dev routes `GET /__couchfusion/control/pages`, `GET …/page-documents`, `POST …/pages/check`, `POST …/pages/apply`; logic in `server/utils/control-page-operations.ts` (operations checked in sequence against a simulated state; new component tags must be in the registry, HTML or enabled global aliases; apply re-checks and re-reads revisions before each operation). Publication logic moved to `server/utils/content-page-publication.ts`.

## Web (`cli-init/web`)

`composables/useLlmRequest.ts` (+ `pathFromBuilderRoute`, `modelLabel`), `components/dev/DevRequestDrawer.vue`, dev page wiring, styles in `assets/css/dev-session.css`, types in `types/api.ts`. Tests 146 (new `tests/llmRequest.test.ts`), typecheck clean.

## hydra1

1. Control plane `0.1.0-4d00a77-dirty202611020951` installed; `phase3/10-agent-model.sh` added `[agent] model = "gpt-5.6-sol"` (backup `server.toml.bak-agent`, doctor ok).
2. The content-layer files were copied into the VPS `layers` working copy (uncommitted there, like the earlier preload/session files); dev restarted through the API (ready in ~18 s).
3. `GET …/pages`: 2 pages (`/` with 21 locales, `/homepage-redesign-network-content`).
4. End-to-end request (proposal only, **not applied**): "set the English SEO description of this page … and replace the content of `/homepage-redesign-network-content` with the homepage". 2 pages / 22 documents handed over; Codex (gpt-5.6-sol, 349k tokens in, 320k cached, 2.3k out) proposed exactly `page.update` (seo) + `page.copyContent`; check ok with previews (`seo.description` before/after, body "7 blocks: network-hero, …" → "10 blocks: landing, phil, …"). The request folder was removed, the pages unchanged.
5. Found in that run and fixed: Codex wrote a task report into `docs/implementation_results/` (the repo's agent rules ask for one) and the changed-files list missed it. The request prompt now forbids run reports unless asked, and requests use the whole-app snapshot. The test report file was deleted and the test proposal retired (`requests/<job>.discarded.json`).
6. UI checked in the browser: dev bar `LLM · gpt-5.6-sol`, drawer opens, page list loads.

Not tested on hydra1: Apply against the live pages (only in the layer's CouchDB test harness), because these pages are the live site.

## Known limits

- "Page in focus" comes from the builder's initial route (`/builder/<path>`); navigating inside the builder without changing the URL is not followed.
- copyContent: when the source page has no fixed body paths but the target does, the target keeps its old ones (save helper behaviour).
- Apply is per operation, not atomic; a failure midway reports what was applied (`.partial.json`).
- Each request is one Codex turn (no follow-up turn yet).

## Dev page stuck on "Starting the dev server…" (same day)

- **Cause:** the site event stream numbered events from 1 in every control-plane process. A browser that reconnected after a control-plane restart resumed with `?after=<old seq>`, and both the server (`e.seq > cursor`) and the page (`seq <= lastSeq`) dropped every event of the new process, including the `ready` transition. Any control-plane restart (upgrade, config change) could leave an open dev page on a stale status. Reproduced in the user's Canary: page loaded 09:51:46, control plane restarted 09:52:05, page stayed on "Starting" while the API said `ready`.
- **Fix (server):** sequences start at boot time in ms × 1000 (`SiteHub::with_base`), so they keep growing across restarts; a cursor the process never issued (older or ahead) gets the whole buffered history. Tests in `events.rs`.
- **Fix (page):** while dev is `starting`/`stopping` the page re-reads the dev status every 3 s, so a missed event cannot leave it stuck.
- **Progress UI** (`composables/useDevStartProgress.ts`): elapsed time, a bar against the start timeout (`readyTimeoutSecs`, new field in the dev view, default 180 s), the last 6 dev log lines of this start, "Live updates are reconnecting" while the stream is down, and after 60 s with 45 s of no output "No new output for … It may be stuck" with Restart / Show log.
- **Verified in Canary:** dev restart showed the timer and live log lines (Vite built, database init), Ready after ~12 s; a control-plane restart in the middle of a dev start showed "reconnecting" and then Ready (before the fix it stayed on Starting).

## Page creation (same day)

- New operation `page.create` (`path`, `title`, `body` required; `seo`, `layout` optional; `publicationState` defaults to `draft`). The layer creates it with `saveLocalizedPageDocument(…, { isCreate: true })` (same shape and history as a builder-created page); later operations in the same proposal may translate or publish it. Every non-HTML tag in a new body must be a known component. Previews have `before: null` for every field of a new page.
- Prompt: Codex may create pages, keeps them draft unless the request asks for a public page, and bases them on an existing page of the same kind.
- hydra1 (release `0.1.0-4d00a77-dirty202611021003`, layer file synced): proposal-only request "create a draft page /llm-request-test …" → one `page.create` (hero + contact from the network components), check ok, no files changed; the test proposal was retired (`.discarded.json`), nothing applied.

## Run log: typing effect and follow-scroll (2026-11-03)

- `components/dev/DevEventLog.vue` + `composables/useTypedLog.ts` (`useTypedLog`, `useStickToBottom`, `charsPerFrame`); the LLM request drawer uses it (the New component drawer and logs drawer still use the plain list).
- New rows are typed in one after another (label at once, text at ≥6 characters per frame, ~360/s, faster when a backlog builds so it clears in ~45 frames), with a blinking caret on the row being typed. Rows already there when the log first appears show at once; reduced motion shows everything at once. Row keys are `<jobId>:<eventId>` so a new run types again.
- The log follows the newest row while you are at the bottom. Scrolling up stops following (a "↓ Latest" button appears); scrolling back to the bottom or clicking the button follows again. Growth of the content never counts as scrolling up. Wheel turns over the floating button scroll the log.
- Tests: `tests/eventLog.test.ts` (11: typing order and speed, new run, reduced motion, unmount, follow/stop/resume, growth, jump); web 163 pass, typecheck clean.
- hydra1: release `0.1.0-1e923e1-dirty202611030836` (SPA only changed). Checked in Canary with two read-only requests: rows type in with the caret, the log stays at the newest row (also with the log forced to 110px so it overflows), scrolling up holds the position and shows "↓ Latest", scrolling down over the button reaches the bottom and hides it. The Rust test `real_radustanciu_manifest_if_present` was skipped for this build: it reads the laptop's apps/radustanciu registry, which has the merge-commit checksum mismatch until the VPS registry commit is pulled.

## Task history and New task (2026-11-03)

- **Server:** the request job now stores its prompt (with page and model) in its result as soon as it starts, so a running request shows it too. New `GET {site}/requests?limit=30` lists the site's requests newest first. Each row has the prompt, page, summary, file and page-change counts, error, `proposal` (`pending`/`applied`/`partial`/`none`, from the stored proposal file) and the latest apply job. Rows carry no events or page bodies. Contract updated.
- **Drawer:** a Task / History switch.
  - While a request runs or once it finishes, the form gives way to the prompt that was sent, the log and the result.
  - **+ New task** (top) and **Start a new task** (bottom) clear it for the next prompt, on the page open in the builder. **Reuse this prompt** puts the prompt and its page back into the form.
  - History lists each request with status, time, page, prompt (two lines) and outcome ("1 file changed · 2 page changes to review"). Clicking a row opens it read-only, with its apply job.
  - Page changes still `pending` can be applied later from History; the server re-checks them against the current pages. Applied or partly applied ones show that instead of an Apply button.
  - A request still running on the server (for example after a page reload) is shown again when the drawer opens, and the builder reloads when it ends with file changes.
  - The old Discard button is gone: an unapplied proposal stays in History instead of being hidden.
- **Requests made before this change** show "Prompt not recorded" (their prompt was never stored).
- **Also fixed:** hovering a primary (dark) button made its background near-transparent while the text stayed light, so it looked blank. This affected every primary button. Primary buttons now stay dark on hover; light ones on the dev status panel stay light.
- **Tests:**
  - Rust 75/75, including the registry test that was skipped this morning and now passes again.
  - Web 169/169 (new: outcome text, history after a request, New task and Reuse, opening an applied request, resuming a running one, drawer rendering). Typecheck clean.
- **Deployed:** release `0.1.0-1e923e1-dirty202611031015` on hydra1, checked in Canary with one read-only request. Covered: the finished view, the History row with its prompt, Reuse, New task, and opening an applied request from 2 Nov.
