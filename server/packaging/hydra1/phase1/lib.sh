#!/usr/bin/env bash
# Shared helpers for the hydra1 Phase 1 steps (same conventions as phase0/lib.sh).
set -euo pipefail
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CF_ROOT=/opt/couchfusion
CF_WORKSPACE=/srv/couchfusion/workspace
CF_ETC=/etc/couchfusion
CF_REPOS=(apps/radustanciu layers cli-content)
CF_PORTS=(9180 9181)
log() { printf '\033[1;34m[phase1]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[phase1] warning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[phase1] error:\033[0m %s\n' "$*" >&2; exit 1; }
require_root() { [ "$(id -u)" -eq 0 ] || die "run as root"; }
# install_file <src> <dest> [mode] [owner]: copies only when content differs.
install_file() {
  local src=$1 dest=$2 mode=${3:-0644} owner=${4:-root:root}
  if [ -f "$dest" ] && cmp -s "$src" "$dest"; then log "unchanged $dest"; else install -D -m "$mode" -o "${owner%%:*}" -g "${owner##*:}" "$src" "$dest"; log "wrote $dest"; fi
  chmod "$mode" "$dest"; chown "$owner" "$dest"
}
