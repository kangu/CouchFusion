#!/usr/bin/env bash
# Step 00 — read-only report. Changes nothing.
source "$(dirname "$0")/lib.sh"
require_root

log "host: $(hostname) / $(. /etc/os-release; echo "$PRETTY_NAME") / $(uname -m)"
log "memory:"; free -h
log "pressure:"; cat /proc/pressure/memory 2>/dev/null || true
log "disk:"; df -h / | tail -1

log "ports this setup wants (must be free):"
for p in "$CF_DEV_PORT" "$CF_DEV_SLOT" 9180 4501; do
  if ss -ltnH "( sport = :$p )" | grep -q .; then warn "port $p is in use"; else echo "  $p free"; fi
done

log "existing users/groups:"
for u in cfcp cfdev cfagent; do getent passwd "$u" >/dev/null && echo "  user $u exists" || echo "  user $u missing"; done
getent group cfsrc >/dev/null && echo "  group cfsrc exists" || echo "  group cfsrc missing"

log "nftables tables:"; nft list tables
log "tailscale serve:"; tailscale serve status 2>&1 | sed 's/^/  /'
log "caddy site file modes:"; ls -l /etc/caddy/sites/
log "toolchain:"; ls "$CF_TOOLCHAIN/bin" 2>/dev/null || echo "  not installed"
log "workspace:"; ls "$CF_WORKSPACE" 2>/dev/null || echo "  not created"

log "user namespaces (Codex sandbox): $(sysctl -n kernel.unprivileged_userns_clone 2>/dev/null) / max $(sysctl -n user.max_user_namespaces)"
log "public listeners on non-loopback addresses (review, not changed):"
ss -ltnpH | awk '$4 !~ /^(127\.|\[::1\]|100\.|\[fd7a)/ {print "  " $4, $6}'
