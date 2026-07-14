# Site Detail Component Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a compact site-detail control rail and a secure, rebuildable inventory of build-time content components whose Vue sources can be opened in the system editor.

**Architecture:** The `cli-content` generator will produce the existing TypeScript definitions and a versioned, hash-linked JSON manifest from one record set using paired temporary-file replacement. CouchFusion will inspect that passive manifest through a focused project service, expose safe site-scoped list/rebuild/open operations, and render them in a dedicated Vue panel without executing site code during inspection.

**Tech Stack:** Bun/ES modules, Node built-ins and `bun:test`; Go 1.24 standard library and `net/http`; Nuxt 4, Vue 3 Composition API, Vitest, Vue Test Utils, and Playwright.

## Global Constraints

- The existing TypeScript registry remains the runtime source; JSON is inspection metadata only.
- Manifest `version` is exactly `1`, `typescriptSha256` is lowercase SHA-256 of the exact TypeScript bytes, and JSON components are ordered by ID.
- Manifest sources are slash-normalized paths relative to the site root; no executable code, defaults, secrets, or absolute paths enter JSON.
- Generation writes and syncs sibling temporary files, replaces TypeScript first, and replaces JSON last as the commit marker.
- Inspection never invokes Bun, imports TypeScript, executes project code, or mutates files.
- A source must be a non-empty relative `.vue` path whose lexical and canonical targets remain under the site root and whose target is a regular file.
- Rebuild may execute only the curated `script:registry:build` task; the browser cannot provide a command, directory, argument, or alternate task ID.
- Open-source accepts only a component ID, re-inspects and revalidates the registered path, and launches without a shell.
- System launch commands are `open <file>` on macOS, `xdg-open <file>` on Linux, and `rundll32 url.dll,FileProtocolHandler <file>` on Windows.
- Runtime CouchDB aliases, in-app source editing/preview, component mutation, arbitrary browsing/task execution, and automatic rebuilds are out of scope.
- Preserve the unrelated untracked `/Users/radu/Projects/nuxt-apps/cli-content/docs/integrate_inside_layer.md` file.

## File Structure

### `/Users/radu/Projects/nuxt-apps/cli-content`

- `registry-output.mjs`: pure record/serialization helpers plus paired atomic output installation.
- `registry-output.test.mjs`: Bun unit tests for manifest shape, path metadata, hashing, ordering, cleanup, and replacement order.
- `generate-component-registry.mjs`: extracts `{ definition, source }` records and delegates output writing.
- `package.json`: exposes the repository test command.

### `/Users/radu/Projects/nuxt-apps/cli-init`

- `internal/project/components.go`: manifest DTOs, discovery, strict validation, hash/staleness checks, and repeatable source resolution.
- `internal/project/components_test.go`: Nuxt 3/4, state, malformed input, lexical escape, symlink escape, and lookup tests.
- `internal/editor/opener.go`: injected system file opener using direct process arguments.
- `internal/editor/opener_test.go`: OS command selection tests with a fake command starter.
- `internal/app/types.go`: content-registry and editor dependency interfaces plus stable domain error mapping.
- `internal/app/service.go`: list, exact rebuild, and open-source orchestration.
- `internal/app/service_test.go`: authorization, exact task resolution, job behavior, and opener delegation tests.
- `internal/web/workspaces.go`: three site-scoped HTTP routes and handlers.
- `internal/web/api_test.go`: API contract and error/status tests.
- `internal/web/testserver/main.go`: safe component fixtures and no-op editor behavior for browser acceptance.
- `web/types/api.ts`: component inventory API types.
- `web/components/sites/ComponentRegistryPanel.vue`: ready/non-ready inventory states and rebuild/open events.
- `web/components/sites/TaskList.vue`: denser task-row semantics.
- `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`: parallel loading, activity-aware rebuilding, scoped open errors, and registry-first layout.
- `web/assets/css/main.css`: compact task, registry ledger, two-column rail, and responsive ordering styles.
- `web/tests/site-detail.test.ts`: component and site-detail interaction coverage.
- `web/e2e/control-plane.spec.ts`: browser acceptance for inventory, rebuild, and open.
- `internal/web/dist/**`: regenerated embedded Nuxt output after all source tests pass.

---

### Task 1: Produce a Versioned, Hash-Linked JSON Manifest

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-content`

**Files:**
- Create: `registry-output.mjs`
- Create: `registry-output.test.mjs`
- Modify: `generate-component-registry.mjs:1-6,45,138-143,1481-1585,1759-1798`
- Modify: `package.json`

**Interfaces:**
- Produces: `createRegistryRecord(definition, sourceFile, appRoot): { definition, manifest }`.
- Produces: `renderRegistryOutputs(records): { typescript: Buffer, manifest: Buffer }`.
- Produces: `writeRegistryPair({ outputFile, records, fileSystem? }): Promise<void>`.
- Manifest records contain `id`, `label`, `description`, `source`, `propCount`, and `allowChildren`.

- [ ] **Step 1: Add failing serialization and metadata tests**

Create `registry-output.test.mjs` with table-driven cases that assert ID ordering, SHA-256 parity, Nuxt 3/4 and nested relative paths, prop counts, child support, and an empty registry:

```js
import { afterEach, describe, expect, test } from 'bun:test'
import { mkdtemp, mkdir, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { createHash } from 'node:crypto'
import {
  createRegistryRecord,
  renderRegistryOutputs,
  writeRegistryPair,
} from './registry-output.mjs'

const roots = []
afterEach(async () => Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true }))))

