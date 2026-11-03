#!/usr/bin/env bash
# Step 06 — publish the control plane (9180) and move the dev slot (9181) from the Phase 0 direct
# mapping (→ 127.0.0.1:4401) to the Rust slot proxy socket. Tailnet only.
# Note (spike S3): on unix targets the backend sees Host=localhost; the real host is in
# X-Forwarded-Host, and Tailscale-User-Login is set by tailscaled (client values are overwritten).
# Undo: tailscale serve --https=9180 off; tailscale serve --https=9181 off;
#       tailscale serve --bg --https=9181 http://127.0.0.1:4401   (back to Phase 0)
source "$(dirname "$0")/lib.sh"
require_root
for p in "${CF_PORTS[@]}"; do
  [ -S /run/couchfusion/ts-$p.sock ] || die "/run/couchfusion/ts-$p.sock missing — start couchfusion.service first (05-start.sh)"
done
tailscale serve --https=9181 off >/dev/null 2>&1 || true
for p in "${CF_PORTS[@]}"; do tailscale serve --bg --https=$p unix:/run/couchfusion/ts-$p.sock; done
tailscale serve status | sed 's/^/  /'
log "open https://hetzner1.tailb7471b.ts.net:9180/ from the laptop (tailnet login: see server.toml)"
