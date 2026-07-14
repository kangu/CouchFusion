# Site Database Login Prefix

**Date:** 2026-07-14

**Status:** Approved

## Goal

Require a two-letter database login prefix whenever CouchFusion creates a site, then replace the starter's `runtimeConfig.dbLoginPrefix` value in `nuxt.config.ts` after cloning. A user-provided value such as `ac` produces `dbLoginPrefix: 'ac-'`.

## Scope

The prefix applies to new site provisioning through both the web workbench and the existing CLI path. It is not a workspace default and does not modify existing sites.

The user supplies exactly two lowercase ASCII letters. The trailing hyphen is owned by CouchFusion and is never entered or transmitted by the browser as part of the field value.

## Wizard experience

The first site-wizard step adds a blank required field labeled `Database login prefix` beneath the site name. Its helper text explains that exactly two lowercase letters are required and that CouchFusion adds the trailing hyphen.

The browser input uses `minlength="2"`, `maxlength="2"`, `pattern="[a-z]{2}"`, and `required`. The wizard cannot advance from the first step unless both the site name and prefix are valid.

The review step displays the generated value with its hyphen, for example `ac-`.

## Request flow

The raw two-letter value is named `dbLoginPrefix` in JSON and TypeScript, and `DBLoginPrefix` in Go:

```text
SiteWizard
  -> page submit payload
  -> POST /api/v1/workspaces/:workspaceId/sites
  -> app.CreateSiteRequest
  -> workspace.CreateSiteRequest
  -> post-clone nuxt.config.ts editor
```

Browser validation is a usability layer only. The application service validates the value before starting a job, and the workspace provisioning layer validates it again before cloning or writing files. Invalid values include blank strings, uppercase letters, digits, punctuation, and values whose length is not exactly two bytes.

The CLI `new` command adds `--db-login-prefix`. In the Bubble Tea flow, the name step also contains a blank two-character prefix input and cannot advance until it is valid. In non-TUI mode, omitting or invalidating `--db-login-prefix` produces an input error before a job starts. Both paths pass the raw two-letter value through the shared provisioning request; web and CLI creation may not diverge in validation or generated output.

## Post-clone mutation

Site provisioning already edits cloned `package.json`, layer parameters, `nuxt.config.ts` extends, metadata, and module setup. Database-prefix mutation runs in the same post-clone sequence after the existing Nuxt extends update and before metadata is written.

The editor opens only `<target>/nuxt.config.ts` and recognizes quoted property assignments shaped like:

```ts
dbLoginPrefix: 'ns-'
dbLoginPrefix: "ns-"
```

It must find exactly one assignment. It preserves the existing quote style, whitespace, comments, and all unrelated bytes, replacing only the string contents with `<prefix>-`.

The write is atomic: create a temporary file beside `nuxt.config.ts`, apply the original file permissions, write and sync the content, close it, then rename it over the original. Cleanup removes the temporary file after every failure.

After a successful update, provisioning emits the event:

```text
code: site.config.database_prefix
level: success
message: Database login prefix configured
```

## Failures

Provisioning fails without guessing when:

- `nuxt.config.ts` is missing or unreadable;
- no `dbLoginPrefix` assignment exists;
- more than one assignment exists;
- the existing assignment is not a quoted string; or
- the atomic write cannot be completed.

The error identifies the configuration problem for logs and tests without exposing credentials or unrelated file content. The asynchronous web job continues to present the existing safe `Site provisioning failed` message at the API boundary.

The cloned directory may remain after a post-clone failure, matching current provisioning behavior. CouchFusion does not delete the partially configured site automatically.

## Testing

Automated coverage will verify:

- wizard blank initial state, required/pattern attributes, first-step blocking, submission payload, and review rendering;
- JSON decoding and propagation across web, application, and workspace request types;
- rejection of blank, uppercase, numeric, punctuation, short, and long prefixes before the job or clone begins;
- successful single-quoted and double-quoted replacement;
- preservation of quote style, permissions, and every unrelated byte;
- errors for missing files, missing properties, duplicate properties, and non-string values;
- the success event after mutation;
- the existing site-creation flow with an explicit prefix; and
- an end-to-end browser provisioning request containing the prefix.

## Acceptance criteria

- A site cannot be created without exactly two lowercase letters for `dbLoginPrefix`.
- Entering `ac` generates exactly `dbLoginPrefix: 'ac-'` or the equivalent double-quoted form already used by the starter.
- Only the intended string contents change in `nuxt.config.ts`.
- A changed or incompatible starter fails explicitly instead of producing an incorrectly configured site.
- Web and CLI creation use the same validation and post-clone editor.
