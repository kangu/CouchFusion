#!/usr/bin/env bash
# Step 08 — let the laptop pull the CouchDB backups (off-box copy), read-only.
# Usage: 08-offbox-pull.sh "<ssh public key of the laptop's pull job>"
# Adds system user cfbackup whose only SSH key is forced to stream /var/backups/couchdb as a tar
# (no shell, no forwarding, tailnet source addresses only). Dumps become group-readable by cfbackup.
# Undo: userdel -r cfbackup; chgrp -R root /var/backups/couchdb; chmod 0700 /var/backups/couchdb
source "$(dirname "$0")/lib.sh"
require_root
KEY=${1:?pass the laptop pull key (one line, ssh-ed25519 ...)}
case "$KEY" in ssh-ed25519\ *) ;; *) die "expected an ssh-ed25519 public key" ;; esac
[ "$(printf %s "$KEY" | wc -l)" -eq 0 ] || die "key must be one line"

if ! id cfbackup >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/cfbackup --no-create-home --shell /bin/sh cfbackup
  log "created cfbackup"
fi
usermod -p '*' cfbackup   # no password; key login only
install -d -m 0755 -o root -g root /var/lib/cfbackup /var/lib/cfbackup/.ssh
printf 'command="/usr/bin/tar -C /var/backups/couchdb --exclude=./.*.partial -cf - .",restrict,from="100.64.0.0/10,fd7a:115c:a1e0::/48" %s\n' "$KEY" > /var/lib/cfbackup/.ssh/authorized_keys.new
chmod 0644 /var/lib/cfbackup/.ssh/authorized_keys.new
mv /var/lib/cfbackup/.ssh/authorized_keys.new /var/lib/cfbackup/.ssh/authorized_keys
log "authorized_keys: forced tar of /var/backups/couchdb, tailnet only"

install -d -m 0750 -o root -g cfbackup /var/backups/couchdb
find /var/backups/couchdb -mindepth 1 -type d -exec chgrp cfbackup {} + -exec chmod 0750 {} +
find /var/backups/couchdb -type f -exec chgrp cfbackup {} + -exec chmod 0640 {} +
install_file "$HERE/files/couchdb-backup.sh" /usr/local/libexec/couchfusion/couchdb-backup.sh 0755
log "self-test: cfbackup can read the dumps, not other files"
sudo -u cfbackup tar -C /var/backups/couchdb -cf - . | tar -tf - | grep -c manifest.json | sed 's/^/  manifests readable: /'
if sudo -u cfbackup cat /etc/couchfusion/env/radustanciu.prod.env >/dev/null 2>&1; then die "cfbackup can read the prod env"; else log "  cfbackup cannot read the prod env (good)"; fi
