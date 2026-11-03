# First Website Onboarding Runway: Implementation Results

Date: 2026-07-18
Branch: `ui`
Status: Implemented

## Delivered

- Private, versioned, atomic onboarding persistence with explicit transitions.
- Onboarding, global-health, and process-owner REST APIs with domain validation.
- Six-milestone `/getting-started` UI with automatic unfinished-journey routing.
- Reusable embedded workspace and site provisioning forms.
- Inline job/process evidence, reload recovery, pause/resume, stale-reference recovery, and onboarding-only reset.
- Explicit install/dev actions, validated loopback URL detection, retained Codex generation, manual preview, and manual `/builder/` handoff.
- Accessibility and responsive milestone presentation, including focus advancement and reduced-motion behavior.
- Go, Vue, and Playwright coverage plus operator documentation.

## Security and scope

Persistence contains only status, selected IDs, a tracked job ID, confirmations, and timestamps. It contains no credentials, prompts, transcripts, commands, logs, filesystem paths, or operating-system process IDs. CouchFusion does not mutate Builder page documents or Git history.

## Verification

The implementation passed:

```sh
go test -race ./... -count=1
go vet ./...
cd web
bun run typecheck
bun run test:run                 # 75 tests
bun run build
bun run e2e                      # embedded control plane
bun run e2e:preview              # fullscreen viewport switching and HMR
bun run e2e:agent                # questions, approvals, consent, retained thread
bun run e2e:onboarding           # embedded full runway, reload, reset, pause/resume
```

The final static Nuxt output was regenerated with `bun run generate` and copied into `internal/web/dist` before the embedded browser suites ran.
