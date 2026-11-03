# LLM Request on the dev page (contract, 2026-11-02)

A generic prompt next to "New component": the user describes a change in words; Codex may change app files and **proposes** page operations; the user reviews the proposal and clicks Apply. Also: the dev bar shows the model used for LLM work.

User decisions (2026-11-02):

- Page changes are **reviewed, then applied** (dev and production share CouchDB, so an applied change is live at once). Page history keeps the previous version.
- Page operations in v1: edit page content, move/rename a route, title / SEO / publication state. No deletion.
- Added the same day: **create pages** (`page.create`), draft by default.
- "Replace a route with another one" = **copy content**: `/a` keeps its URL but gets `/b`'s body.
- File edits: **any file under `apps/<app>/` except dependency manifests/lockfiles** (same policy check as generate). File edits are not reviewed before they land (dev only until a deploy; git shows them).

## Why page operations go through the dev server

Codex runs as `cfagent` in its sandbox, network off, and nft blocks cfagent from CouchDB. The dev server (cfdev) can reach CouchDB and already has the content layer's save/rename/history code. So:

1. the control plane reads page context from the dev server (token-gated control routes) and writes it into the app dir for Codex to read;
2. Codex writes its proposal to a file;
3. the control plane validates it through the dev server (`check`) and stores the proposal in the job result;
4. Apply sends it to the dev server (`apply`), which re-checks revisions and writes with the layer's own helpers.

## Request folder in the app dir

`apps/<app>/.couchfusion/request/` (created by cfcp before the run, deleted after it, setgid group `cfsrc`, so cfagent can read and write):

| File | Written by | Content |
|---|---|---|
| `../.gitignore` | control plane | `*` (the whole `.couchfusion/` folder is ignored by git, including the `.gitignore`) |
| `context.json` | control plane | `{ currentPath: string\|null, defaultLocale, locales: string[], pages: PageSummary[] }` |
| `pages/<n>.json` | control plane | one `PageDocuments` per page (see below); `context.json` maps path → file |
| `operations.json` | Codex | `Proposal` (below). Missing file = no page operations |

`.couchfusion` is excluded from the file-change snapshot, from production build snapshots and from component scans.

## Control routes (layers/content, dev only, header `x-couchfusion-control-token`, 404 without it)

All responses `Cache-Control: no-store`, JSON.

### `GET /__couchfusion/control/pages`

```json
{
  "defaultLocale": "en",
  "locales": ["en", "ro"],
  "pages": [
    { "path": "/about", "title": "About", "publicationState": "published",
      "locales": ["en", "ro"], "redirect": null, "updatedAt": "2026-…" }
  ]
}
```

`pages` are current page documents only (no `oldpage-*` history), one entry per base path; `redirect` is `{ "to": "/x", "status": 301 }` for redirect pages, else `null`. Sorted by path.

### `GET /__couchfusion/control/page-documents?path=/about` (or several `path=` params, or `all=1`)

```json
{ "pages": [ { "path": "/about", "documents": [
  { "locale": "en", "id": "page-/about", "rev": "12-abc", "title": "About",
    "seo": { "title": "…", "description": "…", "image": null },
    "publicationState": "published", "layout": { … }, "body": { "type": "minimal", "value": [ … ] } }
] } ] }
```

### `POST /__couchfusion/control/pages/check`

Request `{ "operations": Operation[], "baseRevs": { "<docId>": "<rev>" } }`. Validates without writing. Response:

```json
{ "ok": true,
  "problems": [ { "index": 0, "message": "Unknown component PricingTabel in body" } ],
  "preview": [ { "index": 0, "op": "page.update", "path": "/about", "locales": ["en"],
                 "changes": [ { "field": "title", "before": "About", "after": "About us" },
                              { "field": "body", "before": "5 blocks: HeroBanner, …", "after": "6 blocks: …, PricingTable" } ] } ] }
```

`ok` is `problems.length === 0`. Problems: unknown op, missing/invalid fields, page not found, target exists (move), redirect page as source, unknown component tags in a body (registry ids, PascalCase aliases, primitives and global aliases are allowed), body over 1 MB, a document whose current rev differs from `baseRevs` ("changed since the request ran").

### `POST /__couchfusion/control/pages/apply`

