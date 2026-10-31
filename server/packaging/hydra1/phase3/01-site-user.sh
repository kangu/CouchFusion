#!/usr/bin/env bash
# Step 01 — the production user (runs cf-site@<app>; owns nothing, reads its releases).
# Undo: userdel cf-site-radustanciu
source "$(dirname "$0")/lib.sh"
require_root
if id "$CF_SITE_USER" >/dev/null 2>&1; then
  log "user $CF_SITE_USER exists"
else
  useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin --user-group "$CF_SITE_USER"
  log "created $CF_SITE_USER"
fi
id "$CF_SITE_USER"
