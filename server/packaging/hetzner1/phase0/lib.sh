#!/usr/bin/env bash
# Shared helpers for the hetzner1 Phase 0 setup steps.
# Every step is idempotent: re-running it changes nothing that is already in place.
set -euo pipefail

CF_ROOT=/opt/couchfusion
CF_TOOLCHAIN=$CF_ROOT/toolchain
CF_WORKSPACE=/srv/couchfusion/workspace
CF_ETC=/etc/couchfusion
CF_APP=${CF_APP:-radustanciu}
CF_DEV_PORT=${CF_DEV_PORT:-4401}
CF_DEV_SLOT=${CF_DEV_SLOT:-9181}

NODE_VERSION=22.23.3
BUN_VERSION=1.4.2
CODEX_VERSION=0.144.4

log() { printf '\033[1;34m[phase0]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[phase0] warning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[phase0] error:\033[0m %s\n' "$*" >&2; exit 1; }

require_root() {
  [ "$(id -u)" -eq 0 ] || die "run as root"
}

# Writes $2 to path $1 only when the content differs, then sets mode/owner.
install_file() {
  local path=$1 content=$2 mode=${3:-0644} owner=${4:-root:root}
  local tmp
  tmp=$(mktemp)
  printf '%s\n' "$content" >"$tmp"
  if [ -f "$path" ] && cmp -s "$tmp" "$path"; then
    rm -f "$tmp"
    log "unchanged $path"
  else
    install -D -m "$mode" -o "${owner%%:*}" -g "${owner##*:}" "$tmp" "$path"
    rm -f "$tmp"
    log "wrote $path"
  fi
  chmod "$mode" "$path"
  chown "$owner" "$path"
}
