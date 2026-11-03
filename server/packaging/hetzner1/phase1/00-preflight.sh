#!/usr/bin/env bash
# Step 00 — read-only checks that Phase 0 is in place. Changes nothing.
source "$(dirname "$0")/lib.sh"
require_root
for u in cfcp cfdev cfagent; do getent passwd $u >/dev/null && log "user $u ok" || die "user $u missing (phase0/01-users.sh)"; done
systemctl cat cf-dev@.service >/dev/null 2>&1 && log "cf-dev@ template ok" || die "cf-dev@.service missing (phase0/05-units.sh)"
systemctl is-active -q couchfusion-nft.service && log "nft guard active" || die "couchfusion-nft.service not active (phase0/06-nft.sh)"
[ -x $CF_ROOT/toolchain/bin/codex ] && log "toolchain ok" || die "toolchain missing (phase0/02-toolchain.sh)"
[ -f /var/lib/cfagent/.codex/auth.json ] && log "codex logged in as cfagent" || warn "codex not logged in (phase0/09-codex.sh login)"
for p in "${CF_PORTS[@]}"; do
  if ss -ltnH "( sport = :$p )" | grep -q .; then warn "127.0.0.1:$p is in use"; else log "port $p free"; fi
done
log "registry file modes (cfcp must be able to read them):"
ls -l $CF_WORKSPACE/apps/radustanciu/app/content-builder/
if ! sudo -u cfcp -g cfsrc test -r $CF_WORKSPACE/apps/radustanciu/app/content-builder/component-definitions.json 2>/dev/null; then
  warn "cfcp cannot read component-definitions.json (generator writes 0600; see layers/content/scripts/registry-output.mjs). Delete/generate need it."
fi