describe('component registry outputs', () => {
  test.each([
    ['Nuxt 3', 'components/content/Hero.vue', 'components/content/Hero.vue'],
    ['Nuxt 4', 'app/components/content/Hero.vue', 'app/components/content/Hero.vue'],
    ['nested', 'app/components/content/marketing/Hero.vue', 'app/components/content/marketing/Hero.vue'],
  ])('%s source stays site-relative', (_name, relativeSource, expected) => {
    const appRoot = path.join(path.sep, 'workspace', 'apps', 'shop')
    const record = createRegistryRecord(
      { id: 'hero', label: 'Hero', description: 'Lead block', props: [{ key: 'title' }], allowChildren: true },
      path.join(appRoot, relativeSource),
      appRoot,
    )
    expect(record.manifest).toEqual({
      id: 'hero', label: 'Hero', description: 'Lead block', source: expected,
      propCount: 1, allowChildren: true,
    })
  })

  test('renders deterministic JSON linked to the exact TypeScript bytes', () => {
    const appRoot = '/workspace/apps/shop'
    const records = [
      createRegistryRecord({ id: 'z-card', label: 'Z Card', description: '', props: [] }, `${appRoot}/components/content/ZCard.vue`, appRoot),
      createRegistryRecord({ id: 'a-hero', label: 'A Hero', description: 'Hero', props: [{ key: 'title' }] }, `${appRoot}/components/content/AHero.vue`, appRoot),
    ]
    const outputs = renderRegistryOutputs(records)
    const manifest = JSON.parse(outputs.manifest.toString('utf8'))
    expect(manifest.version).toBe(1)
    expect(manifest.components.map((component) => component.id)).toEqual(['a-hero', 'z-card'])
    expect(manifest.typescriptSha256).toBe(createHash('sha256').update(outputs.typescript).digest('hex'))
    expect(outputs.manifest.toString('utf8').endsWith('\n')).toBe(true)
  })

  test('renders a valid empty registry', () => {
    const outputs = renderRegistryOutputs([])
    expect(JSON.parse(outputs.manifest.toString('utf8')).components).toEqual([])
    expect(outputs.typescript.toString('utf8')).toContain('const definitions: ComponentDefinition[] = []')
  })
})
```

- [ ] **Step 2: Run the focused test to establish the red state**

Run: `bun test registry-output.test.mjs`

Expected: FAIL because `./registry-output.mjs` does not exist.

- [ ] **Step 3: Implement pure record and rendering helpers**

Create `registry-output.mjs` with these exported contracts and reuse the generator's existing value formatter by moving it into this module:

```js
import fs from 'node:fs/promises'
import path from 'node:path'
import { createHash, randomUUID } from 'node:crypto'

export function createRegistryRecord(definition, sourceFile, appRoot) {
  const source = path.relative(appRoot, sourceFile).split(path.sep).join('/')
  if (!source || source === '..' || source.startsWith('../') || path.posix.isAbsolute(source)) {
    throw new Error(`Component source is outside the app root: ${sourceFile}`)
  }
  return {
    definition,
    manifest: {
      id: String(definition.id ?? ''),
      label: String(definition.label ?? ''),
      description: String(definition.description ?? ''),
      source,
      propCount: Array.isArray(definition.props) ? definition.props.length : 0,
      allowChildren: Boolean(definition.allowChildren),
    },
  }
}

export function renderRegistryOutputs(records) {
  const ordered = [...records].sort((left, right) => left.manifest.id.localeCompare(right.manifest.id))
  const formatted = formatValue(ordered.map((record) => record.definition))
  const typescript = Buffer.from(
    `import type { ComponentDefinition } from '#content/types/builder'\n\n` +
    `const definitions: ComponentDefinition[] = ${formatted}\n\nexport default definitions\n`,
  )
  const manifest = Buffer.from(`${JSON.stringify({
    version: 1,
    typescriptSha256: createHash('sha256').update(typescript).digest('hex'),
    components: ordered.map((record) => record.manifest),
  }, null, 2)}\n`)
  return { typescript, manifest }
}
```

Move `escapeString` and `formatValue` unchanged from `generate-component-registry.mjs` into this module, keeping `formatValue` private.

- [ ] **Step 4: Add failing paired-write behavior tests**

Append tests that inject a recording filesystem adapter. Assert calls rename `component-definitions.ts` before `component-definitions.json`, that installed files match `renderRegistryOutputs`, and that a forced first/second rename failure unlinks all remaining sibling temporary files:

```js
test('syncs both files and installs TypeScript before the manifest commit marker', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'registry-output-'))
  roots.push(root)
  const outputFile = path.join(root, 'app/content-builder/component-definitions.ts')
  await mkdir(path.dirname(outputFile), { recursive: true })
  const operations = []
  const recording = recordingFileSystem(operations)
  await writeRegistryPair({ outputFile, records: [], fileSystem: recording })
  expect(operations.filter(([name]) => name === 'sync')).toHaveLength(2)
  expect(operations.filter(([name]) => name === 'rename').map(([, , target]) => path.basename(target))).toEqual([
    'component-definitions.ts', 'component-definitions.json',
  ])
  expect(JSON.parse(await readFile(outputFile.replace(/\.ts$/, '.json'), 'utf8')).version).toBe(1)
})

