# First Website Onboarding Runway Design

Date: 2026-07-18
Status: Approved and implemented

## Outcome

CouchFusion provides a restart-safe `/getting-started` runway with six visible milestones: tools, workspace, site, local process, Codex generation and preview, then manual Content Builder placement. Every mutation remains explicit. Successful operations advance from observed API evidence, while safe identifiers and timestamps persist in `~/.couchfusion/onboarding.json`.

## Boundaries

- Workspace registration uses exact paths and never scans disk.
- Content and Auth are mandatory for sites selected by onboarding.
- Codex readiness blocks only component generation.
- Preview host startup and Builder placement are manual.
- CouchFusion never inspects the cross-origin Builder session or exposes credentials.
- Paused and completed journeys never force navigation.
- Completion remains permanent until an explicit onboarding-only reset.

## Recovery

Tracked jobs reconnect after reload. Missing jobs or resources clear only dependent progress. Managed processes are rediscovered by workspace, site, and task owner while CouchFusion is running; a server restart returns active journeys to the local-run milestone.
