#!/usr/bin/env bash
# Step 03 — units, build script, polkit rule, tmpfiles, slice size, nft guard for prod ports.
# Nothing is started; existing units are not restarted (the nft table is reloaded in place).
# Undo: rm /etc/systemd/system/{cf-site@.service,cf-build@.service} \
#          /etc/systemd/system/couchfusion.slice.d/20-hydra1.conf /usr/local/libexec/couchfusion/cf-build.sh; \
#       restore phase2/files/{50-couchfusion.rules,couchfusion.tmpfiles.conf}; drop 4501-4509 from
#       /etc/couchfusion/nft/couchfusion.nft; systemctl daemon-reload; systemctl reload couchfusion-nft
source "$(dirname "$0")/lib.sh"
require_root
F=$HERE/files
id "$CF_SITE_USER" >/dev/null 2>&1 || die "run 01-site-user.sh first"

install_file "$F/cf-site@.service" /etc/systemd/system/cf-site@.service
install_file "$F/cf-build@.service" /etc/systemd/system/cf-build@.service
install_file "$F/cf-build.sh" /usr/local/libexec/couchfusion/cf-build.sh 0755
install_file "$F/couchfusion.slice.hydra1.conf" /etc/systemd/system/couchfusion.slice.d/20-hydra1.conf
install_file "$F/50-couchfusion.rules" /etc/polkit-1/rules.d/50-couchfusion.rules
install_file "$F/couchfusion.tmpfiles.conf" /etc/tmpfiles.d/couchfusion.conf
systemd-tmpfiles --create /etc/tmpfiles.d/couchfusion.conf
systemctl daemon-reload

# Sandboxed users (cfdev, cfagent) must not reach production ports.
NFT=$CF_ETC/nft/couchfusion.nft
if grep -q '4501-4509' "$NFT"; then
  log "nft guard already protects 4501-4509"
else
  cp -a "$NFT" "$NFT.bak-phase3"
  sed -i 's/elements = { 25, 557, 2019, 4369,/elements = { 25, 557, 2019, 4369, 4501-4509,/' "$NFT"
  grep -q '4501-4509' "$NFT" || die "could not add 4501-4509 to $NFT (format changed?)"
  nft -c -f "$NFT" || { cp -a "$NFT.bak-phase3" "$NFT"; die "nft syntax check failed; restored"; }
  systemctl reload couchfusion-nft.service
  log "nft guard now protects 4501-4509"
fi

log "polkit self-test:"
sudo -u cfcp systemctl show "cf-site@$CF_APP.service" -p LoadState >/dev/null && log "  cfcp can read cf-site@$CF_APP state"
if (cd / && sudo -u cfcp systemctl --no-ask-password kill -s 0 ssh.service 2>/dev/null); then die "cfcp may manage ssh.service — rule too broad"; else log "  cfcp cannot manage ssh.service (good)"; fi
systemctl show couchfusion.slice -p MemoryHigh -p MemoryMax
log "units installed; nothing started. Next: 04-release.sh deploy"
