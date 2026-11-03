---
name: couchfusion-component-builder
description: Use when creating or updating Vue Content components inside a CouchFusion site's selected Nuxt application.
---

# CouchFusion Component Builder

Create production-ready, application-local Content components that remain editable in CouchFusion's Content Builder.

## Workflow

1. Read every applicable `AGENTS.md` from the repository root through the selected app.
2. Inspect `nuxt.config.ts`, `package.json`, existing `app/components/content/*.vue` components, shared styles, builder metadata, tests, and the canonical registry build/watch scripts. Follow the app's established patterns.
3. Check whether Content i18n is enabled. If it is, identify each text-bearing field as localized or global; ask before editing when the choice is ambiguous.
4. Implement only inside the selected application. Preserve its visual system, responsive behavior, accessibility, and existing structure. Use semantic HTML and platform-native behavior where practical.
5. Define typed props with useful defaults and add slots only for genuine freeform child content. Put `builderFieldMeta` and `builderComponentMeta` in the Vue source. Give discrete options select metadata; model nested objects and arrays using the app's existing registry conventions.
6. Add `useContentPropId()` markers to the real rendered output of every editable prop. Use exact dotted registry paths and numeric indexes for nested arrays, such as ``items.${index}.title``. Never add hidden focus-only elements.
7. Add or update focused tests for defaults, important states, accessibility behavior, and nested marker coverage.
8. Run the smallest relevant installed validation commands. Use the canonical registry generator after component changes and inspect its generated result.

## Boundaries

- Treat all pre-existing uncommitted work as user-owned. Do not overwrite, discard, stage, reformat broadly, or restore it.
- Do not create branches, commits, tags, or other Git history.
- Do not edit generated component registry TypeScript or JSON by hand. Change Vue source metadata and let the canonical generator rebuild every derived registry artifact.
- Do not modify dependency manifests or lockfiles without explicit user consent for this generation. Ask before running any package command that could change them.
- Do not use the network without an explicit approval. Prefer installed project tooling and existing dependencies.
- Stop and ask when a requested change conflicts with repository instructions, overlaps an existing dirty file unexpectedly, or requires writing outside the selected app.

## Completion

Report the relative files changed, validation performed, and any unresolved manual checks. Leave review and all Git operations to the user.
