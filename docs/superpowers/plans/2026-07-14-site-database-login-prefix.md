# Site Database Login Prefix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Require a two-letter database login prefix during site creation and atomically replace the cloned starter's single `dbLoginPrefix` string with the user value plus `-`.

**Architecture:** A shared workspace validator protects every entry point. The value travels through web, application, workspace, and CLI request types as two raw lowercase letters. A focused post-clone editor changes only the quoted value in `nuxt.config.ts`, then emits a success event.

**Tech Stack:** Go 1.24, Vue 3/Nuxt 4, TypeScript, Vitest, Playwright.

## Global Constraints

- Input is exactly two lowercase ASCII letters matching `^[a-z]{2}$`.
- The input begins blank; CouchFusion appends the trailing hyphen.
- Web and CLI creation use the same validation and editor.
- The editor must find exactly one quoted `dbLoginPrefix` assignment.
- Quote style, permissions, formatting, comments, and every unrelated byte must be preserved.
- Writes are atomic and temporary files are cleaned after failures.
- Incompatible starters fail; partially cloned directories are not deleted automatically.

---

### Task 1: Validate and atomically edit the cloned Nuxt config

**Files:**
- Create: `internal/workspace/database_prefix.go`
- Create: `internal/workspace/database_prefix_test.go`
- Modify: `internal/workspace/workspace.go`
- Modify: `internal/workspace/workspace_test.go`

**Interfaces:**
- Produces: `func ValidateDBLoginPrefix(string) error` and `func updateDBLoginPrefix(string, string) error`.
- Extends: `workspace.CreateSiteRequest` with `DBLoginPrefix string`.

- [x] **Step 1: Write failing validator and editor tests**

Test `ValidateDBLoginPrefix` with `ab` as the only valid representative and `""`, `a`, `abc`, `Aq`, `a1`, `a-`, and `éa` as invalid. Test single and double quotes, byte preservation, file mode preservation, missing property, duplicate property, non-string property, and missing file.

The successful preservation assertion uses:

```go
before := "export default defineNuxtConfig({\n  runtimeConfig: {\n    keep: true,\n    dbLoginPrefix: 'ns-', // unchanged comment\n  },\n})\n"
want := strings.Replace(before, "'ns-'", "'ac-'", 1)
```

For every failure, assert no `.nuxt.config.ts-*` temporary file remains in the directory.

- [x] **Step 2: Run the tests and verify the red state**

Run: `go test ./internal/workspace -run 'Test(ValidateDBLoginPrefix|UpdateDBLoginPrefix)' -count=1`

Expected: FAIL because the validator and editor do not exist.

- [x] **Step 3: Implement validation and guarded matching**

Use an anchored regexp for validation. For editing, count property occurrences with `(?m)\bdbLoginPrefix\s*:` and require exactly one. Match the value with single- and double-quoted alternatives, identify only the captured value byte range, and construct the result as `before + prefix + "-" + after`. A non-string property returns `dbLoginPrefix must be a quoted string`; missing and duplicate properties receive distinct errors.

- [x] **Step 4: Implement the atomic writer**

Read the original mode with `os.Stat`. Use `os.CreateTemp(filepath.Dir(path), ".nuxt.config.ts-*")`, `Chmod(original.Perm())`, write all bytes, `Sync`, `Close`, and `os.Rename`. Defer cleanup unless rename succeeds. Wrap errors with the target filename but never include file contents.

- [x] **Step 5: Write the failing provisioning integration test**

Update `fakeClone.scaffoldSite` to create a representative `nuxt.config.ts` containing `dbLoginPrefix: 'ns-'`. Add a site test that passes `DBLoginPrefix: "ac"`, reads the cloned file, expects `dbLoginPrefix: 'ac-'`, and records the `site.config.database_prefix` event. Add an invalid-prefix test proving `clone.calls == 0`.

- [x] **Step 6: Wire the request, mutation, and event**

