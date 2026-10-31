#!/usr/bin/env bash
# /usr/local/libexec/couchfusion/couchdb-restore.sh <dump.json.gz> <target-db> [--into-existing]
# Restores one database dump made by couchdb-backup.sh, keeping document revisions
# (_bulk_docs with new_edits=false), in batches of 500. Refuses a non-empty target unless
# --into-existing is given (then existing revisions are kept and the dump's are merged in).
# Credentials: COUCHDB_ADMIN_AUTH from the environment, e.g.
#   set -a; . /etc/couchfusion/env/radustanciu.prod.env; set +a
set -euo pipefail
dump=${1:?dump file}; target=${2:?target database}; mode=${3:-}
: "${COUCHDB_ADMIN_AUTH:?missing}"
BASE=${COUCHDB_URL:-http://127.0.0.1:5984}
cfg=$(mktemp); trap 'rm -f "$cfg"' EXIT
printf 'header = "Authorization: Basic %s"\nheader = "Content-Type: application/json"\n' "$COUCHDB_ADMIN_AUTH" > "$cfg"
enc=$(jq -rn --arg s "$target" '$s|@uri')
info=$(curl -s -K "$cfg" "$BASE/$enc")
if jq -e '.doc_count' >/dev/null 2>&1 <<<"$info"; then
  n=$(jq '.doc_count + .doc_del_count' <<<"$info")
  [ "$n" -eq 0 ] || [ "$mode" = --into-existing ] || { echo "$target exists with $n docs; pass --into-existing to merge" >&2; exit 2; }
else
  curl -sS --fail -K "$cfg" -X PUT "$BASE/$enc" >/dev/null
fi
total=0
while IFS= read -r batch; do
  res=$(curl -sS --fail -K "$cfg" -X POST "$BASE/$enc/_bulk_docs" --data-binary @- <<<"$batch")
  bad=$(jq '[.[] | select(.error)] | length' <<<"$res")
  [ "$bad" -eq 0 ] || { echo "errors in batch: $(jq -c '[.[] | select(.error)][:3]' <<<"$res")" >&2; exit 1; }
  total=$((total + $(jq '.docs | length' <<<"$batch")))
done < <(zcat "$dump" | jq -c '[.rows[].doc] | _nwise(500) | {new_edits: false, docs: .}')
echo "restored $total docs into $target"
