#!/usr/bin/env bash
# Runs ON THE LAPTOP. One-time seed of the hetzner1 workspace from the laptop checkout.
# The VPS is the primary dev machine afterwards, so this refuses to overwrite an existing
# VPS copy unless --force is given (that would discard edits made on the VPS).
#
# Copies: apps/<app>, layers, cli-content — including .git (local history, no remote needed).
# Never copies: node_modules, .nuxt, .output, .env*, test-results, playwright caches, .DS_Store.
#
# Usage: ./sync-from-laptop.sh [--dry-run] [--force]
set -euo pipefail

HOST=${CF_HOST:-hetzner1}
APP=${CF_APP:-radustanciu}
SRC=${CF_SRC:-$HOME/Projects/nuxt-apps}
DEST=/srv/couchfusion/workspace

DRY=() ; FORCE=0
for a in "$@"; do
  case $a in
    --dry-run) DRY=(--dry-run) ;;
    --force) FORCE=1 ;;
    *) echo "unknown arg $a" >&2; exit 2 ;;
  esac
done

[ -d "$SRC/apps/$APP" ] || { echo "missing $SRC/apps/$APP" >&2; exit 1; }

if [ $FORCE -eq 0 ] && ssh "$HOST" "test -d $DEST/apps/$APP/app"; then
  echo "hetzner1 already has $DEST/apps/$APP — refusing to overwrite VPS edits (use --force)" >&2
  exit 1
fi

EXCLUDES=(
  --exclude=node_modules/ --exclude=.nuxt/ --exclude=.output/ --exclude=.data/
  --exclude='.env' --exclude='.env.*' --exclude=test-results/ --exclude=playwright-report/
  --exclude=.playwright-mcp/ --exclude=.DS_Store --exclude='*.log'
)

for part in "apps/$APP" layers cli-content; do
  echo "== $part"
  # Plain flags only: macOS ships openrsync (protocol 29), which lacks --info/--human-readable.
  rsync -a "${DRY[@]}" "${EXCLUDES[@]}" \
    --rsync-path="mkdir -p $DEST/$(dirname "$part") && rsync" \
    "$SRC/$part/" "$HOST:$DEST/$part/"
done
[ ${#DRY[@]} -eq 0 ] && ssh "$HOST" "du -sh $DEST/apps/$APP $DEST/layers $DEST/cli-content"

echo "done. Next on hetzner1 (as root): 03-workspace.sh (fix ownership), then 04-deps.sh"
