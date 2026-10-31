#!/usr/bin/env bash
# /usr/local/libexec/couchfusion/couchdb-backup.sh — nightly CouchDB dump (spec §9.4).
# Every database (_all_docs with docs and inline attachments) plus the node's _config, gzip'd,
# into /var/backups/couchdb/<UTC date>/ (root, plus group cfbackup read-only). Keeps $KEEP_DAYS days.
# Run by couchdb-backup.service (timer, 03:30) or by hand: systemctl start couchdb-backup
set -euo pipefail
: "${COUCHDB_ADMIN_AUTH:?missing (EnvironmentFile)}"
BASE=${COUCHDB_URL:-http://127.0.0.1:5984}
DEST_ROOT=${BACKUP_DIR:-/var/backups/couchdb}
KEEP_DAYS=${KEEP_DAYS:-14}
umask 027
# Group cfbackup (if present) may read the dumps: the laptop pulls them read-only (08-offbox-pull.sh).
GROUP=$(getent group cfbackup >/dev/null && echo cfbackup || echo root)
day=$(date -u +%Y-%m-%d)
tmp="$DEST_ROOT/.$day.partial"
rm -rf "$tmp"; mkdir -p "$tmp"
cfg=$(mktemp); trap 'rm -f "$cfg"; rm -rf "$tmp"' EXIT
printf 'header = "Authorization: Basic %s"\n' "$COUCHDB_ADMIN_AUTH" > "$cfg"
get() { curl -sS --fail -K "$cfg" "$BASE$1"; }

manifest='{}'
for db in $(get /_all_dbs | jq -r '.[]'); do
  enc=$(jq -rn --arg s "$db" '$s|@uri')
  file="$tmp/$(printf %s "$db" | tr '/' '%').json.gz"
  get "/$enc/_all_docs?include_docs=true&attachments=true" | gzip -9 > "$file"
  count=$(zcat "$file" | jq '.rows | length')
  manifest=$(jq -c --arg db "$db" --argjson n "$count" --arg f "$(basename "$file")" '. + {($db): {docs: $n, file: $f}}' <<<"$manifest")
  echo "dumped $db ($count docs)"
done
get /_node/_local/_config | gzip -9 > "$tmp/_config.json.gz"
jq -n --arg at "$(date -u +%FT%TZ)" --argjson dbs "$manifest" --arg version "$(get / | jq -r .version)" \
  '{created: $at, couchdb: $version, databases: $dbs}' > "$tmp/manifest.json"

chgrp -R "$GROUP" "$tmp"
rm -rf "${DEST_ROOT:?}/$day"; mv "$tmp" "$DEST_ROOT/$day"; trap 'rm -f "$cfg"' EXIT
find "$DEST_ROOT" -mindepth 1 -maxdepth 1 -type d -name '20??-??-??' -mtime +"$KEEP_DAYS" -exec rm -rf {} +
echo "backup $DEST_ROOT/$day: $(du -sh "$DEST_ROOT/$day" | cut -f1), $(ls "$DEST_ROOT" | grep -c '^20') days kept"
