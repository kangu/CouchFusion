#!/usr/bin/env bash
# Step 12 — runs ON THE LAPTOP. Copies radustanciu's data from hetzner1's CouchDB (the current
# primary since 2026-10-29) into hydra1's CouchDB over the tailnet. Dev and prod share it there.
#
# Copies ONLY:
#   databases   rs--content, rs--logins, analytics_radustanciu   (+ their _security objects)
#   _config     section cf_env_radustanciu (key by key; values are never printed)
# Skips analytics_radustanciu_raw_* unless --with-raw-analytics.
#
# Safety:
#   - refuses if a target database already exists on hydra1, unless --merge
#     (replication then adds/updates docs; it never deletes docs on the target)
#   - refuses to overwrite existing cf_env_radustanciu keys on hydra1, unless --merge
#   - --dry-run shows the plan and touches nothing on hydra1
#   - hetzner1 is only read
#
# Credentials: CouchDB admin user + password for hetzner1 and for hydra1, typed at the prompts;
# kept in memory only. The replication runs inside hetzner1's CouchDB, which pushes to hydra1
# over the tailnet (WireGuard-encrypted).
#
# Stop cf-dev@radustanciu on hetzner1 first (or re-run with --merge at the end) so no content
# edit lands on hetzner1 after the copy.
#
# Usage: ./12-copy-couchdb-from-hetzner1.sh [--dry-run] [--merge] [--with-raw-analytics]
set -euo pipefail

APP=${CF_APP:-radustanciu}
LOCAL=${CF_SOURCE_COUCH:-http://hetzner1.tailb7471b.ts.net:5984}
# The replication runs inside hetzner1's CouchDB, which reads its own databases on loopback.
LOCAL_INNER=${CF_SOURCE_INNER:-http://127.0.0.1:5984}
REMOTE=${CF_REMOTE_COUCH:-http://hydra1.tailb7471b.ts.net:5984}
SECTION=cf_env_$APP
DBS=(rs--content rs--logins analytics_$APP)

DRY=0 MERGE=0 RAW=0
for a in "$@"; do
  case $a in
    --dry-run) DRY=1 ;;
    --merge) MERGE=1 ;;
    --with-raw-analytics) RAW=1 ;;
    *) echo "unknown arg $a" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;34m[copy]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[copy] error:\033[0m %s\n' "$*" >&2; exit 1; }
command -v jq >/dev/null || die "jq is required"

read -r -p "hetzner1 CouchDB admin user: " LUSER
read -r -s -p "hetzner1 CouchDB admin password: " LPASS; echo
LOCAL_AUTH=$(printf '%s:%s' "$LUSER" "$LPASS" | base64)
unset LPASS
lc() { curl -sS -m 30 -H "Authorization: Basic $LOCAL_AUTH" -H 'Content-Type: application/json' "$@"; }

if [ $RAW -eq 1 ]; then
  while IFS= read -r db; do DBS+=("$db"); done < <(lc "$LOCAL/_all_dbs" | jq -r ".[] | select(startswith(\"analytics_${APP}_raw_\"))")
fi

[ "$(lc "$LOCAL/_session" | jq -r '.userCtx.roles | index("_admin") != null')" = true ] \
  || die "those hetzner1 credentials are not a CouchDB server admin"

say "source  $LOCAL"
for db in "${DBS[@]}"; do
  info=$(lc "$LOCAL/$db")
  [ "$(jq -r '.error // empty' <<<"$info")" = "" ] || die "database $db not found on hetzner1"
  say "  $db: $(jq -r '.doc_count' <<<"$info") docs"
done
say "  _config $SECTION: $(lc "$LOCAL/_node/_local/_config/$SECTION" | jq -r 'keys | join(", ")')"

say "target  $REMOTE"
if [ $DRY -eq 1 ]; then
  for db in "${DBS[@]}"; do
    code=$(curl -s -m 10 -o /dev/null -w '%{http_code}' -I "$REMOTE/$db")
    say "  $db: $([ "$code" = 404 ] && echo 'will be created' || echo "exists or protected (HTTP $code)")"
  done
  say "dry run: nothing changed"
  exit 0
fi

read -r -p "hydra1 CouchDB admin user: " RUSER
read -r -s -p "hydra1 CouchDB admin password: " RPASS; echo
REMOTE_AUTH=$(printf '%s:%s' "$RUSER" "$RPASS" | base64)
unset RPASS
rc() { curl -sS -m 30 -H "Authorization: Basic $REMOTE_AUTH" -H 'Content-Type: application/json' "$@"; }

[ "$(rc "$REMOTE/_session" | jq -r '.userCtx.roles | index("_admin") != null')" = true ] \
  || die "those hydra1 credentials are not a CouchDB server admin"

for db in "${DBS[@]}"; do
  code=$(rc -o /dev/null -w '%{http_code}' -I "$REMOTE/$db")
  if [ "$code" = 200 ] && [ $MERGE -eq 0 ]; then die "$db already exists on hydra1 (re-run with --merge to add/update docs)"; fi
done
existing=$(rc "$REMOTE/_node/_local/_config/$SECTION" | jq -r 'if type=="object" and (.error|not) then keys | join(", ") else "" end')
if [ -n "$existing" ] && [ $MERGE -eq 0 ]; then die "$SECTION already has keys on hydra1 ($existing); re-run with --merge to overwrite"; fi

for db in "${DBS[@]}"; do
  say "replicating $db"
  body=$(jq -n --arg s "$LOCAL_INNER/$db" --arg t "$REMOTE/$db" --arg la "Basic $LOCAL_AUTH" --arg ra "Basic $REMOTE_AUTH" \
    '{source:{url:$s,headers:{Authorization:$la}}, target:{url:$t,headers:{Authorization:$ra}}, create_target:true}')
  result=$(lc -m 600 -X POST "$LOCAL/_replicate" -d "$body")
  [ "$(jq -r '.ok // false' <<<"$result")" = true ] || die "replication of $db failed: $(jq -c '{error,reason}' <<<"$result")"
  say "  written: $(jq -r '.history[0].docs_written // 0' <<<"$result"), failures: $(jq -r '.history[0].doc_write_failures // 0' <<<"$result")"

  sec=$(lc "$LOCAL/$db/_security")
  rc -X PUT "$REMOTE/$db/_security" -d "$sec" | jq -e '.ok' >/dev/null || die "could not set _security on $db"
  say "  _security copied"

  lcount=$(lc "$LOCAL/$db" | jq -r .doc_count); rcount=$(rc "$REMOTE/$db" | jq -r .doc_count)
  say "  doc_count hetzner1=$lcount hydra1=$rcount"
done

say "copying _config $SECTION"
lc "$LOCAL/_node/_local/_config/$SECTION" | jq -r 'to_entries[] | @base64' | while IFS= read -r row; do
  key=$(jq -r '@base64d | fromjson | .key' <<<"\"$row\"")
  val=$(jq -c '@base64d | fromjson | .value' <<<"\"$row\"")
  rc -X PUT "$REMOTE/_node/_local/_config/$SECTION/$key" -d "$val" >/dev/null
  printf '  %s set\n' "$key"
done

unset REMOTE_AUTH LOCAL_AUTH
say "done."
