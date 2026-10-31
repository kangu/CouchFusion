#!/usr/bin/env bash
# Step 01 — system users and the shared source group.
#   cfcp    control plane (no service yet in Phase 0; created now so ownership is right)
#   cfdev   runs nuxt dev and builds
#   cfagent runs Codex
#   cfsrc   group that may write the work tree (cfdev, cfagent, radu)
# Undo: userdel cfcp cfdev cfagent; groupdel cfsrc; gpasswd -d radu cfsrc
source "$(dirname "$0")/lib.sh"
require_root

getent group cfsrc >/dev/null || { groupadd --system cfsrc; log "created group cfsrc"; }

make_user() {
  local name=$1 home=$2
  if getent passwd "$name" >/dev/null; then
    log "user $name exists"
  else
    useradd --system --home-dir "$home" --create-home --shell /usr/sbin/nologin --user-group "$name"
    log "created user $name ($home)"
  fi
  chmod 0750 "$home"
}

make_user cfcp /var/lib/couchfusion
make_user cfdev /var/lib/cfdev
make_user cfagent /var/lib/cfagent

for u in cfdev cfagent radu; do
  if id -nG "$u" | tr ' ' '\n' | grep -qx cfsrc; then log "$u already in cfsrc"; else usermod -aG cfsrc "$u"; log "added $u to cfsrc"; fi
done
log "note: radu's existing SSH sessions need a re-login to pick up the cfsrc group"
