#!/usr/bin/env bash
# Phase 2 step 01 — registry rebuild unit, polkit rule incl. cf-registry@, tmpfiles (dev dir
# readable by cfdev for the agent marker). Idempotent. Nothing is started or restarted here.
#
#   --self-test   also run `cf-registry@radustanciu` once as cfcp (rewrites the registry files
#                 with the same content; proves polkit + sandbox work end to end)
#
# Undo: rm /etc/systemd/system/cf-registry@.service; reinstall the Phase 1 rule/tmpfiles from a
#       Phase 1 checkout; systemctl daemon-reload
source "$(dirname "$0")/../phase1/lib.sh"
require_root
cd /
P2=$(cd "$(dirname "$0")" && pwd)/files
SELF_TEST=0; [ "${1:-}" = "--self-test" ] && SELF_TEST=1

install_file "$P2/cf-registry@.service" /etc/systemd/system/cf-registry@.service
install_file "$P2/50-couchfusion.rules" /etc/polkit-1/rules.d/50-couchfusion.rules 0644
install_file "$P2/couchfusion.tmpfiles.conf" /etc/tmpfiles.d/couchfusion.conf
systemd-tmpfiles --create /etc/tmpfiles.d/couchfusion.conf
log "/run/couchfusion/dev: $(stat -c '%A %U:%G' /run/couchfusion/dev)"
systemctl daemon-reload
systemd-analyze verify /etc/systemd/system/cf-registry@radustanciu.service 2>&1 | grep -v '^$' || true

# The dev server must be able to see the marker (stat needs search permission on the dir).
if sudo -u cfdev test -x /run/couchfusion/dev; then log "cfdev can reach /run/couchfusion/dev (good)"; else die "cfdev cannot traverse /run/couchfusion/dev"; fi
if sudo -u cfdev test -r /run/couchfusion/dev/radustanciu.env 2>/dev/null; then die "cfdev can read the dev env file — expected 0600 cfcp"; else log "dev env file stays private to cfcp (good)"; fi

log "polkit self-test:"
if sudo -u cfcp systemctl --no-ask-password kill -s 0 ssh.service 2>/dev/null; then die "cfcp may manage ssh.service — polkit rule too broad"; else log "  cfcp cannot manage ssh.service (good)"; fi

if [ $SELF_TEST = 1 ]; then
  log "running cf-registry@radustanciu as cfcp…"
  if sudo -u cfcp systemctl --no-ask-password start cf-registry@radustanciu.service; then
    log "  ok: $(journalctl -u cf-registry@radustanciu.service -n 1 -o cat --no-pager)"
  else
    journalctl -u cf-registry@radustanciu.service -n 20 -o cat --no-pager || true
    die "cf-registry@radustanciu failed"
  fi
fi

if grep -q '^\[services\]' "$CF_ETC/server.toml"; then
  log "server.toml has a [services] section"
else
  warn "server.toml has no [services] section; the built-in default list applies. To customise, add:"
  sed -n '/^\[services\]/,/^units/p' "$(dirname "$0")/../phase1/server.toml" >&2
fi
log "next: install the new release (../phase1/02-release.sh <tarball>), systemctl restart couchfusion, then restart dev from the UI (or 05-start.sh) so the dev env gets COUCHFUSION_AGENT_MARKER"
