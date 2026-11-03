#!/usr/bin/env bash
# Step 09 — deploy and rollback from the control UI (needs a couchfusion-server release with
# `prod-releases`; install it first with phase1/02-release.sh). Adds:
#   - couchfusion.service drop-in: write access to /var/cache/cf-build and /var/www/radustanciu
#   - server.toml: [sites.prod] for radustanciu; cf-site@/cf-build@ in [services] units
# validates the config with the installed binary (restores the old file on failure), then
# restarts the control plane (dev units keep running and are re-adopted).
# Undo: rm /etc/systemd/system/couchfusion.service.d/30-phase3.conf; cp /etc/couchfusion/server.toml.bak-phase3 \
#       /etc/couchfusion/server.toml; systemctl daemon-reload; systemctl restart couchfusion
source "$(dirname "$0")/lib.sh"
require_root
TOML=$CF_ETC/server.toml
BIN=$CF_ROOT/current/couchfusion-server
"$BIN" --version
install_file "$HERE/files/couchfusion.service.phase3.conf" /etc/systemd/system/couchfusion.service.d/30-phase3.conf

[ -f "$TOML.bak-phase3" ] || cp -a "$TOML" "$TOML.bak-phase3"
cp -a "$TOML" "$TOML.prev"
if grep -q '^\[sites\.prod\]' "$TOML"; then
  log "server.toml already has [sites.prod]"
else
  awk -v app="$CF_APP" -v port="$CF_PROD_PORT" -v preview="$CF_PREVIEW_PORT" -v public="${CF_PUBLIC_URL:-https://radustanciu.ro}" '
    { print }
    $0 == "agent_unit = \"cf-agent@" app ".service\"" {
      print ""
      print "# Phase 3: production releases (deploy/rollback from the UI). Same layout as phase3/04-release.sh."
      print "[sites.prod]"
      print "unit = \"cf-site@" app ".service\""
      print "build_unit = \"cf-build@" app ".service\""
      print "port = " port
      print "www_dir = \"/var/www/" app "\""
      print "build_dir = \"/var/cache/cf-build/" app "\""
      print "preview_port = " preview
      print "public_url = \"" public "\""
    }' "$TOML.prev" > "$TOML.new"
  grep -q '^\[sites\.prod\]' "$TOML.new" || { rm -f "$TOML.new"; die "could not find agent_unit for $CF_APP in $TOML"; }
  cat "$TOML.new" > "$TOML"; rm -f "$TOML.new"
  log "added [sites.prod]"
fi
if ! grep -q "^units = .*\"cf-site@$CF_APP.service\"" "$TOML"; then
  sed -i "s/\"cf-dev@$CF_APP.service\",/\"cf-dev@$CF_APP.service\", \"cf-site@$CF_APP.service\", \"cf-build@$CF_APP.service\",/" "$TOML"
  grep -q "^units = .*\"cf-site@$CF_APP.service\"" "$TOML" && log "added cf-site@/cf-build@ to [services] units" || warn "[services] units not updated (format changed?)"
fi
if ! (cd / && sudo -u cfcp "$BIN" doctor --config "$TOML" >/tmp/cf-doctor.log 2>&1); then
  cp -a "$TOML.prev" "$TOML"; sed -n 1,30p /tmp/cf-doctor.log; die "doctor failed with the new config; server.toml restored"
fi
log "doctor ok"
systemctl daemon-reload
systemctl restart couchfusion
sleep 2
systemctl is-active couchfusion
systemctl show couchfusion -p ReadWritePaths -p InaccessiblePaths | sed 's/^/  /'