test.each([1, 2])('cleans temporary files when rename %d fails', async (failedRename) => {
  const root = await mkdtemp(path.join(tmpdir(), 'registry-output-failure-'))
  roots.push(root)
  const outputFile = path.join(root, 'content-builder/component-definitions.ts')
  const operations = []
  await expect(writeRegistryPair({
    outputFile,
    records: [],
    fileSystem: recordingFileSystem(operations, failedRename),
  })).rejects.toThrow('forced rename failure')
  expect(operations.filter(([name]) => name === 'unlink').map(([, file]) => file)).toEqual(
    expect.arrayContaining(operations.filter(([name]) => name === 'open').map(([, file]) => file)),
  )
})
```

The local `recordingFileSystem` delegates to `fs` and records `mkdir`, `open`, handle `writeFile`/`sync`/`close`, `rename`, and `unlink`; its `rename` counter throws `new Error('forced rename failure')` at the requested call.

- [ ] **Step 5: Implement paired temporary-file installation**

Add `writeRegistryPair` using sibling temp names, `open(..., 'wx', 0o600)`, handle `writeFile`, `sync`, and `close`, followed by ordered renames. Always attempt `unlink` for both temporary paths in `finally`, ignoring only `ENOENT`:

```js
export async function writeRegistryPair({ outputFile, records, fileSystem = fs }) {
  const outputs = renderRegistryOutputs(records)
  const manifestFile = outputFile.replace(/\.ts$/, '.json')
  const suffix = `${process.pid}-${randomUUID()}.tmp`
  const typeTemp = `${outputFile}.${suffix}`
  const manifestTemp = `${manifestFile}.${suffix}`
  await fileSystem.mkdir(path.dirname(outputFile), { recursive: true })
  try {
    await writeSynced(fileSystem, typeTemp, outputs.typescript)
    await writeSynced(fileSystem, manifestTemp, outputs.manifest)
    await fileSystem.rename(typeTemp, outputFile)
    await fileSystem.rename(manifestTemp, manifestFile)
  } finally {
    await Promise.all([removeTemporary(fileSystem, typeTemp), removeTemporary(fileSystem, manifestTemp)])
  }
}
```

- [ ] **Step 6: Integrate records into the existing generator**

Import `createRegistryRecord` and `writeRegistryPair`. Rename `generateDefinitions` to `generateRecords`; after applying builder metadata, push `createRegistryRecord(definition, file, appRoot)`, sort by `record.manifest.id`, and replace `writeDefinitionsFile(definitions)` with:

```js
const records = await generateRecords(vueFiles, helpers)
await writeRegistryPair({ outputFile, records })
console.log(`Generated ${records.length} component definitions for ${appName}.`)
```

Delete the duplicate formatter and direct `fs.writeFile` implementation from the generator.

- [ ] **Step 7: Add and run the repository test command**

Add to `package.json`:

```json
"scripts": {
  "test": "bun test"
}
```

Run: `bun test`

Expected: all `registry-output.test.mjs` tests PASS.

- [ ] **Step 8: Exercise generation against the existing Nuxt 4 fixture app**

Run: `bun generate-component-registry.mjs --app=with-ui-2`

Expected: command succeeds and `/Users/radu/Projects/nuxt-apps/apps/with-ui-2/app/content-builder/component-definitions.json` has version `1`, IDs in lexical order, relative `.vue` sources, and a digest matching `shasum -a 256 .../component-definitions.ts`.

- [ ] **Step 9: Commit the generator contract**

```bash
git add registry-output.mjs registry-output.test.mjs generate-component-registry.mjs package.json
git commit -m "feat: emit component registry manifest"
```

Do not stage generated files in `/Users/radu/Projects/nuxt-apps/apps/with-ui-2` or the unrelated documentation file.

### Task 2: Inspect and Secure Component Manifests in Go

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Create: `internal/project/components.go`
- Create: `internal/project/components_test.go`

**Interfaces:**
- Produces: `type ComponentRegistryStatus string` with `ready`, `stale`, `missing`, and `invalid` constants.
- Produces: `type ComponentDefinition struct { ID, Label, Description, Source string; PropCount int; AllowChildren bool }`.
- Produces: `type ComponentRegistry struct { Status ComponentRegistryStatus; Components []ComponentDefinition; Problem string }`.
- Produces: `func (ComponentInspector) Inspect(context.Context, string) ComponentRegistry`.
- Produces: `func (ComponentInspector) ResolveSource(context.Context, string, string) (string, error)`.
- Produces: `ErrComponentNotFound` and `ErrComponentRegistryInvalid`.

- [ ] **Step 1: Write failing state and discovery tests**

Create helpers that build a site root, write a TypeScript file, calculate its SHA-256, and write either `app/content-builder/component-definitions.json` or `content-builder/component-definitions.json`. Add tests for Nuxt 4 ready, Nuxt 3 ready, valid empty, missing, source-newer stale, malformed JSON, unsupported version, duplicate IDs, and hash mismatch:

```go
func TestComponentInspectorReportsReadyStaleMissingInvalidAndEmpty(t *testing.T) {
    tests := []struct {
        name string
        arrange func(*testing.T, string)
        want ComponentRegistryStatus
        count int
    }{
        {name: "missing", want: ComponentRegistryMissing},
        {name: "nuxt4 ready", arrange: writeValidNuxt4Registry, want: ComponentRegistryReady, count: 1},
        {name: "nuxt3 ready", arrange: writeValidNuxt3Registry, want: ComponentRegistryReady, count: 1},
        {name: "valid empty", arrange: writeEmptyRegistry, want: ComponentRegistryReady},
        {name: "stale", arrange: writeStaleRegistry, want: ComponentRegistryStale, count: 1},
        {name: "malformed", arrange: writeMalformedRegistry, want: ComponentRegistryInvalid},
        {name: "unsupported version", arrange: writeVersionTwoRegistry, want: ComponentRegistryInvalid},
        {name: "duplicate id", arrange: writeDuplicateRegistry, want: ComponentRegistryInvalid},
        {name: "hash mismatch", arrange: writeHashMismatchRegistry, want: ComponentRegistryInvalid},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            root := t.TempDir()
            if test.arrange != nil { test.arrange(t, root) }
            got := (ComponentInspector{}).Inspect(context.Background(), root)
            if got.Status != test.want || len(got.Components) != test.count {
                t.Fatalf("Inspect() = %#v", got)
            }
            if got.Components == nil { t.Fatal("components must encode as []") }
        })
    }
}
```

- [ ] **Step 2: Run project tests to verify the new contract is red**

Run: `go test ./internal/project -run ComponentInspector -count=1`

Expected: FAIL because `ComponentInspector` and registry types are undefined.

- [ ] **Step 3: Implement strict manifest discovery and validation**

Decode with `json.Decoder.DisallowUnknownFields`, reject trailing JSON, enforce all scalar rules, sort nothing, and require strictly increasing unique IDs so the installed contract is deterministic. Discovery checks Nuxt 4 first and Nuxt 3 second; no manifest returns `missing`. Validate the companion `.ts` SHA-256 before trusting entries. Return `invalid` with concise problem copy and an empty component slice for any failed invariant.

Use these manifest-only structs so Go does not accept extra executable metadata:

```go
type componentManifest struct {
    Version          int                   `json:"version"`
    TypeScriptSHA256 string                `json:"typescriptSha256"`
    Components       []ComponentDefinition `json:"components"`
}

