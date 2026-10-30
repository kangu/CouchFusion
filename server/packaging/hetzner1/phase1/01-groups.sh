#!/usr/bin/env bash
# Step 01 — cfcp joins cfsrc (work tree) and systemd-journal (dev logs).
# Undo: gpasswd -d cfcp cfsrc; gpasswd -d cfcp systemd-journal
source "$(dirname "$0")/lib.sh"
require_root
for g in cfsrc systemd-journal; do
  if id -nG cfcp | tr ' ' '\n' | grep -qx $g; then log "cfcp already in $g"; else usermod -aG $g cfcp; log "added cfcp to $g"; fi
done
