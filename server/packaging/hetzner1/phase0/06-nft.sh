#!/usr/bin/env bash
# Step 06 — loopback/tailnet egress guard for the sandboxed users.
# Adds ONE separate nftables table (inet couchfusion). It only matches packets sent by
# cfdev/cfagent; every other process on the box is untouched. Existing iptables-nft tables
# are not modified, and the nftables.service (disabled today) stays disabled.
#
#   cfdev   -> blocked: Caddy admin, Webmin, Hermes, DevTools, other apps' ports, control-plane ports
#              allowed: CouchDB :5984 (D1: dev shares prod data), internet egress, its own port
#   cfagent -> blocked: all of the above AND CouchDB; Codex runs with network off anyway
#   both    -> blocked from the box's own tailnet addresses (tailscale-served Webmin, CouchDB, Hermes)
#
# Undo: systemctl disable --now couchfusion-nft.service   (deletes the table)
source "$(dirname "$0")/lib.sh"
require_root

getent passwd cfdev >/dev/null && getent passwd cfagent >/dev/null || die "run 01-users.sh first"

install_file "$CF_ETC/nft/couchfusion.nft" "table inet couchfusion {
  set dev_uids   { type uid; elements = { cfdev } }
  set agent_uids { type uid; elements = { cfagent } }

  # Loopback services that sandboxed code must never reach.
  set protected_ports {
    type inet_service; flags interval
    elements = { 556, 2004, 2019, 4310, 4311, 4369, 8087, 8096, 8443, 9119, 9180-9199, 9223, 9224, 10000, 33287 }
  }

  chain output {
    type filter hook output priority 0; policy accept;

    # DNS: /etc/resolv.conf points at Tailscale MagicDNS, which sits inside the tailnet range.
    ip  daddr 100.100.100.100    udp dport 53 accept
    ip  daddr 100.100.100.100    tcp dport 53 accept
    ip6 daddr fd7a:115c:a1e0::53 udp dport 53 accept
    ip6 daddr fd7a:115c:a1e0::53 tcp dport 53 accept

    # tailnet addresses of this box (and the rest of the tailnet)
    meta skuid @dev_uids   ip  daddr 100.64.0.0/10      reject
    meta skuid @agent_uids ip  daddr 100.64.0.0/10      reject
    meta skuid @dev_uids   ip6 daddr fd7a:115c:a1e0::/48 reject
    meta skuid @agent_uids ip6 daddr fd7a:115c:a1e0::/48 reject

    # loopback
    meta skuid @dev_uids   ip  daddr 127.0.0.0/8 tcp dport @protected_ports reject with tcp reset
    meta skuid @dev_uids   ip6 daddr ::1         tcp dport @protected_ports reject with tcp reset
    meta skuid @agent_uids ip  daddr 127.0.0.0/8 tcp dport @protected_ports reject with tcp reset
    meta skuid @agent_uids ip6 daddr ::1         tcp dport @protected_ports reject with tcp reset
    meta skuid @agent_uids ip  daddr 127.0.0.0/8 tcp dport 5984 reject with tcp reset
    meta skuid @agent_uids ip6 daddr ::1         tcp dport 5984 reject with tcp reset
  }
}" 0644

install_file /etc/systemd/system/couchfusion-nft.service "[Unit]
Description=CouchFusion egress guard for sandboxed users (nft table inet couchfusion)
After=network-pre.target
Before=network.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=-/usr/sbin/nft delete table inet couchfusion
ExecStart=/usr/sbin/nft -f $CF_ETC/nft/couchfusion.nft
ExecReload=/bin/sh -c '/usr/sbin/nft delete table inet couchfusion; /usr/sbin/nft -f $CF_ETC/nft/couchfusion.nft'
ExecStop=/usr/sbin/nft delete table inet couchfusion

[Install]
WantedBy=multi-user.target"

nft -c -f "$CF_ETC/nft/couchfusion.nft" || die "nft syntax check failed; nothing loaded"
systemctl daemon-reload
systemctl enable couchfusion-nft.service >/dev/null
systemctl restart couchfusion-nft.service
log "table loaded:"; nft list table inet couchfusion | sed -n 1,5p   # sed reads all input (head would SIGPIPE under pipefail)

log "self-test (expect BLOCKED for the protected ones):"
probe() {
  local user=$1 port=$2
  if sudo -u "$user" timeout 3 bash -c "exec 3<>/dev/tcp/127.0.0.1/$port" 2>/dev/null; then
    echo "  $user -> :$port open"
  else
    echo "  $user -> :$port BLOCKED/closed"
  fi
}
probe cfdev 2019; probe cfdev 9119; probe cfdev 10000; probe cfdev 5984
probe cfagent 2019; probe cfagent 5984
probe radu 2019
log "expected: cfdev blocked on 2019/9119/10000 but open on 5984; cfagent blocked on both; radu unchanged (open)"
log "DNS check (must resolve for both):"
for u in cfdev cfagent; do
  if sudo -u "$u" getent hosts registry.npmjs.org >/dev/null; then echo "  $u dns ok"; else warn "$u dns FAILED — undo with: systemctl disable --now couchfusion-nft.service"; fi
done
