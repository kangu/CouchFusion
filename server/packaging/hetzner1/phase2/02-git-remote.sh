#!/usr/bin/env bash
# Phase 2 step 02 — git remote for apps/radustanciu (fetch / fast-forward pull / push from the UI).
#
#   - /etc/couchfusion/credentials/git-apps-radustanciu  (root:root 0600; created EMPTY if missing)
#       git credential format, filled by you with:  sudoedit /etc/couchfusion/credentials/git-apps-radustanciu
#         username=<forgejo user>
#         password=<forgejo access token with repository write scope>
#   - couchfusion.service drop-in: LoadCredential= hands that file to the service only
#     (it appears as $CREDENTIALS_DIRECTORY/git-apps-radustanciu, readable by cfcp, nobody else)
#   - [[git.remotes]] entry in /etc/couchfusion/server.toml (appended once; existing entries kept)
#
# The token is never put in argv, env or logs: git gets a credential helper scoped to the remote's
# host that `cat`s the file. Push is never forced and only `main` is allowed.
#
# After filling the credential:  systemctl restart couchfusion   (credentials are copied at start)
# Undo: rm /etc/systemd/system/couchfusion.service.d/20-git-credentials.conf; remove the
#       [git]/[[git.remotes]] lines from server.toml; systemctl daemon-reload; systemctl restart couchfusion
source "$(dirname "$0")/../phase1/lib.sh"
require_root
cd /

REPO=apps/radustanciu
CRED=git-apps-radustanciu
URL=${CF_GIT_URL:-https://git2.kangu.ro/radu/radustanciu-src.git}
BRANCHES=${CF_GIT_BRANCHES:-'["main"]'}
CRED_FILE=$CF_ETC/credentials/$CRED

install -d -m 0700 -o root -g root "$CF_ETC/credentials"
if [ -f "$CRED_FILE" ]; then
  log "keeping existing $CRED_FILE ($( [ -s "$CRED_FILE" ] && echo filled || echo EMPTY))"
else
  install -m 0600 -o root -g root /dev/null "$CRED_FILE"
  log "created EMPTY $CRED_FILE — fill it with: sudoedit $CRED_FILE"
fi

install -d -m 0755 -o root -g root /etc/systemd/system/couchfusion.service.d
DROPIN=$(mktemp)
printf '[Service]\n# Git push/pull credential for %s (see phase2/02-git-remote.sh)\nLoadCredential=%s:%s\n' "$REPO" "$CRED" "$CRED_FILE" > "$DROPIN"
install_file "$DROPIN" /etc/systemd/system/couchfusion.service.d/20-git-credentials.conf 0644
rm -f "$DROPIN"

if grep -q '^\[\[git.remotes\]\]' "$CF_ETC/server.toml"; then
  log "server.toml already has [[git.remotes]]; not changing it"
else
  cat >> "$CF_ETC/server.toml" <<TOML

[git]

[[git.remotes]]
repo = "$REPO"
remote = "origin"
url = "$URL"
credential = "$CRED"
branches = $BRANCHES
TOML
  log "appended [[git.remotes]] for $REPO to server.toml"
fi

# The repo's origin must match the configured URL exactly (the server refuses otherwise).
G="sudo -u cfcp git -c safe.directory=$CF_WORKSPACE/$REPO -C $CF_WORKSPACE/$REPO"
actual=$($G remote get-url origin 2>/dev/null || true)
[ "$actual" = "$URL" ] && log "origin of $REPO matches ($URL)" || warn "origin of $REPO is '$actual', expected $URL"

systemctl daemon-reload
sudo -u cfcp "$CF_ROOT/current/couchfusion-server" doctor --config "$CF_ETC/server.toml" >/dev/null 2>&1 \
  && log "server.toml valid (doctor)" || warn "doctor reported problems: sudo -u cfcp $CF_ROOT/current/couchfusion-server doctor"
if [ -s "$CRED_FILE" ]; then
  systemctl restart couchfusion && log "couchfusion restarted with the credential"
else
  log "next: fill the credential, then: systemctl restart couchfusion"
fi
