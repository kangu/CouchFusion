# Component Registry On-Demand Preview Design

**Date:** 2026-07-14

## Summary

CouchFusion will add an explicitly managed preview host and a fullscreen component preview to the existing build-time component registry on a site's detail page. The preview runs inside the selected Nuxt site, so it uses the site's real components, styles, plugins, auto-imports, and development HMR connection.

Preview is intentionally on demand. The user must first select **Start preview host**. CouchFusion starts a second Nuxt development process on an independently allocated loopback port; it does not reuse, replace, or interrupt the site's ordinary development process. Once the host is ready, each registry entry exposes **Preview**. The preview opens fullscreen and provides the same desktop/mobile switcher behavior as the Content layer's component picker.

Version 1 renders declared component defaults only. It does not synthesize placeholder values and does not apply `previewProps`.

## Scope

### Included

- Explicit start and stop controls for one dedicated preview host per site.
- A second Nuxt development process that can run beside the normal site process.
- Loopback-only binding on an automatically selected port.
- A guarded Content-layer preview shell and guarded render-only route.
- Fullscreen preview launched from build-time registry entries.
- Desktop and fixed-width `375px` mobile modes.
- Real Nuxt rendering with HMR, including CSS updates.
- Declared component defaults only.
- Clear startup, stopped, unavailable, not-found, and render-failure states.
- Focused unit, API, component, integration, and browser coverage.

### Excluded

- Inline registry thumbnails.
- Starting the preview host implicitly when Preview is clicked.
- User-entered prop controls or fixture editing.
- Synthetic placeholder props.
- `previewProps` overrides.
- Previewing arbitrary Vue files that are absent from the generated registry.
- Remote or non-loopback preview hosts.
- Production deployment of the preview routes.

## Selected Architecture

The selected approach is a guarded preview facility supplied by the shared Content Nuxt layer and launched through a dedicated CouchFusion-managed Nuxt process.

The alternatives were rejected for these reasons:

- Reusing an ordinary application route couples previews to each site's routing, middleware, layouts, and content state.
- Generating a temporary standalone preview application duplicates Nuxt configuration and cannot reliably reproduce the site's runtime environment.

The responsibilities remain deliberately separated:

- **CouchFusion Go server** validates the selected site, constructs the trusted process configuration, owns the preview process, exposes lifecycle APIs, and applies the UI frame policy.
- **CouchFusion web UI** presents start/stop state, enables registry preview actions, and owns the viewport-filling overlay and close action.
- **Content Nuxt layer** registers guarded preview pages, resolves runtime registry components and declared defaults, renders the component, and owns the preview toolbar and desktop/mobile behavior.
- **Selected Nuxt site** supplies the actual component registry, component implementations, styles, plugins, and Vite HMR runtime.

## Preview Host Lifecycle

There is at most one dedicated component-preview process for a given workspace/site pair. Its internal task identity is distinct from the site's ordinary `dev` task, so both processes may run concurrently.

The site detail page follows this lifecycle:

1. With no host running, the registry panel shows **Start preview host** and disables component Preview actions.
2. Starting the host calls a dedicated API. The browser sends no executable, arguments, directory, port, environment values, or token.
3. The application service resolves the registered workspace and contained site, verifies the existing curated `dev` task, allocates an available loopback port, and generates a high-entropy preview token.
4. The process starts with server-owned environment overrides equivalent to:
   - `COUCHFUSION_PREVIEW=1`
   - `COUCHFUSION_PREVIEW_TOKEN=<random per process>`
   - `NUXT_HOST=127.0.0.1`
   - `NUXT_PORT=<allocated port>`
5. The process manager captures output and discovers the reported HTTP URL using its existing managed-process behavior.
6. While starting, the UI shows a non-blocking starting state. Preview actions become available only after the host has a usable loopback URL.
7. Closing a fullscreen component preview leaves the preview host running for fast subsequent previews.
8. **Stop preview host** terminates only the dedicated preview process. Exiting CouchFusion also cleans it up through existing process-manager shutdown behavior.

The environment extension belongs to the trusted internal `project.Command`/process boundary. API clients can never provide arbitrary environment variables. Snapshots and logs must not expose the preview token.

If the allocated port is lost before Nuxt binds it, the process fails normally and the UI offers Start again. Version 1 does not hide retries inside the server.

## HTTP API

