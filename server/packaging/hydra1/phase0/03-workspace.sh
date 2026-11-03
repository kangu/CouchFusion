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
  # Work tree only: .git directories are never touched here (Phase 1 hands them to cfcp).
  NOGIT=(-path '*/.git' -prune -o)
  find "$CF_WORKSPACE" "${NOGIT[@]}" -exec chown radu:cfsrc {} +
  find "$CF_WORKSPACE" "${NOGIT[@]}" -exec chmod g+rwX,o-rwx {} +
  find "$CF_WORKSPACE" "${NOGIT[@]}" -type d -exec chmod g+s {} +
  find "$CF_WORKSPACE" "${NOGIT[@]}" -type d -exec setfacl -m d:g:cfsrc:rwX -m g:cfsrc:rwX {} +
  find "$CF_WORKSPACE" "${NOGIT[@]}" -type f -exec setfacl -m g:cfsrc:rwX {} +

  while IFS= read -r gitdir; do
    if [ "$(stat -c %U "$gitdir")" = cfcp ]; then log "keeping $gitdir (owned by cfcp, Phase 1)"; continue; fi
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
