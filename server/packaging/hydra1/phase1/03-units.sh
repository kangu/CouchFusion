#!/usr/bin/env bash
# Step 03 — config, units, cf-dev drop-in, polkit rule, tmpfiles. Nothing is started.
# Undo: rm /etc/systemd/system/{couchfusion.service,cf-agent@.service} \
#          /etc/systemd/system/cf-dev@.service.d/10-couchfusion-runtime.conf \
#          /etc/polkit-1/rules.d/50-couchfusion.rules /etc/tmpfiles.d/couchfusion.conf; systemctl daemon-reload
source "$(dirname "$0")/lib.sh"
require_root
F=$HERE/files
if [ -f $CF_ETC/server.toml ]; then log "keeping existing $CF_ETC/server.toml"; else install_file "$HERE/server.toml" $CF_ETC/server.toml 0640 root:cfcp; fi
install_file "$F/couchfusion.service" /etc/systemd/system/couchfusion.service
install_file "$F/cf-agent@.service" /etc/systemd/system/cf-agent@.service
install_file "$F/cf-dev@.service.d/10-couchfusion-runtime.conf" /etc/systemd/system/cf-dev@.service.d/10-couchfusion-runtime.conf
install_file "$F/50-couchfusion.rules" /etc/polkit-1/rules.d/50-couchfusion.rules 0644
install_file "$F/couchfusion.tmpfiles.conf" /etc/tmpfiles.d/couchfusion.conf
systemd-tmpfiles --create /etc/tmpfiles.d/couchfusion.conf
# Codex runs commands in a login shell; Debian's /etc/profile resets PATH for non-root users,
# so put the toolchain back via the agent's own ~/.profile (read last by login shells).
install_file "$F/cfagent.profile" /var/lib/cfagent/.profile 0644
systemctl daemon-reload
systemd-analyze verify /etc/systemd/system/couchfusion.service /etc/systemd/system/cf-agent@radustanciu.service 2>&1 | grep -v '^$' || true
log "polkit self-test (expect: cf-dev allowed, ssh refused):"
sudo -u cfcp systemctl show cf-dev@radustanciu.service -p ActiveState >/dev/null && log "  cfcp can read unit state"
# `kill -s 0` is a no-op signal, so a misconfigured rule cannot disturb ssh during the test.
if sudo -u cfcp systemctl --no-ask-password kill -s 0 ssh.service 2>/dev/null; then die "cfcp may manage ssh.service — polkit rule too broad"; else log "  cfcp cannot manage ssh.service (good)"; fi
