# Site Database Login Prefix — Implementation Result

**Completed:** 2026-07-14

## Delivered

Site creation now requires a raw two-letter lowercase ASCII database login prefix at every supported entry point. The browser wizard starts the field blank, blocks progress until it matches `^[a-z]{2}$`, previews the stored value with its trailing hyphen, and submits the raw two letters. The CLI accepts `--db-login-prefix ac`; its Bubble Tea flow collects a missing value alongside the app name and includes `Database : ac-` in review.

Application validation runs before a provisioning job starts. The value travels unchanged through the HTTP/application request and workspace provisioner. After the starter clone and Nuxt layer update, CouchFusion finds exactly one quoted `dbLoginPrefix` property in `nuxt.config.ts` and replaces only its string value with `<prefix>-`.

The config edit preserves quote style, permissions, formatting, comments, and unrelated bytes. It writes a sibling temporary file, syncs and closes it, then atomically renames it over the original. Missing, duplicate, or non-string properties fail the provisioning job and leave the partial clone available for inspection. Success emits `site.config.database_prefix` with `Database login prefix configured`.

The generated Nuxt SPA was refreshed in `internal/web/dist`, so released Go binaries include the new wizard behavior.

## Usage

```bash
couchfusion new --name shop --db-login-prefix ac
```

In the local web UI, open a workspace, choose **Sites → Create site**, and enter two lowercase letters in **Database login prefix**. Both paths write `ac-` for an input of `ac`.

## Verification

The release gate completed successfully:

```text
go test -race ./... -count=1  PASS
go vet ./...                   PASS
bun run test:run               PASS (8 files, 15 tests)
bun run typecheck              PASS
bun run generate               PASS (6 routes prerendered)
bun run e2e                    PASS (1 control-plane journey)
bun run e2e:hmr                PASS (1 HMR/session journey)
git diff --check               PASS
```
