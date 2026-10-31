#!/usr/bin/env bash
# Step 07 — tighten Caddy site file permissions (one file holds an inline API key).
# Makes the site files readable by root and the caddy group only. Caddy is NOT reloaded:
# a permission change does not need it, and the check below confirms caddy can still read them.
# Undo: chmod 0644 /etc/caddy/sites/*.caddy; chown root:root /etc/caddy/sites/*.caddy
source "$(dirname "$0")/lib.sh"
require_root

caddy_user=$(systemctl show -p User --value caddy.service)
caddy_user=${caddy_user:-root}
log "caddy.service runs as: $caddy_user"

shopt -s nullglob
sites=(/etc/caddy/sites/*.caddy)
if [ ${#sites[@]} -eq 0 ]; then
  log "no /etc/caddy/sites/*.caddy yet; nothing to tighten (re-run after adding site files)"
  exit 0
fi
log "before:"; ls -l /etc/caddy/sites/
chown root:caddy /etc/caddy/sites/*.caddy
chmod 0640 /etc/caddy/sites/*.caddy
log "after:"; ls -l /etc/caddy/sites/

if [ "$caddy_user" != root ]; then
  for f in /etc/caddy/sites/*.caddy; do
    sudo -u "$caddy_user" test -r "$f" || die "$caddy_user cannot read $f — revert with the undo line above"
  done
fi
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 \
  && log "caddy validate: ok" || warn "caddy validate reported a problem (run it manually to see why)"