type ComponentDefinition struct {
    ID            string `json:"id"`
    Label         string `json:"label"`
    Description   string `json:"description"`
    Source        string `json:"source"`
    PropCount     int    `json:"propCount"`
    AllowChildren bool   `json:"allowChildren"`
}
```

Keep the raw invalid reason internal; expose stable messages such as `Component registry JSON is invalid.`, `Component registry does not match its TypeScript definitions.`, and `A registered component source is unsafe or unavailable.`

- [ ] **Step 4: Write failing containment and lookup tests**

Add cases for absolute paths, `../` lexical escape, wrong extension, missing file, directory named `.vue`, symlink escape (skip on Windows when privileges prevent creation), unknown ID, and a source replaced by a symlink between inspect and resolve:

```go
func TestComponentInspectorRejectsUnsafeSources(t *testing.T) {
    for _, source := range []string{"", "../Outside.vue", "/tmp/Outside.vue", "components/content/Hero.ts", "components/content/Missing.vue"} {
        t.Run(strings.ReplaceAll(source, "/", "_"), func(t *testing.T) {
            root := t.TempDir()
            writeRegistryWithSource(t, root, source)
            got := (ComponentInspector{}).Inspect(context.Background(), root)
            if got.Status != ComponentRegistryInvalid { t.Fatalf("Inspect() = %#v", got) }
        })
    }
}

func TestResolveSourceRevalidatesContainment(t *testing.T) {
    root := t.TempDir()
    writeValidNuxt4Registry(t, root)
    source, err := (ComponentInspector{}).ResolveSource(context.Background(), root, "hero")
    if err != nil || !strings.HasSuffix(source, filepath.Join("content", "Hero.vue")) { t.Fatalf("source=%q err=%v", source, err) }
    if _, err := (ComponentInspector{}).ResolveSource(context.Background(), root, "missing"); !errors.Is(err, ErrComponentNotFound) {
        t.Fatalf("error = %v", err)
    }
}
```

- [ ] **Step 5: Implement canonical source resolution**

Add a private resolver that rejects non-slash-normalized paths, cleans with `filepath.FromSlash`, uses `filepath.Rel` for lexical containment, uses `filepath.EvalSymlinks` for canonical containment, and requires `os.Stat(...).Mode().IsRegular()`. `ResolveSource` calls `Inspect`, rejects `missing`/`invalid`, locates the ID, then repeats the resolver against current filesystem state before returning the absolute canonical filename.

- [ ] **Step 6: Run and commit the project inspector**

Run: `go test ./internal/project -count=1`

Expected: PASS, including all pre-existing inspection and task tests.

```bash
git add internal/project/components.go internal/project/components_test.go
git commit -m "feat: inspect content component manifests"
```

### Task 3: Add a Cross-Platform Injected File Opener

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Create: `internal/editor/opener.go`
- Create: `internal/editor/opener_test.go`

**Interfaces:**
- Produces: `type CommandStarter interface { Start(name string, args ...string) error }`.
- Produces: `type Opener struct { GOOS string; Commands CommandStarter }`.
- Produces: `func (Opener) Open(string) error`.
- Produces: `ErrUnsupportedPlatform`.

- [ ] **Step 1: Write failing OS-command tests**

```go
func TestOpenerUsesDirectPlatformArguments(t *testing.T) {
    tests := []struct { goos, name string; args []string }{
        {"darwin", "open", []string{"/workspace/apps/shop/app/components/content/Hero.vue"}},
        {"linux", "xdg-open", []string{"/workspace/apps/shop/app/components/content/Hero.vue"}},
        {"windows", "rundll32", []string{"url.dll,FileProtocolHandler", "/workspace/apps/shop/app/components/content/Hero.vue"}},
    }
    for _, test := range tests {
        t.Run(test.goos, func(t *testing.T) {
            commands := &commandStarterFake{}
            err := (Opener{GOOS: test.goos, Commands: commands}).Open("/workspace/apps/shop/app/components/content/Hero.vue")
            if err != nil || commands.name != test.name || !reflect.DeepEqual(commands.args, test.args) {
                t.Fatalf("command=%s %#v err=%v", commands.name, commands.args, err)
            }
        })
    }
}
```

Also assert an empty filename is rejected, `GOOS: "plan9"` returns `ErrUnsupportedPlatform`, and starter errors are preserved.

- [ ] **Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/editor -count=1`

