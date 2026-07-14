# Canonical Content Registry Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every Content registry generation path produce the same integrity-linked TypeScript and CouchFusion JSON metadata from one Content-layer implementation.

**Architecture:** `layers/content` becomes the sole generator implementation and owns a focused paired-output module. The existing `cli-content` executables remain compatible but delegate to the layer scripts, forwarding arguments, stdio, signals, and exit status. Existing behavior unique to either generator is merged into the layer before delegation removes the duplicate.

**Tech Stack:** Bun, Node.js ESM, `bun:test`, Nuxt Content-layer scripts, SHA-256, atomic filesystem replacement.

## Global Constraints

- Work in `/Users/radu/Projects/nuxt-apps/layers` on `outside-preview` and `/Users/radu/Projects/nuxt-apps/cli-init` on `ui`.
- Create `codex/canonical-registry-generator` in `/Users/radu/Projects/nuxt-apps/cli-content` before modifying its `main` branch checkout.
- Preserve the existing uncommitted `previewSections` change from `cli-content/generate-component-registry.mjs` by porting its stricter validation into the canonical layer generator before replacing the legacy file.
- Preserve the untracked `/Users/radu/Projects/nuxt-apps/cli-content/docs/integrate_inside_layer.md`; do not stage, rewrite, or delete it.
- Preserve Content-layer multi-directory discovery, `--component-dir`, package `contentRegistry.componentDirs`, and Vue compiler `NodeTypes` fallback behavior.
- Preserve `cli-content` image-field UI inference when consolidating.
- The JSON manifest remains version `1` and is installed after its matching TypeScript file.
- Do not require existing app package scripts to change.

---

### Task 1: Move the Paired Output Contract into the Content Layer

**Files:**
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/scripts/registry-output.mjs`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/tests/scripts/registry-output.test.mjs`

**Interfaces:**
- Produces: `createRegistryRecord(definition, sourceFile, appRoot)`.
- Produces: `renderRegistryOutputs(records) -> { typescript: Buffer, manifest: Buffer }`.
- Produces: `writeRegistryPair({ outputFile, records, fileSystem? }) -> Promise<void>`.

- [ ] **Step 1: Copy the output-contract tests into the Content layer before the implementation**

Create `content/tests/scripts/registry-output.test.mjs` from the committed `cli-content/registry-output.test.mjs`. Keep tests for Nuxt 3/Nuxt 4/nested sources, outside-root rejection, lexical ordering, exact SHA-256 linkage, empty registries, TypeScript-before-JSON installation, sync calls, and cleanup after either rename fails.

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
cd /Users/radu/Projects/nuxt-apps/layers
bun test content/tests/scripts/registry-output.test.mjs
```

Expected: FAIL because `content/scripts/registry-output.mjs` does not exist.

- [ ] **Step 3: Add the focused output module**

Create `content/scripts/registry-output.mjs` with the already-reviewed `cli-content/registry-output.mjs` implementation. It must retain these public operations:

```js
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
  // Render TypeScript, then hash those exact bytes into the version-1 JSON manifest.
}

export async function writeRegistryPair({ outputFile, records, fileSystem = fs }) {
  // Write and sync sibling temporary files, rename TypeScript first, JSON second,
  // and remove remaining temporary files in finally.
}
```

Move `escapeString` and `formatValue` unchanged into this module as private helpers.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run the command from Step 2.

Expected: all output-contract tests pass with no temporary files left behind.

- [ ] **Step 5: Commit the output contract**

```bash
cd /Users/radu/Projects/nuxt-apps/layers
git add content/scripts/registry-output.mjs content/tests/scripts/registry-output.test.mjs
git commit -m "feat: own component registry outputs in content layer"
```

---

### Task 2: Make the Content Generator Produce Both Artifacts

**Files:**
- Modify: `/Users/radu/Projects/nuxt-apps/layers/content/scripts/generate-component-registry.mjs`
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/tests/scripts/generator-contract.test.mjs`

**Interfaces:**
- Consumes: `createRegistryRecord` and `writeRegistryPair` from Task 1.
- Produces: every successful generator run installs matching `component-definitions.ts` and `component-definitions.json`.