Same request. Runs `check` first; any problem → `409 { ok: false, problems }` and nothing is written. Otherwise applies the operations in order with the layer's helpers (page history is saved as for a builder save) and answers `{ ok: true, applied: [ { index, path, documents: [ { id, rev } ] } ] }`. A failure after some writes answers `500 { ok: false, applied: [...], failed: { index, message } }`.

## Operations (`operations.json`)

```json
{
  "summary": "One short paragraph for the user: what was changed and what is proposed.",
  "operations": [
    { "op": "page.create", "path": "/new-page", "title": "New page",
      "seo": { "title": "…", "description": "…", "image": null },
      "body": { "type": "minimal", "value": [ … ] }, "layout": { … }, "publicationState": "draft" },
    { "op": "page.update", "path": "/about", "locale": "en",
      "title": "About us", "seo": { "title": "…", "description": "…", "image": null },
      "body": { "type": "minimal", "value": [ … ] }, "layout": { … } },
    { "op": "page.move", "from": "/old", "to": "/new", "keepRedirect": true },
    { "op": "page.copyContent", "from": "/b", "to": "/a" },
    { "op": "page.publication", "path": "/x", "state": "draft" }
  ]
}
```

- `page.create`: new page in the default locale; `title` and `body` required, `seo`/`layout` optional, `publicationState` defaults to `draft`. Created with the layer's create path (same document shape as a builder-created page). Problems: path exists (also via earlier operations), invalid/reserved path, unknown components (every non-HTML tag must be known). Later operations may update or publish it. Apply refuses (409, nothing written) if the page appeared meanwhile.
- `page.update`: at least one of `title`, `seo` (partial allowed), `body`, `layout`. `locale` omitted = default locale. Saved like a builder save of that locale (`saveLocalizedPageDocument`, history kept).
- `page.move`: `renameContentPageUrl` (all locales; `keepRedirect` leaves a redirect at `from`).
- `page.copyContent`: for every locale document of `to`, copy `body` and `layout` from the same locale of `from` (falling back to `from`'s default locale). `to` keeps its URL, title, SEO and state. Both must exist; `from` must not be a redirect.
- `page.publication`: `published` | `draft` (same as the builder's publication toggle).
- At most 50 operations and 5 MB per proposal (checked by the control plane before `check`).

## Control-plane API (couchfusion-server)

Capability `llm-request`.

- `GET {site}/pages` → proxies `GET /__couchfusion/control/pages` (409 when dev is not ready).
- `POST {site}/requests` `{ prompt, currentPath? }`, header `Idempotency-Key` → job `llm.request`. Needs dev ready (409). Holds the site lease and the agent marker. Steps: fetch `pages` + `page-documents?all=1` → write the request folder → run Codex (`cf-agent@`, same sandbox, request prompt) → file changes, dependency policy, restart reasons, registry settle when components changed → read `operations.json` (optional) → `check` → result.
- `POST {site}/requests/{jobId}/apply` → job `llm.request.apply` (site lease; at most one apply per request: idempotency key `apply:<jobId>`). Calls `apply` with the stored operations and base revs, then asks the builder to reload.
- `GET {site}/requests?limit=30` (added 2026-11-03) → `{ requests: [...] }`, newest first (limit 1–100, default 30). Each row: `id`, `status`, `createdAt`, `completedAt`, `prompt` (stored in the request result since 2026-11-03; `null` for older requests), `currentPath`, `summary`, `changedFiles` and `operations` (counts), `error` (message), `proposal` (`pending` = stored and waiting for Apply, `applied`, `partial`, `none`) and `applyJob` (`{id, status}` of the latest apply, or `null`). No events or page bodies; open a request with `GET /jobs/{id}`. The request job sets its result (prompt, page, model) when it starts, so a running request shows its prompt too.

Request job result:

```json
{ "summary": "…", "changedFiles": ["app/components/content/Hero.vue"], "needsRestart": null,
  "operations": [ … ], "baseRevs": { … }, "check": { "ok": true, "problems": [], "preview": [ … ] },
  "currentPath": "/about", "usage": { … }, "threadId": "…", "model": "gpt-5.6-sol" }
```

## Model

`server.toml` `[agent]` (optional): `model = "gpt-5.6-sol"`, `reasoning_effort = "high"`. When set, every Codex run (generate and request) gets `-m <model>` and `-c model_reasoning_effort="<effort>"`. Bootstrap returns `agent: { model, reasoningEffort }` (`null` = Codex's account default; the UI shows "Codex default"). The dev bar shows `LLM: <model>` (with effort when set).
