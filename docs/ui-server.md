# CouchFusion UI server

From an installed release, run:

```bash
couchfusion ui
```

From this source checkout, use UI development mode for Vue and CSS HMR:

```bash
cd web
bun install
cd ..
go run . ui --dev
```

Open the tokenized loopback URL printed in the terminal. Register an existing workspace by its exact absolute path, or initialize a new one. CouchFusion never scans the disk for projects. Use `--no-open` to prevent automatic browser launch and `--port 4400` to request a fixed Go server port.

## Preview a registered component

1. Open a workspace, then a site with a ready build-time component registry.
2. Select **Start preview host** in **Local content components**.
3. Select **Preview** beside a component.
4. Use the desktop/mobile controls at the top of the fullscreen preview. Mobile uses a 375 px render viewport.
5. Edit the site's component source; the preview updates through the site's Nuxt HMR connection.
6. Close the fullscreen view when finished. Select **Stop preview host** to terminate its dedicated Nuxt process.

The preview host is explicit, separate from the site's normal development task, and bound only to loopback. If it fails to start, confirm the site has an exact `dev` package script and installed dependencies. Rebuild a missing or stale registry, and inspect the reported host problem or Nuxt terminal output for compile errors. Stop CouchFusion with Ctrl-C; it also stops UI-managed child processes.
