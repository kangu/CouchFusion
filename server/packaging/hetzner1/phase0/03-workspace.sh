#!/usr/bin/env bash
# Step 03 — workspace directory and permissions.
# Run once before the laptop seed (sync-from-laptop.sh), and again after it to fix ownership.
#   - work tree: owner radu, group cfsrc, group-writable, setgid dirs, default ACL so new files stay group-writable
#   - every .git: owner radu, NOT writable by cfsrc (agent/dev code must not edit git config or hooks)
# Undo: rm -rf /srv/couchfusion
source "$(dirname "$0")/lib.sh"
require_root

install -d -m 0755 -o root -g root /srv/couchfusion
install -d -m 2775 -o radu -g cfsrc "$CF_WORKSPACE" "$CF_WORKSPACE/apps"

if [ -n "$(ls -A "$CF_WORKSPACE/apps" 2>/dev/null)" ] || [ -d "$CF_WORKSPACE/layers" ]; then
  log "fixing ownership and modes under $CF_WORKSPACE"
  chown -R radu:cfsrc "$CF_WORKSPACE"
  chmod -R g+rwX,o-rwx "$CF_WORKSPACE"
  find "$CF_WORKSPACE" -type d -not -path '*/.git/*' -not -name .git -exec chmod g+s {} +
  setfacl -R -m d:g:cfsrc:rwX -m g:cfsrc:rwX "$CF_WORKSPACE"

  while IFS= read -r gitdir; do
    setfacl -R -b "$gitdir"
    chmod -R g-w,o-rwx "$gitdir"
    find "$gitdir" -type d -exec chmod g-s {} +
    log "locked $gitdir (read-only for cfsrc)"
  done < <(find "$CF_WORKSPACE" -maxdepth 3 -name .git -type d -not -path '*/node_modules/*')

  # Mark the repos safe for radu's own git use (owner is radu, so this only matters for root/cfcp later).
  log "workspace repos:"
  find "$CF_WORKSPACE" -maxdepth 3 -name .git -type d -not -path '*/node_modules/*' -printf '  %h\n'
else
  log "workspace is empty — run sync-from-laptop.sh on the laptop, then re-run this step"
fi

if find "$CF_WORKSPACE" -maxdepth 3 -name '.env*' -not -path '*/node_modules/*' | grep -q .; then
  warn "found .env files in the workspace; secrets belong in $CF_ETC/env/ only:"
  find "$CF_WORKSPACE" -maxdepth 3 -name '.env*' -not -path '*/node_modules/*' | sed 's/^/  /'
fi
