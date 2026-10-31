#!/bin/bash
# Pull hydra1's nightly CouchDB backups to this Mac (off-box copy). Run daily by launchd
# (ro.kangu.couchfusion.hydra1-backup); safe to run by hand. hydra1 only lets this key stream
# /var/backups/couchdb as a tar (phase3/08-offbox-pull.sh). Keeps 60 days here.
set -euo pipefail
umask 077
HOST=${HYDRA1_HOST:-100.83.137.109}
KEY=${HYDRA1_BACKUP_KEY:-$HOME/.ssh/id_hydra1_backup}
DEST=${HYDRA1_BACKUP_DIR:-$HOME/Backups/hydra1-couchdb}
KEEP_DAYS=60
notify() { /usr/bin/osascript -e "display notification \"$1\" with title \"hydra1 CouchDB backup\"" >/dev/null 2>&1 || true; }
fail() { echo "$(date '+%F %T') FAILED: $1"; notify "Pull failed: $1"; exit 1; }
mkdir -p "$DEST"
tmp=$(mktemp -d "$DEST/.pull.XXXXXX"); trap 'rm -rf "$tmp"' EXIT
/usr/bin/ssh -i "$KEY" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=20 \
  "cfbackup@$HOST" > "$tmp/backups.tar" 2> "$tmp/ssh.err" || fail "ssh: $(tail -1 "$tmp/ssh.err")"
/usr/bin/tar -tf "$tmp/backups.tar" | grep -q 'manifest.json$' || fail "no manifest in the stream"
/usr/bin/tar -xf "$tmp/backups.tar" -C "$DEST"
latest=$(ls -1 "$DEST" | grep -E '^20[0-9]{2}-[0-9]{2}-[0-9]{2}$' | sort | tail -1)
[ -n "$latest" ] || fail "no dated backup folder"
age_days=$(( ( $(date -u +%s) - $(date -j -u -f %Y-%m-%d "$latest" +%s) ) / 86400 ))
[ "$age_days" -le 2 ] || fail "newest backup is $latest ($age_days days old) — check couchdb-backup.timer on hydra1"
find "$DEST" -mindepth 1 -maxdepth 1 -type d -name '20??-??-??' -mtime +"$KEEP_DAYS" -exec rm -rf {} +
echo "$(date '+%F %T') ok: newest $latest, $(ls -1 "$DEST" | grep -c '^20') days here, $(du -sh "$DEST" | cut -f1)"
