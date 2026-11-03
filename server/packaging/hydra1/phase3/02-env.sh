#!/usr/bin/env bash
# Step 02 — production env files, derived from the dev env box-to-box (values never printed).
#   $CF_APP.prod.env   root:cf-site-<app> 0640  runtime secrets for cf-site@<app> (cfcp cannot read)
#   $CF_APP.build.env  root:cfdev 0640          non-secret build-time values for cf-build@<app>
# Dev and production share hydra1's CouchDB (spec D1), so the CouchDB values are the same.
# Existing files are kept; delete one to regenerate it.
# Undo: rm /etc/couchfusion/env/radustanciu.{prod,build}.env
source "$(dirname "$0")/lib.sh"
require_root
DEV=$CF_ETC/env/$CF_APP.dev.env
PROD=$CF_ETC/env/$CF_APP.prod.env
BUILD=$CF_ETC/env/$CF_APP.build.env
SITE_URL=${CF_SITE_URL:-https://radustanciu.ro}
[ -f "$DEV" ] || die "missing $DEV"
id "$CF_SITE_USER" >/dev/null 2>&1 || die "run 01-site-user.sh first"

if [ -f "$PROD" ]; then
  log "keeping existing $PROD"
else
  tmp=$(mktemp); chmod 0600 "$tmp"
  {
    echo "# Production env for $CF_APP (cf-site@$CF_APP). Edit with: sudoedit $PROD"
    echo "# Generated from $DEV by phase3/02-env.sh. Never copy into the workspace."
    echo "NITRO_HOST=127.0.0.1"
    echo "NITRO_PORT=$CF_PROD_PORT"
    echo "PUBLIC_SITE_URL=$SITE_URL"
    # Copy runtime values, dropping dev-only keys (port, slot URL, Vite hosts, control plane).
    awk -F= '/^(COUCHDB_URL|COUCHDB_ADMIN_AUTH|COUCHDB_COOKIE_SECRET|IMAGEKIT_[A-Z_]+|NUXT_IMAGEKIT_FOLDER|NUXT_PUBLIC_UMAMI_[A-Z_]+)=/' "$DEV"
    # runtimeConfig keys baked at build time with placeholders get their real values from
    # Nuxt's NUXT_* overrides at runtime.
    awk -F= '$1=="COUCHDB_URL" && $2!="" {print "NUXT_COUCH_URL=" substr($0, index($0,"=")+1)}
             $1=="COUCHDB_ADMIN_AUTH" && $2!="" {print "NUXT_COUCH_ADMIN_AUTH=" substr($0, index($0,"=")+1)}
             $1=="COUCHDB_COOKIE_SECRET" && $2!="" {print "NUXT_COUCHDB_COOKIE_SECRET=" substr($0, index($0,"=")+1)}' "$DEV"
  } > "$tmp"
  grep -q '^COUCHDB_ADMIN_AUTH=.' "$tmp" || { rm -f "$tmp"; die "dev env has no COUCHDB_ADMIN_AUTH value"; }
  install -m 0640 -o root -g "$CF_SITE_USER" "$tmp" "$PROD"; rm -f "$tmp"
  log "wrote $PROD ($(grep -c '=' "$PROD") keys; values not shown)"
fi

if [ -f "$BUILD" ]; then
  log "keeping existing $BUILD"
else
  tmp=$(mktemp)
  {
    echo "# Non-secret build-time values for cf-build@$CF_APP. Never put secrets here."
    echo "PUBLIC_SITE_URL=$SITE_URL"
    awk -F= '/^NUXT_PUBLIC_UMAMI_[A-Z_]+=/' "$DEV"
  } > "$tmp"
  install -m 0640 -o root -g cfdev "$tmp" "$BUILD"; rm -f "$tmp"
  log "wrote $BUILD"
fi
stat -c '%U:%G %a %n' "$PROD" "$BUILD"
log "keys in $PROD: $(sed -n 's/^\([A-Z_]*\)=.*/\1/p' "$PROD" | tr '\n' ' ')"
