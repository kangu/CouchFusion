# Compact UI Type Scale Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace oversized CouchFusion UI display typography with a compact, consistent application-scale hierarchy.

**Architecture:** Define shared display-size custom properties in the existing global stylesheet and route every display-heading selector through them. Protect the scale with browser assertions at desktop and mobile widths, then regenerate the static Nuxt bundle consumed by Go embedding.

**Tech Stack:** Nuxt 4, Vue 3, CSS custom properties, Playwright, Bun, Go embed

## Global Constraints

- Preserve the condensed display face, uppercase treatment, palette, mono labels, borders, background, and component layouts.
- Page titles use `clamp(2.5rem, 4vw, 4rem)`.
- Welcome titles use `clamp(2.75rem, 6vw, 4.5rem)`.
- Dialog titles use `clamp(1.75rem, 4vw, 2.75rem)`.
- Card/panel/health headings use `1.625rem`; empty-state headings use `1.75rem`; metrics use `2.5rem`; process/activity headings use `1.35rem`.
- Mobile rules must not increase headings beyond desktop caps.

---

### Task 1: Recalibrate and Verify the Display Type Scale

**Files:**
- Modify: `web/assets/css/main.css`
- Modify: `web/e2e/control-plane.spec.ts`
- Regenerate: `internal/web/dist/**`

**Interfaces:**
- Consumes: existing `.page-header`, `.welcome-sheet`, `.dialog`, card, metric, process, and activity selectors.
- Produces: CSS variables `--display-page`, `--display-hero`, `--display-dialog`, `--display-section`, `--display-empty`, `--display-metric`, and `--display-activity`.

- [x] **Step 1: Add failing desktop and mobile browser assertions to the existing flow**

Add these assertions immediately after the Workspaces heading becomes visible in the existing `controls an explicit CouchFusion workspace from one local UI` test. Restore the desktop viewport before the registration flow continues:

```ts
const title = page.getByRole('heading', { name: 'Workspaces' })
expect(await title.evaluate((element) => Number.parseFloat(getComputedStyle(element).fontSize))).toBeLessThanOrEqual(64)

await page.setViewportSize({ width: 390, height: 844 })
expect(await title.evaluate((element) => Number.parseFloat(getComputedStyle(element).fontSize))).toBeLessThanOrEqual(48)
await page.setViewportSize({ width: 1280, height: 720 })
```

- [x] **Step 2: Run the browser assertion and verify failure**

Run: `cd web && bun run e2e`

Expected: FAIL because the desktop Workspaces title can resolve above `64px` under the current `clamp(3.4rem, 7vw, 7rem)` rule.

- [x] **Step 3: Define and apply the compact type tokens**

Add to `:root`:

```css
--display-page: clamp(2.5rem, 4vw, 4rem);
--display-hero: clamp(2.75rem, 6vw, 4.5rem);
--display-dialog: clamp(1.75rem, 4vw, 2.75rem);
--display-section: 1.625rem;
--display-empty: 1.75rem;
--display-metric: 2.5rem;
--display-activity: 1.35rem;
```

Replace the existing display sizes with:

```css
.welcome-sheet h1 { font-size: var(--display-hero); line-height: .9; }
.page-header h1 { font-size: var(--display-page); line-height: .9; }
.dialog h2 { font-size: var(--display-dialog); line-height: .95; }
.workspace-card h2, .site-card h2, .panel h2, .health-card h2 { font-size: var(--display-section); }
.empty-state h2 { font-size: var(--display-empty); }
.metric-grid strong { font-size: var(--display-metric); }
.process-header h2, .activity-detail h2, .activity-card h3 { font-size: var(--display-activity); }
```

Remove the mobile `.welcome-sheet h1` rule that increases the title to `clamp(3.3rem, 16vw, 6rem)`.

- [x] **Step 4: Run frontend verification**

Run:

```bash
cd web
bun run test:run
bun run typecheck
bun run generate
bun run e2e
```

Expected: 14 unit tests PASS, typechecking and generation PASS, and the Playwright journey PASS with both typography assertions.

- [x] **Step 5: Refresh embedded assets and verify Go integration**

Run:

```bash
find internal/web/dist -mindepth 1 -delete
cp -R web/.output/public/. internal/web/dist/
go test ./internal/web ./internal/cli -count=1
git diff --check
```

Expected: embedded-server and CLI tests PASS with no whitespace errors.

- [x] **Step 6: Commit the compact scale**

```bash
git add web/assets/css/main.css web/e2e/control-plane.spec.ts internal/web/dist docs/superpowers/plans/2026-07-14-compact-ui-type-scale.md
git commit -m "style: compact CouchFusion UI typography"
```
