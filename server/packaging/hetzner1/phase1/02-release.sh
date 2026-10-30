#!/usr/bin/env bash
# Step 02 — install a release tarball built by server/release.sh.
#   ./02-release.sh /root/couchfusion-server-<rel>.tar.zst
# Layout: /opt/couchfusion/releases/<rel>/{couchfusion-server,web/} and current → that release.
# Undo: point /opt/couchfusion/current back at the previous release (kept), or rm it.
source "$(dirname "$0")/lib.sh"
require_root
TARBALL=${1:?usage: $0 <couchfusion-server-REL.tar.zst>}
[ -f "$TARBALL.sha256" ] && (cd "$(dirname "$TARBALL")" && sha256sum -c "$(basename "$TARBALL").sha256") || warn "no checksum file next to $TARBALL"
REL=$(basename "$TARBALL" .tar.zst); REL=${REL#couchfusion-server-}
DEST=$CF_ROOT/releases/$REL
if [ -d "$DEST" ]; then log "release $REL already unpacked"; else
  install -d -m 0755 "$CF_ROOT/releases"
  tar --zstd -xf "$TARBALL" -C "$CF_ROOT/releases"
  mv "$CF_ROOT/releases/couchfusion-$REL" "$DEST"
  chown -R root:root "$DEST"; chmod -R go-w "$DEST"
  log "unpacked $DEST"
fi
"$DEST/couchfusion-server" --version
[ -f "$DEST/web/index.html" ] || warn "release has no web/ (SPA); the control UI will show 'not installed'"
ln -sfn "$DEST" "$CF_ROOT/current.new" && mv -T "$CF_ROOT/current.new" "$CF_ROOT/current"
log "current → $(readlink $CF_ROOT/current)"
ls -1dt $CF_ROOT/releases/* | tail -n +4 | while read -r old; do log "pruning ${old:?}"; rm -rf "${old:?}"; done