Add `DBLoginPrefix` to `workspace.CreateSiteRequest`, validate before `prepareForClone`, call `updateDBLoginPrefix` immediately after `updateNuxtExtends`, then emit:

```go
dependencies.emit(EventSuccess, "site.config.database_prefix", "Database login prefix configured")
```

- [x] **Step 7: Verify and commit Task 1**

Run:

```bash
gofmt -w internal/workspace/database_prefix.go internal/workspace/database_prefix_test.go internal/workspace/workspace.go internal/workspace/workspace_test.go
go test ./internal/workspace -count=1
git diff --check
```

Commit: `feat: configure site database login prefix`

---

### Task 2: Propagate and validate the API request

**Files:**
- Modify: `internal/app/types.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/service_test.go`
- Modify: `internal/web/workspaces.go`
- Modify: `internal/web/api_test.go`
- Modify: `internal/web/testserver/main.go`

**Interfaces:**
- Consumes: `workspace.ValidateDBLoginPrefix` and `workspace.CreateSiteRequest.DBLoginPrefix`.
- Extends: `app.CreateSiteRequest` and POST JSON with `DBLoginPrefix string` / `dbLoginPrefix`.

- [x] **Step 1: Write failing application validation tests**

Extend the successful service test with `DBLoginPrefix: "ac"` and record the provisioner request. Add a table test covering blank, uppercase, numeric, short, and long input. Assert every invalid request returns a validation error for `dbLoginPrefix` and starts zero jobs.

- [x] **Step 2: Write the failing API propagation test**

Change the API request fixture to:

```json
{"name":"shop","modules":["auth"],"dbLoginPrefix":"ac"}
```

Record the request in `apiApplicationStub.CreateSite` and assert `DBLoginPrefix == "ac"`.

- [x] **Step 3: Run the tests and verify the red state**

Run: `go test ./internal/app ./internal/web -run 'Test.*(CreateSite|Site)' -count=1`

Expected: FAIL because the application and HTTP request types do not expose the prefix.

- [x] **Step 4: Implement application validation and propagation**

Add the field to `app.CreateSiteRequest`. In both `CreateSite` and `CreateSiteAtRoot`, call `workspace.ValidateDBLoginPrefix`; map failures to:

```go
ValidationError("dbLoginPrefix", "Database login prefix must contain exactly two lowercase letters")
```

Pass the value through `workspaceProvisioner.CreateSite`.

- [x] **Step 5: Decode the JSON field and update the fixture**

Add `DBLoginPrefix string \`json:"dbLoginPrefix"\`` to the create-site input and pass it into `app.CreateSiteRequest`. Ensure the deterministic testserver accepts and records requests containing the field so browser tests remain representative.

- [x] **Step 6: Verify and commit Task 2**

Run:

```bash
gofmt -w internal/app/types.go internal/app/service.go internal/app/service_test.go internal/web/workspaces.go internal/web/api_test.go internal/web/testserver/main.go
go test ./internal/app ./internal/web -count=1
git diff --check
```

Commit: `feat: propagate site database prefix`

---

### Task 3: Add the required field to the web wizard

**Files:**
- Modify: `web/components/provision/SiteWizard.vue`
- Modify: `web/pages/workspaces/[workspaceId]/sites/index.vue`
- Create: `web/tests/site-wizard.test.ts`
- Modify: `web/e2e/control-plane.spec.ts`

**Interfaces:**
- Produces submit payload `{ name, modules, branch, force, dbLoginPrefix }`.
- Consumes API JSON field `dbLoginPrefix`.

- [x] **Step 1: Write the failing wizard test**

Mount the open wizard and assert `[name="dbLoginPrefix"]` starts blank and has `required`, `minlength="2"`, `maxlength="2"`, and `pattern="[a-z]{2}"`. Submit step one while blank and assert the wizard remains on step 1. Enter `ac`, advance through modules, assert review contains `ac-`, submit, and expect:

