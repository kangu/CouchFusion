#!/usr/bin/env bash
# Step 05 — preview production over the tailnet before DNS: https://<host>:9189 -> 127.0.0.1:4501.
# Tailnet only (tailscale serve, not funnel). Remove after the cut-over.
# Undo: tailscale serve --https=9189 off
source "$(dirname "$0")/lib.sh"
require_root
tailscale serve --bg --https="$CF_PREVIEW_PORT" "http://127.0.0.1:$CF_PROD_PORT"
tailscale serve status | sed -n 1,40p
