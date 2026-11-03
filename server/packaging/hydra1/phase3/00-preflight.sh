#!/usr/bin/env bash
# Step 00 — checks only; changes nothing.
source "$(dirname "$0")/lib.sh"
require_root
ok=1
check() { if eval "$2"; then log "ok: $1"; else warn "FAIL: $1"; ok=0; fi; }
check "Phase 1/2 control plane active" "systemctl is-active --quiet couchfusion.service"
check "dev env file exists ($CF_ETC/env/$CF_APP.dev.env)" "[ -f $CF_ETC/env/$CF_APP.dev.env ]"
check "toolchain node + bun" "[ -x $CF_TOOLCHAIN/bin/node ] && [ -x $CF_TOOLCHAIN/bin/bun ]"
check "cfcp is in cfsrc" "id -nG cfcp | tr ' ' '\n' | grep -qx cfsrc"
check "prod port $CF_PROD_PORT free" "! ss -ltnH 'sport = :$CF_PROD_PORT' | grep -q ."
check "preview port $CF_PREVIEW_PORT free" "! ss -ltnH 'sport = :$CF_PREVIEW_PORT' | grep -q ."
check "MemAvailable >= 2048 MiB (dev + build)" "[ $(mem_available_mib) -ge 2048 ]"
check "disk free >= 5 GiB on /var" "[ $(df --output=avail -BG /var | tail -1 | tr -dc 0-9) -ge 5 ]"
check "caddy installed" "command -v caddy >/dev/null"
log "MemAvailable $(mem_available_mib) MiB; /var free $(df --output=avail -h /var | tail -1 | tr -d ' ')"
[ $ok -eq 1 ] && log "preflight passed" || die "preflight failed"