Expected: FAIL because the package implementation is missing.

- [ ] **Step 3: Implement the opener without a shell**

Use `runtime.GOOS` and an adapter whose `Start` calls `exec.Command(name, args...).Start()`. Select only the three approved command/argument arrays and reject empty filenames before starting.

- [ ] **Step 4: Run and commit opener tests**

Run: `go test ./internal/editor -count=1`

Expected: PASS.

```bash
git add internal/editor/opener.go internal/editor/opener_test.go
git commit -m "feat: open component sources in system editor"
```

### Task 4: Orchestrate Component List, Exact Rebuild, and Open Operations

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Modify: `internal/app/types.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/service_test.go`

**Interfaces:**
- Consumes: `project.ComponentInspector.Inspect` and `ResolveSource` from Task 2.
- Consumes: `editor.Opener.Open` from Task 3.
- Produces: `type ContentComponentService interface { Inspect(context.Context, string) project.ComponentRegistry; ResolveSource(context.Context, string, string) (string, error) }`.
- Produces: `type FileOpener interface { Open(string) error }`.
- Produces: `type ComponentRegistryView struct { project.ComponentRegistry; CanRebuild bool }`.
- Produces: `Components`, `RebuildComponentRegistry`, and `OpenComponentSource` service methods.

- [ ] **Step 1: Write failing application-service tests**

Extend fakes and add tests that prove:

```go
func TestComponentsAddsRebuildCapabilityFromCuratedTasks(t *testing.T) {
    components := &componentServiceFake{registry: project.ComponentRegistry{Status: project.ComponentRegistryReady, Components: []project.ComponentDefinition{}}}
    tasks := &taskFake{tasks: []project.Task{{ID: "script:registry:build", Name: "registry:build", Kind: "script"}}}
    service := newServiceForTest(t, Dependencies{Registry: registeredWorkspace(), Projects: shopProject(), Tasks: tasks, Components: components})
    got, err := service.Components(context.Background(), "w1", "shop")
    if err != nil || !got.CanRebuild || components.inspectedRoot != "/workspace/apps/shop" { t.Fatalf("got=%#v err=%v", got, err) }
}

func TestRebuildComponentRegistryRunsOnlyExactCuratedTask(t *testing.T) {
    tasks := &taskFake{
        tasks: []project.Task{{ID: "script:registry:build", Name: "registry:build", Kind: "script"}},
        commands: map[string]project.Command{"script:registry:build": {Executable: "bun", Args: []string{"run", "registry:build"}, Dir: "/workspace/apps/shop"}},
    }
    runner := &commandRunnerFake{}
    service := newServiceForTest(t, Dependencies{Registry: registeredWorkspace(), Projects: shopProject(), Tasks: tasks, Commands: runner})
    job, err := service.RebuildComponentRegistry(context.Background(), "w1", "shop", "browser")
    if err != nil { t.Fatal(err) }
    waitForJobStatus(t, service, job.ID, jobs.Succeeded)
    if !reflect.DeepEqual(runner.command.Args, []string{"run", "registry:build"}) { t.Fatalf("command=%#v", runner.command) }
}

func TestOpenComponentSourceResolvesRegisteredIDBeforeOpening(t *testing.T) {
    components := &componentServiceFake{source: "/workspace/apps/shop/app/components/content/Hero.vue"}
    opener := &fileOpenerFake{}
    service := newServiceForTest(t, Dependencies{Registry: registeredWorkspace(), Projects: shopProject(), Components: components, Files: opener})
    if err := service.OpenComponentSource(context.Background(), "w1", "shop", "hero"); err != nil { t.Fatal(err) }
    if components.resolvedID != "hero" || opener.filename != components.source { t.Fatalf("components=%#v opener=%#v", components, opener) }
}
```

