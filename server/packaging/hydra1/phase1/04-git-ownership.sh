#!/usr/bin/env bash
# Step 04 — each repo's .git is owned by cfcp (UI commits) with an ACL so radu can still commit
# over SSH; cfsrc (dev/agent code) gets no write access to .git. Also marks the repos safe for radu.
# Undo: chown -R radu:radu <repo>/.git; setfacl -R -b <repo>/.git
source "$(dirname "$0")/lib.sh"
require_root
# Run from / so sudo -u <user> commands never trip over an unreadable cwd (e.g. /root).
cd /
for repo in "${CF_REPOS[@]}"; do
  g=$CF_WORKSPACE/$repo/.git
  [ -d "$g" ] || { warn "no $g"; continue; }
  chown -R cfcp:cfcp "$g"
  chmod -R u+rwX,g-w,o-rwx "$g"
  setfacl -R -b "$g"
  setfacl -R -m u:radu:rwX -m d:u:radu:rwX "$g"
  # Git replaces the index via rename. Files created by radu must also grant cfcp access.
  find "$g" -type d -exec setfacl -m d:u:cfcp:rwx {} +
  sudo -u radu git config --global --get-all safe.directory 2>/dev/null | grep -qx "$CF_WORKSPACE/$repo" \
    || sudo -u radu git config --global --add safe.directory "$CF_WORKSPACE/$repo"
  # A seed from macOS carries core.ignorecase=true; this disk is case-sensitive.
  sudo -u cfcp git -C "$CF_WORKSPACE/$repo" -c safe.directory="$CF_WORKSPACE/$repo" config core.ignorecase false
  log "$repo: .git owned by cfcp, radu via ACL, core.ignorecase=false"
done
sudo -u cfcp git -C $CF_WORKSPACE/apps/radustanciu -c safe.directory=$CF_WORKSPACE/apps/radustanciu status --short | head -5 || warn "cfcp git status failed"
