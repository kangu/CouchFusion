# Commit message from workbench tasks, dev vs live, Back button (2026-11-03)

## Default commit message from workbench tasks

- **Server:** `GET /workspaces/{ws}/repos/{repo}/status` now returns `workbenchNotes` for a site's app repo (`apps/<site>`), oldest first, and omits the field when there are none. Each note has `jobId`, `type`, `completedAt`, `title` (commit-subject sized), optional `text`, and `files` (the task's files that are still uncommitted).
  - **Jobs considered:** the newest 50 successful jobs of each kind: `llm.request`, `component.generate`, `component.delete`, `component.restore`, `component-registry.rebuild`.
  - **Rule:** a job counts while at least one of its files is uncommitted and was not committed after the job finished. Per-file commit times come from one `git log -n 500 --name-only`. Partial commits and files committed elsewhere (e.g. pulled from the Mac) drop out on their own.
  - **Files per job:** `changedFiles` for requests and new components; the trash table for delete and restore; the two registry files for a rebuild.
  - **Titles:**
    - LLM request: first sentence of its summary, cut to 72 characters, or the prompt for requests without a summary.
    - New component: "Add component X", with the prompt as its text. The generate job now stores its prompt.
    - Delete and restore: "Delete component X" / "Restore component X".
    - Registry rebuild: "Rebuild the component registry".
- **Git drawer:** the commit message is pre-filled.
  - One task: its title and summary.
  - Several tasks: `<first title> (+N more)` and one note per task, `- [YYYY-MM-DD HH:MM] title` in the viewer's time zone, with the summary indented below.
  - When more tasks finish, the suggestion grows and replaces the message while it is still the previous suggestion or empty. A message you edited is never overwritten; a "Use the suggested message" link brings the suggestion back.
  - After a commit, the message resets to whatever is still uncommitted.
- **Not checked live:** the app repo was clean during testing, so no notes appeared on hydra1. Unit tests cover the selection and the drawer behaviour. The first workbench task that changes files will show it.

## Dev vs live by the Deploy button

- **Server:** `GET …/sites/{site}/prod` now returns `dev`, one entry each for the app and layers. Each entry has `liveHead`, `devHead`, `ahead`/`behind` (from `git rev-list --count`), `liveDirty`, `uncommitted`, and `changedFiles` (first 20) with `changedFileCount`. `changed` is set when the heads differ, there are uncommitted files not in the live build, or the live build had uncommitted changes that dev no longer has. When the live release was built with uncommitted changes, only files modified after the build count.
- **Production panel:** a block under Refresh / Deploy, which keeps refreshing every 15 s.
  - When dev is ahead: "Dev has changes that are not live", with a line per repo (e.g. "App: 2 new commits · 3 uncommitted files") and a Files list.
  - Otherwise: "Dev matches the live release".
- **Checked in Canary:** "matches" on hydra1 (app `33ede5a`, layers `b724421`, both clean). An empty scratch file in layers showed "Layers: 1 uncommitted file" with the file listed; it was removed straight after.

## Back button on the dev page

- The ← link in the dev bar now goes to the site's page (`/workspaces/<ws>/sites/<site>`) instead of the workspace list.
- The whole 36 × 36 square is clickable, with a bolder light arrow that turns cyan on hover or focus. Checked in Canary: a click in the square's corner opens the site page.

## Tests and release

- Rust 80/80 (new: notes selection and subjects, `git log` time parsing, drift rules, modified-after-build). Clippy clean.
- Web 175/175 (new: commit draft and drawer pre-fill, drift text). Typecheck clean.
- hydra1 runs release `0.1.0-1e923e1-dirty202611031033`. The control plane was restarted with no jobs running; the dev server stayed up.

## cli-content removed from hydra1 (2026-11-03, afternoon)

- **What it was:** the workspace's third repo, on branch `codex/canonical-registry-generator`, with no remote and one untracked note. Since `df357dd` it only forwards to `layers/content/scripts/`. Its one live use was the app's `registry:build` / `registry:watch` scripts, which Codex runs. `cf-registry@`, the dev watcher and production builds never used it.
- **Tidy-up first:** committed the note (`3a2a704`) on the Mac, fast-forwarded `main`, and brought hydra1 to the same `main` with a bundle.
- **Mix-up with `cf-cli.git`:** a git2 remote was briefly set up for cli-content against `https://git2.kangu.ro/radu/cf-cli.git`. That repo holds **cli-init** (pushed from the Mac's remote `git2`), so the workbench Push was correctly refused as non-fast-forward. Undone on hydra1: `origin`, the stray `origin/main` (cli-init objects pruned), the `[[git.remotes]]` block (`server.toml` is identical to before) and the drop-in. `/etc/couchfusion/credentials/git-cli-content` is kept but unused.
- **App (`apps/radustanciu` `8b59846`, pushed to git2, pulled on hydra1 via the control plane):** `registry:build` → `../../layers/content/scripts/generate-component-registry.mjs`, `registry:watch` → `…/watch-component-registry.mjs`. Both paths give identical output. A Mac run only changes `lastUpdatedAt` milliseconds (file mtime precision), so those were not committed.
- **hydra1:** `workspace.repos = ["apps/radustanciu", "layers"]` (backup `server.toml.bak-before-cli-content-retire`). The folder moved to `/srv/couchfusion/retired/cli-content-2026-11-03` (root 0700), not deleted. The control plane was restarted with no jobs running.
- **Checks:**
  - `bun run registry:build` as cfdev produced 14 definitions with no diff.
  - The repo list shows 2 repos, both clean. The dev server is ready (dev site 200) and the registry is ready (14 components).
  - Production shows the app ahead by the `package.json` commit only, which production does not use.
- **Still mentioning cli-content (harmless):** the `cf-build.sh` comment and the `InaccessiblePaths=-…/cli-content/.git` entries (the `-` makes a missing path fine). The Mac's cli-content stays for other apps.