The existing component-registry API gains explicit preview-host operations:

```text
GET    /api/v1/workspaces/{workspaceId}/sites/{siteId}/components/preview-host
POST   /api/v1/workspaces/{workspaceId}/sites/{siteId}/components/preview-host/start
POST   /api/v1/workspaces/{workspaceId}/sites/{siteId}/components/preview-host/stop
```

The status response contains only UI-safe lifecycle information:

```json
{
  "status": "stopped | starting | ready | failed",
  "processId": "optional managed process id",
  "url": "optional CouchFusion-consumable preview shell base URL",
  "startedAt": "optional timestamp",
  "problem": "optional concise failure description"
}
```

The response must not expose the raw token as a separate field. The application service may return a complete tokenized shell URL to its own authenticated UI because the iframe needs it locally, but API logs, events, process snapshots, and error messages must redact its query value. The URL is accepted only when its scheme is `http`, hostname is exactly `127.0.0.1` or `localhost`, and port matches the allocated preview process.

Start is idempotent for an already-starting or ready preview host and returns its current lifecycle view. Stop is idempotent for an absent or already-terminal host.

## Content-Layer Preview Routes

The Content layer registers two internal pages only when `COUCHFUSION_PREVIEW=1` is present at Nuxt startup:

```text
/__couchfusion/components/preview/:componentId
/__couchfusion/components/render/:componentId
```

Both pages require the process token in a query parameter. Missing or incorrect tokens return a not-found response rather than revealing that the facility exists. Neither route is registered for an ordinary development or production start.

### Preview shell

The shell is the fullscreen document loaded by the CouchFusion overlay. It contains:

- the registered component label and ID;
- a desktop/mobile segmented switcher at the top;
- a refresh action for the render iframe;
- a centered preview canvas filling the remaining height;
- a native render iframe pointed at the guarded render-only route.

Desktop gives the render iframe all available canvas width. Mobile fixes its viewport at `375px` and centers it horizontally. The iframe retains the full available height in both modes.

### Render-only page

The render page resolves `componentId` from `useComponentRegistry()`, selects `previewComponentId` when the definition declares one and otherwise uses `id`, computes declared defaults, and renders the component without an application layout around it.

The render iframe is a real Nuxt page rather than a document created with `document.write`. Consequently:

- responsive media queries see the selected iframe width;
- Nuxt plugins and auto-imported components are available;
- link and injected styles are loaded normally;
- Vite JavaScript and CSS HMR update the preview in place.

## Reuse of Existing Content Preview Logic

The device and prop logic currently embedded in `ComponentPickerDialog.vue` will be extracted into focused Content-layer units rather than duplicated:

- A reusable preview-device switcher owns the existing desktop/mobile icons, accessible names, active-state behavior, and keyboard-compatible buttons.
- A shared preview-device model defines the device union and the `375px` mobile width. The component picker and CouchFusion preview shell consume the same constants and state semantics.
- A shared default resolver returns props whose definitions have an explicit `default` value.

The component picker preserves its current behavior by composing the shared declared defaults with its existing synthetic fallbacks and `previewProps`. The CouchFusion render page calls the resolver in declared-defaults-only mode and performs no additional merge. This prevents the new feature from silently changing existing component-picker previews.

The existing `PreviewFrame.vue` remains available to the component picker. The new fullscreen shell does not use its stylesheet-cloning implementation because cloned style elements would not reliably follow later CSS HMR mutations.

## Fullscreen CouchFusion Experience

The site detail registry panel adds host controls near the registry heading and a Preview action on every ready registry row.

Preview opens a true viewport-filling modal layer above the CouchFusion application. The embedded Content-layer shell fills it. CouchFusion provides a persistent close control above the iframe so a broken component or preview document cannot trap the user. Escape also closes the overlay, focus returns to the originating Preview button, background scrolling is locked, and the underlying interface is inert while open.

The Content-layer toolbar remains at the top of the embedded shell and provides the shared desktop/mobile selector and refresh action. This locates preview-specific rendering controls with the renderer while CouchFusion retains the outer lifecycle and escape controls.

On narrow CouchFusion windows, the fullscreen layer still occupies the viewport. Mobile preview remains `375px` where space permits and shrinks only to the available canvas width to prevent horizontal page overflow; the device model remains mobile.

## Security Boundaries

