#!/usr/bin/env bash
# Step 05 — start the control plane, run doctor, and restart dev through the API so the dev
# server gets a control token (needed for page-usage scans and the builder bridge).
# Undo: systemctl disable --now couchfusion.service
source "$(dirname "$0")/lib.sh"
require_root
# Run from / so sudo -u <user> commands never trip over an unreadable cwd (e.g. /root).
cd /
sudo -u cfcp $CF_ROOT/current/couchfusion-server doctor --config $CF_ETC/server.toml || warn "doctor reported problems (see above)"
systemctl enable --now couchfusion.service
for _ in $(seq 1 50); do [ -S /run/couchfusion/ts-9180.sock ] && break; sleep 0.2; done
systemctl is-active couchfusion.service
LOGIN=$(sed -n 's/^allowed_logins = \["\([^"]*\)".*/\1/p' $CF_ETC/server.toml)
api() { curl -s --unix-socket /run/couchfusion/ts-9180.sock -H "X-Forwarded-Host: hetzner1.tailb7471b.ts.net:9180" -H "Tailscale-User-Login: $LOGIN" "$@"; }
api http://localhost/api/v1/bootstrap; echo
log "restarting dev via the API (new control token)…"
api -X POST -H 'Content-Type: application/json' -d '{}' http://localhost/api/v1/workspaces/hetzner1/sites/radustanciu/dev/restart; echo
for _ in $(seq 1 120); do
  st=$(api http://localhost/api/v1/workspaces/hetzner1/sites/radustanciu/dev | sed -E 's/.*"status":"([a-z]+)".*/\1/')
  [ "$st" = ready ] || [ "$st" = failed ] && break; sleep 2
done
log "dev status: $st"