```ts
expect(wrapper.emitted('submit')?.[0]?.[0]).toMatchObject({ dbLoginPrefix: 'ac' })
```

- [x] **Step 2: Run the unit test and verify the red state**

Run: `cd web && bun run test:run -- site-wizard`

Expected: FAIL because the input and payload field are absent.

- [x] **Step 3: Implement the wizard field and validation**

Extend the form and emitted type with `dbLoginPrefix`. Render the blank input and helper text on step 1. Track validity with `/^[a-z]{2}$/` and disable/guard Continue until name and prefix are valid. Render `${form.dbLoginPrefix}-` in review and emit the raw value.

- [x] **Step 4: Propagate the page payload and extend E2E**

Update the page callback type. In the browser journey, fill `Database login prefix` with `ac` before Continue and retain the existing successful provisioning assertions.

- [x] **Step 5: Verify and commit Task 3**

Run:

```bash
cd web
bun run test:run
bun run typecheck
bun run e2e
cd ..
git diff --check
```

Expected: 8 unit-test files and the control-plane journey PASS.

Commit: `feat: collect database prefix in site wizard`

---

### Task 4: Add the required CLI input and complete verification

**Files:**
- Modify: `main.go`
- Modify: `internal/workspace/new_tui.go`
- Create: `internal/workspace/new_tui_test.go`
- Modify: `README.md`
- Create: `docs/implementation_results/2026-07-14-site-database-login-prefix.md`
- Modify: `docs/superpowers/plans/2026-07-14-site-database-login-prefix.md`

**Interfaces:**
- Adds: `couchfusion new --db-login-prefix ac`.
- Extends: `CreateSiteOperation` with the raw prefix before credential arguments.

- [x] **Step 1: Write failing TUI model tests**

Construct `newAppModelWithOperation`, assert the prefix input starts blank with a two-character limit, and send Enter with an invalid prefix to remain on `stepName`. Fill `ac`, advance, and invoke the operation from the running command; assert the operation receives `ac`. Assert the summary contains `Database : ac-`.

- [x] **Step 2: Run the TUI test and verify the red state**

Run: `go test ./internal/workspace -run 'TestNewApp.*Prefix' -count=1`

Expected: FAIL because the model and operation lack the prefix.

- [x] **Step 3: Implement the TUI field**

Add `prefixInput`, focus index, error, and `dbLoginPrefix` to `newAppModel`. The name step switches fields with Tab/arrow keys, requires a sanitized name and `ValidateDBLoginPrefix(prefix) == nil`, displays both inputs, includes `Database : <prefix>-` in the summary, and passes the prefix to `CreateSiteOperation`.

- [x] **Step 4: Add and propagate the CLI flag**

Parse:

```go
dbLoginPrefix := fs.String("db-login-prefix", "", "Two lowercase letters used for database login names")
```

Pass it as the TUI hint and into both `app.CreateSiteRequest` paths. Update root usage text to mention the flag. Non-TUI invalid or missing values are rejected by the application service before a job starts.

- [x] **Step 5: Update documentation and record results**

Document the web field and CLI example `couchfusion new --name shop --db-login-prefix ac`. Record the atomic edit behavior, validation, events, and exact verification results in the implementation-results file.

- [x] **Step 6: Run the complete release gate**

Run:

```bash
go test -race ./... -count=1
go vet ./...
cd web
bun run test:run
bun run typecheck
bun run generate
bun run e2e
bun run e2e:hmr
cd ..
git diff --check
```

Expected: all Go, frontend, generation, and browser checks PASS.

- [x] **Step 7: Mark the plan complete and commit**

Mark every checkbox complete and commit:

```bash
git add main.go internal/workspace/new_tui.go internal/workspace/new_tui_test.go README.md docs/implementation_results/2026-07-14-site-database-login-prefix.md docs/superpowers/plans/2026-07-14-site-database-login-prefix.md
git commit -m "docs: ship site database login prefix"
git status --short
```
