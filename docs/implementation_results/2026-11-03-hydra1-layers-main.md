# hydra1: VPS `layers` moved to `main` (2026-11-03)

## Before

`/srv/couchfusion/workspace/layers` was on `feat/merged-couchdb` (`a186f23`) with files copied by hand from the laptop (control-plane page routes, dev preload, one-year sessions) and a `bun.lock` rewritten by `bun install`. Every copied source file matched a commit on the laptop's `main` (`34fe159`, `16deae6`, `c1c72c1`); only `bun.lock` was VPS-only. The control plane showed no remote because `server.toml` only lists `apps/radustanciu` under `[[git.remotes]]`, and the box has no credential for the `layers` remote.

## What was done

1. The user pushed `main` (`f29abbd`) to git2.
2. The laptop made a bundle (`git bundle create … main ^a186f23`), copied it to hydra1 (checksum checked), removed after use.
3. As `cfcp` (umask 007, `core.hooksPath=/dev/null`): `git stash push -u` (stash `hydra1 hand-copied layer files before moving to main (2026-11-03)`), fetch the bundle into `origin/main`, `checkout main`, `merge --ff-only`, upstream `origin/main`. Working tree clean.
4. No dependency install: `package.json` files and `bun.lock` are identical in `a186f23` and `f29abbd`.
5. Dev restarted through the API; ready in ~13 s, no errors.

## Checks

- Layer tests (laptop, same commit): 727 pass, 5 fail, 3 suites fail to load (`bun:test` files); `bun test` on those: 6 pass, 3 fail. The same failures happen at `a186f23`, so none come from this update (email MJML spacing, content workbench translation menu, builder focused editor, core provisioning-policy CLI, auth login-heading).
- Layer tests on hydra1 were not run: the shared setup (`_tests/setup/content.ts`) needs CouchDB admin credentials, and the only CouchDB there is production.
- Live checks on dev (127.0.0.1:4401): `/` 200; `/admin` signed out 404; bogus password login 401; Nostr challenge 200 (cookie secret read from CouchDB works); control routes 404 without token, `pages` / `page-documents` / `pages/check` 200 (check only, nothing written); production 4501 and preview 9189 200.
- `chttpd_auth/secret` is readable with the app's admin credentials and differs from `COUCHDB_COOKIE_SECRET` in the env files. Since `19f3916` the auth layer ignores that variable; it can be removed from `radustanciu.dev.env` / `.prod.env` (sudoedit, user).

## Noticed (not caused by this update)

- `rs--content` now holds only `page-/index` (English). The 20 locale copies and `/old-homepage-2026` were deleted by one `_bulk_docs` from a tailnet client (`hydra1…:5984`, user `radu`) at 2026-11-03T07:20:57Z; `content-settings:i18n` was changed through the app on 2026-11-02T10:37Z. The app's `nuxt.config.ts` still lists 23 languages. The user confirmed the deletion was intended (English only).
- `layers/analytics/.git` (copied from the laptop on 2026-10-28, `radu:staff`) is a nested repo folder in the VPS tree.

## Undo

`sudo -u cfcp git -c safe.directory=… -C /srv/couchfusion/workspace/layers checkout feat/merged-couchdb && … stash pop`, then restart dev.

## Git remote for `layers` (same day)

- `phase2/02-git-remote.sh` takes `CF_GIT_REPO` (default `apps/radustanciu`); credential name `git-<repo with / as ->`, drop-in `20-git-credentials-<cred>.conf` (radustanciu keeps `20-git-credentials.conf`), and the `[[git.remotes]]` block is inserted after the existing ones (backup `server.toml.bak-git-<cred>`).
- hydra1: `CF_GIT_REPO=layers` → `[[git.remotes]] repo = "layers"`, `url = "https://git2.kangu.ro/radu/nuxt-apps-layers.git"`, `credential = "git-layers"`, `branches = ["main"]`. The user created a separate git2 token (repository read/write) and filled `/etc/couchfusion/credentials/git-layers` with sudoedit; control plane restarted.
- Fetch from the API succeeded (`main` = `origin/main`, 0/0); repo status shows `hasRemote`, upstream `origin/main`, push branch `main`. Push not tested (nothing to push).
- `doctor` had failed on "component registry invalid (0 components)": the app merge commit `1d15a02` combined both sides of `component-definitions.ts` while the `.json` kept one side's `typescriptSha256`. A registry rebuild (API `components/registry/rebuild`) fixed it (14 components, doctor ok); the regenerated files are uncommitted in the VPS app repo, and the laptop copy has the same mismatch until it pulls that commit.

## Deploy failed on a stale layers lockfile (2026-11-03, 11:00)

- **Symptom:** workbench Deploy failed in `cf-build@radustanciu` at `bun install --frozen-lockfile` in layers: "lockfile had changes, but lockfile is frozen". Production stayed unchanged.
- **Cause:** the lightning work (`f0af2fe`, `1340ef7`) added `@getalby/sdk` and `light-bolt11-decoder` to `layers/package.json` without updating `bun.lock`. Dev never noticed because `nuxt dev` does not run a frozen install.
- **Fix:** regenerated `layers/bun.lock` with bun 1.4.2 (the hydra1 toolchain version; the Mac has 1.1.38) and committed it as layers `b724421` (no push). Moved it to hydra1 with a git bundle and `merge --ff-only` as cfcp. The hydra1 layers checkout is now one commit ahead of git2 until `b724421` is pushed.
- **Deploy:** release `20261103T100314Z-e141468588a4` (app `e141468`), build 108 s, health 3 × 200, previous `20261103T071314Z-7cb90e4b52e4` kept for rollback. Checked `/` 200, `/admin` 404 signed out, `/api/content/pages` 200.
- **Next time:** after changing any `package.json` in layers or an app, run `bun install` and commit `bun.lock` with it; check with `bun install --frozen-lockfile` before deploying.
