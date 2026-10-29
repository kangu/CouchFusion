#!/usr/bin/env bash
# Step 04 — install JS dependencies as cfdev (never root), so node_modules is owned by the
# sandboxed user and postinstall scripts cannot run as root. Also spike S6 (better-sqlite3 builds).
source "$(dirname "$0")/lib.sh"
require_root

# COUCHDB_ADMIN_AUTH below is a dummy ("prepare-only:placeholder"): the app's postinstall runs
# `nuxt prepare`, and the auth layer refuses to load without the variable set. Prepare only
# generates types and never connects to CouchDB. The real value lives in the dev env file only.
as_cfdev() {
  local dir=$1; shift
  sudo -u cfdev -g cfsrc env -i \
    HOME=/var/lib/cfdev \
    PATH="$CF_TOOLCHAIN/bin:/usr/bin:/bin" \
    NUXT_TELEMETRY_DISABLED=1 \
    CONTENT_REGISTRY_WATCH=0 \
    COUCHDB_ADMIN_AUTH=cHJlcGFyZS1vbmx5OnBsYWNlaG9sZGVy \
    bash -c 'umask 0002; cd "$0" && "$@"' "$dir" "$@"
}

for dir in "$CF_WORKSPACE/layers" "$CF_WORKSPACE/apps/$CF_APP"; do
  [ -f "$dir/package.json" ] || die "missing $dir/package.json — run the laptop seed first"
  log "bun install in $dir"
  if ! as_cfdev "$dir" bun install --frozen-lockfile; then
    warn "frozen install failed in $dir; retrying without --frozen-lockfile (the lockfile may change — review with git status)"
    as_cfdev "$dir" bun install
  fi
done

log "S6: better-sqlite3 native module"
if as_cfdev "$CF_WORKSPACE/apps/$CF_APP" node -e "new (require('better-sqlite3'))(':memory:').prepare('select 1').get(); console.log('  ok')"; then
  log "S6 pass"
else
  warn "S6 fail: better-sqlite3 did not load"
fi