- [ ] **Step 1: Add a failing canonical-generator contract test**

Create `content/tests/scripts/generator-contract.test.mjs` that reads the canonical generator source and asserts the orchestration and merged behavior:

```js
import { describe, expect, test } from 'bun:test'
import fs from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const generator = fileURLToPath(new URL('../../scripts/generate-component-registry.mjs', import.meta.url))

describe('canonical component registry generator', () => {
  test('builds records and installs the paired output', async () => {
    const source = await fs.readFile(generator, 'utf8')
    expect(source).toContain("from './registry-output.mjs'")
    expect(source).toContain('createRegistryRecord(definition, file, appRoot)')
    expect(source).toContain('writeRegistryPair({ outputFile, records })')
    expect(source).not.toContain('writeDefinitionsFile')
  })

  test('retains image hints, preview sections, and layer directory extensions', async () => {
    const source = await fs.readFile(generator, 'utf8')
    expect(source).toContain('applyImageFieldUi')
    expect(source).toContain('meta.previewSections')
    expect(source).toContain('extraComponentDirFlags')
    expect(source).toContain('readConfiguredComponentDirs')
    expect(source).toContain('resolveNodeTypes')
  })
})
```

This source-contract test is paired with the real output-module tests because the generator resolves compiler packages from a consuming app and cannot be imported safely as a unit.

- [ ] **Step 2: Run the contract test and verify RED**

Run:

```bash
cd /Users/radu/Projects/nuxt-apps/layers
bun test content/tests/scripts/generator-contract.test.mjs
```

Expected: FAIL on the missing paired-output import/call and missing image hint behavior.

- [ ] **Step 3: Merge the generator behaviors into the layer**

Modify the generator as follows:

```js
import { createRegistryRecord, writeRegistryPair } from './registry-output.mjs'
```

Keep the layer's directory/configuration and `resolveNodeTypes` code. Port `IMAGE_FIELD_KEYWORDS`, `normalizeKey`, `looksLikeImageField`, and `applyImageFieldUi` from `cli-content`; make `applySchemaUiHints` return explicit metadata first and inferred image UI otherwise:

```js
const applySchemaUiHints = (schema, key, ui) => {
  if (ui?.widget === 'textarea' && schema.type === 'text') schema.type = 'textarea'
  if (ui) {
    schema.ui = ui
    return schema
  }
  return applyImageFieldUi(schema, key)
}
```

Replace `generateDefinitions` with `generateRecords`, retaining all parsing behavior and wrapping each definition:

```js
records.push(createRegistryRecord(definition, file, appRoot))
return records.sort((a, b) => a.manifest.id.localeCompare(b.manifest.id))
```

Port the current uncommitted `cli-content` `previewSections` filtering exactly: accept only object entries with non-empty string `id` and `label`, trim both values, and assign only when the filtered array is non-empty.

Remove the local formatter and `writeDefinitionsFile`; finish with:

```js
const records = await generateRecords(vueFiles, helpers)
await writeRegistryPair({ outputFile, records })
console.log(`Generated ${records.length} component definitions for ${appName}.`)
```

- [ ] **Step 4: Run generator and existing Content tests**

Run:

```bash
cd /Users/radu/Projects/nuxt-apps/layers
bun test content/tests/scripts/registry-output.test.mjs content/tests/scripts/generator-contract.test.mjs
bunx vitest --config vitest.config.ts content/tests/builder/component-preview-primitives.spec.ts content/tests/builder/component-preview-pages.spec.ts content/tests/server/component-preview-registration.spec.ts --run
```

Expected: all focused tests pass.

- [ ] **Step 5: Commit the canonical generator**

```bash
git add content/scripts/generate-component-registry.mjs content/tests/scripts/generator-contract.test.mjs
git commit -m "fix: generate couchfusion registry metadata during dev"
```

---

### Task 3: Replace `cli-content` Duplication with Compatibility Delegates

