#!/usr/bin/env bash
# Phase 2 step 02 — git remote for one workspace repo (fetch / fast-forward pull / push from the UI).
# Default repo apps/radustanciu; another repo:  CF_GIT_REPO=layers ./02-git-remote.sh
#
#   - /etc/couchfusion/credentials/git-<repo with / as ->  (root:root 0600; created EMPTY if missing)
#       git credential format, filled by you with:  sudoedit /etc/couchfusion/credentials/<that name>
#         username=<forgejo user>
#         password=<forgejo access token with repository write scope>
#   - couchfusion.service drop-in: LoadCredential= hands that file to the service only
#     (it appears as $CREDENTIALS_DIRECTORY/git-apps-radustanciu, readable by cfcp, nobody else)
#   - [[git.remotes]] entry for the repo in /etc/couchfusion/server.toml (appended once; existing entries kept)
#
# The token is never put in argv, env or logs: git gets a credential helper scoped to the remote's
# host that `cat`s the file. Push is never forced and only `main` is allowed.
#
# After filling the credential:  systemctl restart couchfusion   (credentials are copied at start)
# Undo: rm the repo's drop-in (apps/radustanciu: 20-git-credentials.conf, others:
#       20-git-credentials-<cred>.conf) in /etc/systemd/system/couchfusion.service.d/; remove its
#       [[git.remotes]] block from server.toml; systemctl daemon-reload; systemctl restart couchfusion
source "$(dirname "$0")/../phase1/lib.sh"
require_root
cd /

REPO=${CF_GIT_REPO:-apps/radustanciu}
CRED=git-${REPO//\//-}
case "$REPO" in
  apps/radustanciu) DEFAULT_URL=https://git2.kangu.ro/radu/radustanciu-src.git; DROPIN_NAME=20-git-credentials.conf ;;
  layers)           DEFAULT_URL=https://git2.kangu.ro/radu/nuxt-apps-layers.git; DROPIN_NAME=20-git-credentials-$CRED.conf ;;
  *)                DEFAULT_URL=; DROPIN_NAME=20-git-credentials-$CRED.conf ;;
esac
URL=${CF_GIT_URL:-$DEFAULT_URL}
[ -n "$URL" ] || die "set CF_GIT_URL for $REPO"
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
install_file "$DROPIN" "/etc/systemd/system/couchfusion.service.d/$DROPIN_NAME" 0644
rm -f "$DROPIN"

if grep -qx "repo = \"$REPO\"" "$CF_ETC/server.toml"; then
  log "server.toml already has a [[git.remotes]] entry for $REPO; not changing it"
elif grep -q '^\[\[git.remotes\]\]' "$CF_ETC/server.toml"; then
  # Append after the last [[git.remotes]] block, before the next top-level table.
  cp -p "$CF_ETC/server.toml" "$CF_ETC/server.toml.bak-git-$CRED"
  python3 - "$CF_ETC/server.toml" "$REPO" "$URL" "$CRED" "$BRANCHES" <<'PY'
import re, sys
path, repo, url, cred, branches = sys.argv[1:]
text = open(path).read()
block = f'[[git.remotes]]\nrepo = "{repo}"\nremote = "origin"\nurl = "{url}"\ncredential = "{cred}"\nbranches = {branches}\n\n'
starts = [m.start() for m in re.finditer(r'^\[\[git\.remotes\]\]$', text, re.M)]
nxt = re.compile(r'^(#.*\n)*\[(?!\[git\.remotes\]\])', re.M).search(text, starts[-1] + 1)
pos = nxt.start() if nxt else len(text)
if not nxt and not text.endswith('\n\n'):
    block = ('\n' if text.endswith('\n') else '\n\n') + block.rstrip('\n') + '\n'
open(path, 'w').write(text[:pos] + block + text[pos:])
PY
  log "added [[git.remotes]] for $REPO to server.toml (backup server.toml.bak-git-$CRED)"
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