- The preview host binds only to loopback.
- The browser cannot select commands, arguments, directories, ports, environment variables, or route flags.
- The service resolves only the curated `dev` task for the contained registered site.
- A random token is generated for each preview process and is never persisted.
- Both preview pages validate the token.
- Component IDs are resolved from the in-memory runtime registry; they are never converted into filesystem paths.
- Preview URLs are accepted only for the allocated loopback origin.
- CouchFusion's CSP adds `frame-src` permission for loopback HTTP preview origins. Existing `frame-ancestors 'none'` continues preventing CouchFusion itself from being framed.
- Cross-origin iframe isolation is retained; CouchFusion does not inspect or inject into the site document.
- Token values are redacted from server logs, process output surfaced as metadata, activity events, and user-facing errors.

The token is necessarily present in the local iframe request URL. It is a short-lived capability limited to a loopback-only process and becomes invalid when that process stops.

## Failure Handling

- **No curated dev task:** Start is unavailable with guidance that the site must expose a supported `dev` task.
- **Startup failure or early exit:** host status becomes failed, process output remains available through existing activity/process diagnostics, and Start becomes available again.
- **No reported URL:** the host remains starting for at most 60 seconds. If the process has not reported a verified URL by then, CouchFusion stops it and records a failed startup; it never enables Preview with an unverified URL.
- **Host stops while preview is open:** the overlay shows a disconnected state with Close and Start host actions.
- **Unknown component ID:** the preview shell presents a contained not-found state.
- **Component render exception:** a Nuxt/Vue error boundary presents the component ID and concise error copy while leaving the toolbar and close path usable.
- **No declared defaults:** the component is rendered with an empty props object.
- **Stale or invalid build-time registry:** existing registry status rules remain authoritative; Preview is unavailable until a usable registry entry exists.
- **HMR compilation error:** the Nuxt/Vite error UI is allowed to surface inside the render iframe and recover after the source is corrected.

## Testing Strategy

### Content layer

- Unit tests for declared-default extraction, including false, zero, empty-string, array, and object defaults.
- Regression tests proving the existing component picker still applies its synthetic fallbacks and `previewProps` while using the extracted shared helper.
- Component tests proving both picker and preview shell use the shared device switcher and `375px` constant.
- Route-registration tests proving preview routes are absent without the flag and present with it.
- Route guard tests for absent, incorrect, and correct tokens.
- Shell tests for desktop/full width, mobile/`375px`, refresh, unknown component, and render error states.

### Go service and API

- Process command tests for trusted environment overrides, environment inheritance, defensive copying, token redaction, loopback host, and allocated port.
- Service tests for curated `dev` resolution, concurrent normal and preview task ownership, idempotent start/stop, and URL validation.
- API tests for status/start/stop responses and stable validation/error mapping.
- CSP tests proving loopback frames are allowed without weakening other directives.
- Race tests for concurrent status/start/stop requests.

### CouchFusion UI

- Registry-panel tests for stopped, starting, ready, and failed host states.
- Tests proving Preview is disabled until ready and opens the correct component once ready.
- Fullscreen overlay tests for Escape, close, focus restoration, scroll lock, refresh, and stopped-host recovery.

### Browser acceptance

An end-to-end fixture starts CouchFusion, starts the dedicated host, opens a registry component fullscreen, verifies the desktop canvas, switches to mobile and measures a `375px` render viewport, edits a fixture component stylesheet, observes the HMR-applied visual change without restarting either server, closes the overlay, and stops the preview host. The test also confirms the site's ordinary development process can remain active concurrently.

## Acceptance Criteria

- A user explicitly starts and stops a dedicated preview host from the site detail page.
- The preview host runs beside the ordinary site dev process without a port or ownership conflict.
- Registry Preview actions are available only when the dedicated host is ready.
- A selected registry component opens in a viewport-filling preview.
- The top switcher uses the Content layer's shared component-picker device logic.
- Desktop fills the available preview canvas and mobile uses a `375px` viewport.
- The component receives only explicitly declared defaults in version 1.
- Site JavaScript and CSS changes appear through HMR without rotating the CouchFusion UI-server token.
- Preview routes are absent outside the dedicated preview process and reject invalid tokens.
- Arbitrary process configuration and filesystem component selection remain impossible through the browser API.
- Failure states never remove the user's ability to close the preview or restart the host.