Add negative cases for absent rebuild task, a long-running task with the same ID, resolver not found/invalid errors, and opener failure mapping to `ErrorProcess` with message `Could not open component source`.

- [ ] **Step 2: Run focused application tests to establish failure**

Run: `go test ./internal/app -run 'Components|RebuildComponentRegistry|OpenComponentSource' -count=1`

Expected: FAIL because dependencies and methods are undefined.

- [ ] **Step 3: Add dependencies, defaults, and error mapping**

Add `Components ContentComponentService` and `Files FileOpener` to `Dependencies`; default them in `NewService` to `project.ComponentInspector{}` and `editor.Opener{}`. Map `project.ErrComponentNotFound` to `not_found` and `project.ErrComponentRegistryInvalid` to `validation`.

- [ ] **Step 4: Implement list and exact rebuild orchestration**

`Components` resolves the registered site, inspects its root, and scans `Tasks(site)` for a finite task whose ID is exactly `script:registry:build`. `RebuildComponentRegistry` repeats that check, resolves that exact ID through `TaskCatalog.Resolve`, and starts a finite job with type `component-registry.rebuild`; its run closure invokes only `CommandRunner.Run` with the resolved command.

- [ ] **Step 5: Implement open orchestration**

`OpenComponentSource` resolves the registered site, passes only `site.Root` and `componentID` to `ResolveSource`, then calls `Files.Open`. Preserve not-found/validation mappings from the inspector and wrap launcher failures as:

```go
return &Error{Code: ErrorProcess, Message: "Could not open component source", cause: err}
```

- [ ] **Step 6: Run and commit application tests**

Run: `go test ./internal/app -count=1`

Expected: PASS.

```bash
git add internal/app/types.go internal/app/service.go internal/app/service_test.go
git commit -m "feat: manage site component registries"
```

### Task 5: Expose the Site-Scoped Component HTTP API

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Modify: `internal/web/workspaces.go`
- Modify: `internal/web/api_test.go`
- Modify: `internal/web/testserver/main.go`

**Interfaces:**
- Consumes: application methods from Task 4.
- Produces: `GET .../components`, `POST .../components/registry/rebuild`, and `POST .../components/{componentId}/open`.

- [ ] **Step 1: Add failing API contract tests**

Extend `apiApplicationStub` with component result/error/call fields and methods. Add table tests that send authenticated requests through `newAPIHandler` and assert:

```go
func TestComponentRoutes(t *testing.T) {
    application := &apiApplicationStub{components: app.ComponentRegistryView{
        ComponentRegistry: project.ComponentRegistry{Status: project.ComponentRegistryReady, Components: []project.ComponentDefinition{{ID: "hero", Label: "Hero", Source: "app/components/content/Hero.vue", PropCount: 2, AllowChildren: true}}},
        CanRebuild: true,
    }}
    handler := newAPIHandler(application)
    assertJSONRequest(t, handler, http.MethodGet, "/api/v1/workspaces/w1/sites/shop/components", "", http.StatusOK, `"status":"ready"`)
    assertJSONRequest(t, handler, http.MethodPost, "/api/v1/workspaces/w1/sites/shop/components/registry/rebuild", `{}`, http.StatusAccepted, `"type":"component-registry.rebuild"`)
    assertJSONRequest(t, handler, http.MethodPost, "/api/v1/workspaces/w1/sites/shop/components/hero/open", `{}`, http.StatusNoContent, ``)
    if application.openedComponentID != "hero" { t.Fatalf("opened ID=%q", application.openedComponentID) }
}
```

Also assert malformed IDs are `422`, missing IDs are `404`, invalid manifests are `422`, and launcher failures are `409` through existing error mapping.

- [ ] **Step 2: Run the focused API tests to verify failure**

Run: `go test ./internal/web -run ComponentRoutes -count=1`

Expected: FAIL because routes and application methods are missing.

- [ ] **Step 3: Extend the application interface and register exact routes**

Add:

```go
Components(context.Context, string, string) (app.ComponentRegistryView, error)
RebuildComponentRegistry(context.Context, string, string, string) (jobs.Snapshot, error)
OpenComponentSource(context.Context, string, string, string) error
```

Register the three approved route patterns before the fallback handler.

- [ ] **Step 4: Implement handlers with empty mutation bodies**

Use `workspaceSiteIDs`, `pathID`, `decodeEmptyRequest`, and the session ID already in context. List returns `200`, rebuild returns `202`, and open writes `204` only after the application method succeeds.

- [ ] **Step 5: Extend the safe browser fixture**

Give `fixtureSite()` the exact script `"registry:build": "bun run ../../cli-content/generate-component-registry.mjs"`. Add a ready two-component response (`hero` and `parallax`), a successful rebuild job, and a no-op `OpenComponentSource` that records the requested ID in memory without launching an editor.

- [ ] **Step 6: Run and commit HTTP tests**

Run: `go test ./internal/web -count=1`

Expected: PASS.

```bash
git add internal/web/workspaces.go internal/web/api_test.go internal/web/testserver/main.go
git commit -m "feat: expose component registry API"
```

### Task 6: Build the Compact Registry-First Site Detail UI

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Modify: `web/types/api.ts`
- Create: `web/components/sites/ComponentRegistryPanel.vue`
- Modify: `web/components/sites/TaskList.vue`
- Modify: `web/pages/workspaces/[workspaceId]/sites/[siteId].vue`
- Modify: `web/assets/css/main.css`
- Modify: `web/tests/site-detail.test.ts`