**Files:**
- Create: `/Users/radu/Projects/nuxt-apps/cli-content/delegate-to-content-layer.mjs`
- Create: `/Users/radu/Projects/nuxt-apps/cli-content/delegate-to-content-layer.test.mjs`
- Replace: `/Users/radu/Projects/nuxt-apps/cli-content/generate-component-registry.mjs`
- Replace: `/Users/radu/Projects/nuxt-apps/cli-content/watch-component-registry.ts`
- Delete: `/Users/radu/Projects/nuxt-apps/cli-content/registry-output.mjs`
- Delete: `/Users/radu/Projects/nuxt-apps/cli-content/registry-output.test.mjs`
- Keep untouched: `/Users/radu/Projects/nuxt-apps/cli-content/docs/integrate_inside_layer.md`

**Interfaces:**
- Produces: `contentLayerScript(name, options?) -> Promise<string>`.
- Produces: `delegateToContentLayer(name, args, options?) -> Promise<number>`.
- Preserves both existing bin entry points and all their arguments.

- [ ] **Step 1: Create a feature branch without disturbing current changes**

```bash
cd /Users/radu/Projects/nuxt-apps/cli-content
git switch -c codex/canonical-registry-generator
```

Expected: the modified generator and untracked documentation remain present on the new branch.

- [ ] **Step 2: Write failing delegate tests**

Create `delegate-to-content-layer.test.mjs` with injected filesystem/spawn boundaries:

```js
import { describe, expect, test } from 'bun:test'
import path from 'node:path'
import { contentLayerScript, delegateToContentLayer } from './delegate-to-content-layer.mjs'

test('resolves the canonical sibling layer script', async () => {
  const accessed = []
  const target = await contentLayerScript('generate-component-registry.mjs', {
    moduleDirectory: '/workspace/cli-content',
    access: async (file) => accessed.push(file),
  })
  expect(target).toBe('/workspace/layers/content/scripts/generate-component-registry.mjs')
  expect(accessed).toEqual([target])
})

test('forwards arguments and returns the delegated exit code', async () => {
  const calls = []
  const code = await delegateToContentLayer('generate-component-registry.mjs', ['--app=shop'], {
    resolveScript: async () => '/workspace/layers/content/scripts/generate-component-registry.mjs',
    spawn: (command, options) => {
      calls.push([command, options])
      return { exited: Promise.resolve(7), kill() {} }
    },
    installSignalHandlers() { return () => {} },
  })
  expect(calls[0][0]).toEqual(['bun', '/workspace/layers/content/scripts/generate-component-registry.mjs', '--app=shop'])
  expect(calls[0][1]).toEqual({ stdin: 'inherit', stdout: 'inherit', stderr: 'inherit' })
  expect(code).toBe(7)
})
```

Also test a missing canonical script error and signal cleanup.

- [ ] **Step 3: Run delegate tests and verify RED**

Run:

```bash
cd /Users/radu/Projects/nuxt-apps/cli-content
bun test delegate-to-content-layer.test.mjs
```

Expected: FAIL because the delegate module is missing.

- [ ] **Step 4: Implement the delegate and thin entry points**

Implement `contentLayerScript` using `../layers/content/scripts/<name>` relative to `cli-content`, validate it with `fs.access`, and throw a path-specific error if missing. Implement `delegateToContentLayer` with `Bun.spawn`, inherited stdio, SIGINT/SIGTERM forwarding, handler cleanup after `child.exited`, and returned exit code.

Replace `generate-component-registry.mjs` with:

```js
#!/usr/bin/env bun
import { delegateToContentLayer } from './delegate-to-content-layer.mjs'

const exitCode = await delegateToContentLayer('generate-component-registry.mjs', process.argv.slice(2))
process.exit(exitCode)
```

Replace `watch-component-registry.ts` with the equivalent delegate to `watch-component-registry.mjs`. Preserve executable permissions and the existing package `bin` names.

Delete the legacy `registry-output.mjs` and its test after the output contract tests are green in `layers/content`; leaving them in `cli-content` would retain a second implementation.

- [ ] **Step 5: Run all `cli-content` tests and check the user document remains untouched**

Run:

