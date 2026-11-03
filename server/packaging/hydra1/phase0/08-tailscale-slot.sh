#!/usr/bin/env bash
# Step 08 — publish the dev slot on the tailnet (tailnet only, HTTPS, like Hermes on :9119).
# Phase 0 points the slot straight at nuxt dev for spike S2. In Phase 1 the Rust proxy
# takes over the slot and this mapping is replaced.
# Undo: tailscale serve --https=9181 off
source "$(dirname "$0")/lib.sh"
require_root

tailscale serve --bg --https="$CF_DEV_SLOT" "http://127.0.0.1:$CF_DEV_PORT"
log "tailscale serve now:"; tailscale serve status | sed 's/^/  /'
log "open https://hydra1.tailb7471b.ts.net:$CF_DEV_SLOT/ from a tailnet device once cf-dev@$CF_APP is running"
