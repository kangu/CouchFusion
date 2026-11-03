# Compact UI Type Scale Design

**Date:** 2026-07-14

**Status:** Approved direction

## Goal

Reduce oversized display typography consistently across the CouchFusion web UI while retaining its editorial workbench identity. The interface should read as an operational application rather than a marketing page.

## Type scale

- Page titles use `clamp(2.5rem, 4vw, 4rem)` with a slightly more relaxed line height.
- The welcome hero uses `clamp(2.75rem, 6vw, 4.5rem)` instead of reaching `8.8rem`.
- Dialog titles use `clamp(1.75rem, 4vw, 2.75rem)`.
- Card, panel, and health headings use `1.625rem`; empty-state headings use `1.75rem`.
- Metric numerals use `2.5rem`.
- Process and activity headings use `1.35rem`.

Mobile rules must not increase any heading beyond its desktop cap.

## Preserved visual language

The condensed display face, uppercase treatment, graphite-and-paper palette, cyan and lime signals, mono labels, borders, geometric background, and existing component layouts remain unchanged. This is a type-scale recalibration, not a redesign.

## Implementation

Introduce shared display-size CSS custom properties and apply them to the existing heading selectors in `web/assets/css/main.css`. Regenerate the Nuxt static bundle and copy it into `internal/web/dist` so `couchfusion ui` serves the updated design.

## Verification

- Frontend unit tests and strict typechecking pass.
- Nuxt static generation succeeds.
- Browser acceptance passes against the embedded UI.
- The Workspaces page is visually inspected at desktop and mobile widths to confirm hierarchy and wrapping.