```bash
cd /Users/radu/Projects/nuxt-apps/cli-content
bun test
git status --short
```

Expected: all tests pass; `docs/integrate_inside_layer.md` remains untracked and is not staged.

- [ ] **Step 6: Commit only the compatibility implementation**

```bash
git add generate-component-registry.mjs watch-component-registry.ts delegate-to-content-layer.mjs delegate-to-content-layer.test.mjs registry-output.mjs registry-output.test.mjs
git commit -m "refactor: delegate registry generation to content layer"
```

---

### Task 4: Prove Normal Dev Generation and Document Ownership

**Files:**
- Create: `/Users/radu/Projects/nuxt-apps/layers/content/docs/implementation_results/2026-07-14-canonical-component-registry-generator.md`
- Modify: `/Users/radu/Projects/nuxt-apps/cli-init/docs/superpowers/plans/2026-07-14-canonical-content-registry-generator.md`

**Interfaces:**
- Consumes: the canonical layer generator and legacy delegates.
- Produces: verified real-app paired generation and migration documentation.

- [ ] **Step 1: Back up and regenerate a real app registry through the layer command**

Use `/Users/radu/Projects/nuxt-apps/apps/with-ui` because it already contains a valid registry pair and installed generator dependencies. Save both generated files to temporary paths, run:

```bash
cd /Users/radu/Projects/nuxt-apps/apps/with-ui
bun /Users/radu/Projects/nuxt-apps/layers/content/scripts/generate-component-registry.mjs
```

Assert both files were updated together, JSON version is `1`, and this command prints a matching digest:

```bash
shasum -a 256 app/content-builder/component-definitions.ts
node -e "const m=require('./app/content-builder/component-definitions.json'); console.log(m.typescriptSha256)"
```

Restore both app files in a `finally`/trap path so the app repository is unchanged.

- [ ] **Step 2: Prove the legacy command reaches the same implementation**

Repeat the backed-up real-app generation through:

```bash
bun /Users/radu/Projects/nuxt-apps/cli-content/generate-component-registry.mjs
```

Expected: the same component count, paired files, and matching digest; restore both app files afterward.

- [ ] **Step 3: Run complete verification**

```bash
cd /Users/radu/Projects/nuxt-apps/layers
bun test content/tests/scripts/registry-output.test.mjs content/tests/scripts/generator-contract.test.mjs
bunx vitest --config vitest.config.ts content/tests --run

cd /Users/radu/Projects/nuxt-apps/cli-content
bun test

cd /Users/radu/Projects/nuxt-apps/cli-init
go test -race ./...
go vet ./...
```

Expected: all test commands pass.

- [ ] **Step 4: Document the completed migration**

Create the Content-layer implementation result with:

- the root cause (two divergent generators);
- the layer as canonical owner;
- the paired output and JSON commit-marker behavior;
- preserved image inference, preview sections, multi-directory, and compiler fallback behavior;
- legacy compatibility entry points;
- verification commands and real-app proof.

- [ ] **Step 5: Commit documentation and mark this plan complete**

```bash
cd /Users/radu/Projects/nuxt-apps/layers
git add content/docs/implementation_results/2026-07-14-canonical-component-registry-generator.md
git commit -m "docs: record canonical registry generator migration"

cd /Users/radu/Projects/nuxt-apps/cli-init
git add docs/superpowers/plans/2026-07-14-canonical-content-registry-generator.md
git commit -m "docs: complete canonical registry generator plan"
```

## Final Review

- [ ] `layers/content/scripts/generate-component-registry.mjs` is the only registry parsing implementation.
- [ ] A normal Content watcher run generates matching `.ts` and `.json` files.
- [ ] Both legacy `cli-content` executables are thin delegates.
- [ ] The existing `previewSections` change is present in the canonical generator.
- [ ] Existing image inference and layer-only directory/compiler behaviors are preserved.
- [ ] `/Users/radu/Projects/nuxt-apps/cli-content/docs/integrate_inside_layer.md` remains untouched and untracked.
- [ ] The real app used for proof has no lasting changes.
- [ ] All three repositories contain no unintended changes.
