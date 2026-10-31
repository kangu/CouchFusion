# hydra1 Phase 2 install — workbench polish

Contract: `cli-init/docs/superpowers/specs/2026-10-30-phase2-workbench-contract.md`. Builds on Phase 1 (`../phase1`).

## What changes on the box

| Item | Change |
|---|---|
| `/etc/systemd/system/cf-registry@.service` | **new** oneshot: runs the layer's registry generator as `cfdev` (sandboxed, no network). Used by "Rebuild registry" and as the fallback when the watcher is late after delete/restore/generate. Repository code never runs as `cfcp`. |
| `/etc/polkit-1/rules.d/50-couchfusion.rules` | allows `cfcp` to start/stop/restart `cf-registry@*` too (still nothing else) |
| `/etc/tmpfiles.d/couchfusion.conf` | `/run/couchfusion/dev` becomes `2710 cfcp:cfdev`, so the dev server can see `<app>.agent-running` (the env file stays `0600 cfcp`) |
| `server.toml` | optional `[services] units = [...]` (default list built in) |
| dev runtime env (written by the server) | adds `COUCHFUSION_AGENT_MARKER=/run/couchfusion/dev/<app>.agent-running` |

Nothing restarts existing services. `couchfusion.service` is restarted once (dev keeps running), and the dev server once more from the UI so it picks up the marker path.

## Order

On the laptop:

```bash
cd cli-init/web && bun run generate
```

```bash
cd cli-init/server && ./release.sh
```

```bash
rsync -a dist/couchfusion-server-<REL>.tar.zst dist/couchfusion-server-<REL>.tar.zst.sha256 packaging/hydra1/phase1 packaging/hydra1/phase2 root@hydra1:/root/couchfusion-p2/
```

On hydra1 as root:

```bash
bash /root/couchfusion-p2/phase2/01-install.sh --self-test
```

```bash
cd /root/couchfusion-p2 && bash phase1/02-release.sh couchfusion-server-<REL>.tar.zst
```

```bash
systemctl restart couchfusion
```

Then restart the dev server from the control UI (or `bash /root/couchfusion-p2/phase1/05-start.sh`) so `cf-dev@radustanciu` gets `COUCHFUSION_AGENT_MARKER`. The marker only has an effect once the Phase 2 `layers/content` change (Stream C) is synced to the workspace.

## Verify

- `GET /api/v1/bootstrap` lists `component-trash`, `component-source`, `registry-rebuild`, `services`.
- Components drawer → Rebuild registry → job `component-registry.rebuild` succeeds; `journalctl -u cf-registry@radustanciu -n 5`.
- Trash tab lists earlier deletes (e.g. `zz-ui-smoke`); Restore puts the file back and the builder reloads with it.
- Services page lists the allowlisted units with state and recent (redacted) logs.
- During a Codex run `ls -l /run/couchfusion/dev/` shows `radustanciu.agent-running`; it disappears when the job ends.
- Through the SSH tunnel, site events carry `http://localhost:9181` (never the tailnet URL).
