#!/usr/bin/env bash
# Step 07 — nightly CouchDB backups (spec §9.4): every database + _config, 14 days, root only,
# in /var/backups/couchdb/<date>/. Runs one backup now and test-restores it into scratch
# databases (restoretest-*), compares doc counts, then deletes the scratch databases.
# Undo: systemctl disable --now couchdb-backup.timer; rm /etc/systemd/system/couchdb-backup.{service,timer} \
#       /usr/local/libexec/couchfusion/couchdb-{backup,restore}.sh; systemctl daemon-reload   (dumps stay)
source "$(dirname "$0")/lib.sh"
require_root
command -v jq >/dev/null || die "jq missing (apt install jq)"
if getent group cfbackup >/dev/null; then install -d -m 0750 -o root -g cfbackup /var/backups/couchdb
else install -d -m 0700 -o root -g root /var/backups/couchdb; fi
install_file "$HERE/files/couchdb-backup.sh" /usr/local/libexec/couchfusion/couchdb-backup.sh 0755
install_file "$HERE/files/couchdb-restore.sh" /usr/local/libexec/couchfusion/couchdb-restore.sh 0755
install_file "$HERE/files/couchdb-backup.service" /etc/systemd/system/couchdb-backup.service
install_file "$HERE/files/couchdb-backup.timer" /etc/systemd/system/couchdb-backup.timer
systemctl daemon-reload
systemctl enable --now couchdb-backup.timer
log "backup now:"
systemctl start couchdb-backup.service
journalctl -u couchdb-backup.service -n 15 --no-pager -o cat | grep -E 'dumped|backup ' || true
day=$(ls -1 /var/backups/couchdb | grep '^20' | sort | tail -1)
dir=/var/backups/couchdb/$day
log "test restore of $dir into scratch databases:"
set -a; . "$CF_ETC/env/$CF_APP.prod.env"; set +a
cfg=$(mktemp); trap 'rm -f "$cfg"' EXIT
printf 'header = "Authorization: Basic %s"\n' "$COUCHDB_ADMIN_AUTH" > "$cfg"
fail=0
for db in $(jq -r '.databases | keys[]' "$dir/manifest.json"); do
  case "$db" in _*) continue ;; esac   # system databases: not restored into scratch copies
  want=$(jq -r --arg d "$db" '.databases[$d].docs' "$dir/manifest.json")
  file=$(jq -r --arg d "$db" '.databases[$d].file' "$dir/manifest.json")
  scratch="restoretest-$(printf %s "$db" | tr '_/' '--')"
  /usr/local/libexec/couchfusion/couchdb-restore.sh "$dir/$file" "$scratch" >/dev/null
  got=$(curl -s -K "$cfg" "http://127.0.0.1:5984/$scratch" | jq '.doc_count + .doc_del_count')
  # _all_docs lists live docs (including design docs); deleted ones are not in the dump.
  if [ "$got" -eq "$want" ]; then echo "  $db: $got/$want docs ok"; else echo "  $db: $got/$want docs MISMATCH"; fail=1; fi
  curl -s -K "$cfg" -X DELETE "http://127.0.0.1:5984/$scratch" >/dev/null
done
systemctl list-timers couchdb-backup.timer --no-pager | sed -n 1,2p
[ $fail -eq 0 ] && log "backups installed; restore test passed" || die "restore test found mismatches"