**Interfaces:**
- Consumes: component API routes from Task 5.
- Produces: `ComponentRegistryResponse`, `ContentComponent`, and `ComponentRegistryStatus` TypeScript types.
- Produces: `ComponentRegistryPanel` props `registry`, `rebuilding`, `openingId`, `error`; emits `rebuild` and `open(componentId)`.

- [ ] **Step 1: Add failing compact-task and registry-panel tests**

Extend `web/tests/site-detail.test.ts` to assert task metadata and buttons stay in one `.task-row`, busy state affects only the matching task, and all registry states render correctly:

```ts
import ComponentRegistryPanel from '../components/sites/ComponentRegistryPanel.vue'

const readyRegistry = {
  status: 'ready' as const,
  canRebuild: true,
  components: [{
    id: 'hero', label: 'Hero', description: 'Lead block',
    source: 'app/components/content/Hero.vue', propCount: 2, allowChildren: true,
  }],
}

it('renders the component ledger and emits explicit actions', async () => {
  const wrapper = mount(ComponentRegistryPanel, { props: { registry: readyRegistry, rebuilding: false, openingId: '', error: '' } })
  expect(wrapper.get('[data-registry-status]').text()).toContain('Ready')
  expect(wrapper.text()).toContain('1 component')
  expect(wrapper.text()).toContain('2 properties')
  expect(wrapper.text()).toContain('Accepts children')
  expect(wrapper.text()).toContain('app/components/content/Hero.vue')
  await wrapper.get('[data-action="rebuild-registry"]').trigger('click')
  await wrapper.get('[data-component="hero"]').trigger('click')
  expect(wrapper.emitted('rebuild')).toHaveLength(1)
  expect(wrapper.emitted('open')?.[0]).toEqual(['hero'])
})

it.each([
  ['stale', 'may not match'], ['missing', 'has not been generated'], ['invalid', 'could not be trusted'],
] as const)('renders %s guidance', (status, copy) => {
  const wrapper = mount(ComponentRegistryPanel, { props: { registry: { status, canRebuild: true, components: [], problem: copy }, rebuilding: false, openingId: '', error: '' } })
  expect(wrapper.text()).toContain(copy)
})
```

Add a valid-empty test and assertions for rebuild/open disabled labels and panel-scoped errors.

- [ ] **Step 2: Run the focused Vue test to establish failure**

Run: `cd web && bun run test:run tests/site-detail.test.ts`

Expected: FAIL because the registry panel and types are missing.

- [ ] **Step 3: Add frontend API types**

Add:

```ts
export type ComponentRegistryStatus = 'ready' | 'stale' | 'missing' | 'invalid'
export interface ContentComponent {
  id: string; label: string; description: string; source: string
  propCount: number; allowChildren: boolean
}
export interface ComponentRegistryResponse {
  status: ComponentRegistryStatus
  components: ContentComponent[]
  problem?: string
  canRebuild: boolean
}
```

- [ ] **Step 4: Implement the focused registry panel component**

Build semantic header, count, status lozenge, conditional rebuild control, non-ready guidance, valid-empty message, and a ledger of component rows. Use buttons with `data-action="rebuild-registry"` and `data-component="<id>"`; emit only IDs and never source paths. Keep failure copy in an internal `.notice--error` region with `role="alert"`.

- [ ] **Step 5: Compact the task list markup and styling**

Keep one article per task with a single metadata line and one action. Add `aria-busy` to the active row. In `main.css`, change task rows to a tighter grid with approximately `.55rem` vertical padding, a `2.25rem` minimum button height, and no oversized section-heading dependency.

- [ ] **Step 6: Add failing page-orchestration tests**

Mount the site-detail page with mocked `useApi`, `useActivity`, and route params. Assert initial loading issues three parallel GETs; rebuild POST tracks the returned job; a terminal succeeded snapshot triggers a fresh components GET; failed jobs retain the registry and display panel error; open POST targets the encoded component ID and errors remain in the component panel.

Use deferred promises so the test can assert `Rebuilding…` and `Opening…` disabled states before resolution.

- [ ] **Step 7: Implement page state and activity-aware refresh**

Load site, tasks, and components with `Promise.allSettled` so a component request failure does not hide site/task data. Add `registry`, `registryError`, `rebuilding`, and `openingId` refs. Rebuild posts `{}`, calls the existing `watchJob`, and passes a completion callback that reloads registry only when the fetched snapshot becomes `succeeded`; failed/cancelled snapshots set concise panel copy. Open posts `{}` to the ID route and clears only `openingId` in `finally`.

Update `watchJob` to accept an optional callback exactly once:

```ts
function watchJob(job: JobSnapshot, onTerminal?: (job: JobSnapshot) => void) {
  let completed = false
  // existing subscription setup
  // after each fetched snapshot:
  if (!completed && ['succeeded', 'failed', 'cancelled'].includes(snapshot.status)) {
    completed = true
    onTerminal?.(snapshot)
  }
}
```

- [ ] **Step 8: Implement the approved layout and responsive order**

