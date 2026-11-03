#!/usr/bin/env bash
# Shared helpers for the hydra1 Phase 3 steps (production). Same conventions as phase1/lib.sh.
set -euo pipefail
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CF_ROOT=/opt/couchfusion
CF_TOOLCHAIN=$CF_ROOT/toolchain
CF_WORKSPACE=/srv/couchfusion/workspace
CF_ETC=/etc/couchfusion
CF_APP=${CF_APP:-radustanciu}
CF_SITE_USER=cf-site-$CF_APP
CF_PROD_PORT=${CF_PROD_PORT:-4501}
CF_PREVIEW_PORT=${CF_PREVIEW_PORT:-9189}
CF_WWW=/var/www/$CF_APP
CF_BUILD=/var/cache/cf-build/$CF_APP
CF_KEEP_RELEASES=5
CF_KEEP_SNAPSHOTS=2
log() { printf '\033[1;34m[phase3]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[phase3] warning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[phase3] error:\033[0m %s\n' "$*" >&2; exit 1; }
require_root() { [ "$(id -u)" -eq 0 ] || die "run as root"; }
# install_file <src> <dest> [mode] [owner]: copies only when content differs.
install_file() {
  local src=$1 dest=$2 mode=${3:-0644} owner=${4:-root:root}
  if [ -f "$dest" ] && cmp -s "$src" "$dest"; then log "unchanged $dest"; else install -D -m "$mode" -o "${owner%%:*}" -g "${owner##*:}" "$src" "$dest"; log "wrote $dest"; fi
  chmod "$mode" "$dest"; chown "$owner" "$dest"
}
# Run a command as cfcp from / (a /root cwd breaks sudo -u).
as_cfcp() { (cd / && sudo -u cfcp "$@"); }
mem_available_mib() { awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo; }