Replace `.detail-grid` with a `.site-detail-grid` containing a narrow `.site-detail-rail` (Tasks then Fingerprint) and a wider registry panel. Current activity remains after the grid at full width. On screens at or below `880px`, make the registry panel order first and the rail second. Use natural section sizes from existing CSS variables; do not reintroduce hero-sized headings.

- [ ] **Step 9: Run Vue tests, typecheck, and production generation**

Run:

```bash
cd web
bun run test:run
bun run typecheck
bun run generate
```

Expected: all Vitest tests PASS, Nuxt typecheck exits 0, and static generation exits 0.

- [ ] **Step 10: Commit the frontend**

```bash
git add web/types/api.ts web/components/sites/ComponentRegistryPanel.vue web/components/sites/TaskList.vue 'web/pages/workspaces/[workspaceId]/sites/[siteId].vue' web/assets/css/main.css web/tests/site-detail.test.ts
git commit -m "feat: add site component registry panel"
```

### Task 7: Verify Browser Acceptance and Refresh Embedded Assets

**Repository:** `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Modify: `web/e2e/control-plane.spec.ts`
- Modify: `internal/web/dist/**` (generated)

**Interfaces:**
- Consumes: safe testserver fixtures from Task 5 and site-detail UI from Task 6.
- Produces: end-to-end evidence that inventory, rebuild, open, compact tasks, and responsive ordering work together.

- [ ] **Step 1: Add failing browser assertions**

After navigating to Shop in `control-plane.spec.ts`, assert:

```ts
const components = page.getByRole('region', { name: 'Local content components' })
await expect(components).toContainText('2 components')
await expect(components.getByText('Hero', { exact: true })).toBeVisible()
await expect(components.getByText('app/components/content/Hero.vue')).toBeVisible()
await components.getByRole('button', { name: 'Open source' }).first().click()
await components.getByRole('button', { name: 'Rebuild registry' }).click()
await expect(page.getByText('component-registry.rebuild', { exact: true })).toBeVisible()
```

At desktop width, compare element bounding boxes to prove the task rail is left of the registry. At `390px`, assert the registry top is above the task panel top. Retain the existing Run/Start/Stop acceptance assertions.

- [ ] **Step 2: Run browser acceptance to establish any missing integration**

Run: `cd web && bun run e2e`

Expected before final integration: the new assertions fail if fixture routes, accessible labels, activity refresh, or ordering is incomplete.

- [ ] **Step 3: Correct only integration defects exposed by acceptance**

Keep selectors accessible: add `aria-label="Local content components"` to the registry region and `aria-label="Curated tasks"` to the task region if absent. Keep the fake opener side-effect free and make its successful `204` observable only through the request completion.

- [ ] **Step 4: Run complete repository verification**

Run from `/Users/radu/Projects/nuxt-apps/cli-init`:

```bash
gofmt -w internal/project/components.go internal/project/components_test.go internal/editor/opener.go internal/editor/opener_test.go internal/app/types.go internal/app/service.go internal/app/service_test.go internal/web/workspaces.go internal/web/api_test.go internal/web/testserver/main.go
go test ./...
cd web
bun run test:run
bun run typecheck
bun run generate
bun run e2e
```

Expected: every command exits 0.

- [ ] **Step 5: Refresh embedded static output after source verification**

Replace the embedded output with the already verified Nuxt generation, then run the affected Go tests:

```bash
cd /Users/radu/Projects/nuxt-apps/cli-init
find internal/web/dist -mindepth 1 -delete
cp -R web/.output/public/. internal/web/dist/
go test ./internal/web ./internal/cli
git status --short
```

Expected: Go tests PASS; status contains only intentional source, plan, test, and regenerated `internal/web/dist` changes.

- [ ] **Step 6: Commit acceptance and embedded assets**

```bash
git add web/e2e/control-plane.spec.ts internal/web/dist
git commit -m "test: cover component registry workflow"
```

### Task 8: Final Cross-Repository Contract Verification

**Repositories:** `/Users/radu/Projects/nuxt-apps/cli-content`, `/Users/radu/Projects/nuxt-apps/cli-init`

**Files:**
- Verify only; no planned source changes.

**Interfaces:**
- Validates the generator JSON contract against CouchFusion inspection without introducing a runtime dependency between repositories.

- [ ] **Step 1: Regenerate a real app manifest**

Run:

```bash
cd /Users/radu/Projects/nuxt-apps/cli-content
bun test
bun generate-component-registry.mjs --app=with-ui-2
```

Expected: tests PASS and both registry outputs are produced.

- [ ] **Step 2: Start CouchFusion with the UI branch and inspect manually**

Use the documented UI development command, open workspace `nuxt-apps`, navigate to `with-ui-2`, and verify the panel reports `ready`, lists the generated content components, and shows relative paths only.

- [ ] **Step 3: Exercise both explicit actions manually**

Click `Rebuild registry`; verify Current activity completes successfully and the panel refreshes. Click `Open source` on one component; verify the configured system editor opens that exact `.vue` file.

- [ ] **Step 4: Verify clean intended repository state**

Run:

```bash
git -C /Users/radu/Projects/nuxt-apps/cli-content status --short
git -C /Users/radu/Projects/nuxt-apps/cli-init status --short
```

Expected: `cli-content` still shows the pre-existing untracked `docs/integrate_inside_layer.md` and no accidental generated app files; `cli-init` contains only intentional committed work.
