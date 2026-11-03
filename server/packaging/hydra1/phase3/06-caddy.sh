#!/usr/bin/env bash
# Step 06 — Caddy site for radustanciu (cut-over prep). Caddy serves nothing else on hydra1;
# the stock Caddyfile (":80 file_server") is backed up and replaced by an import of /etc/caddy/sites.
# Test before DNS:  curl -k --resolve radustanciu.ro:443:127.0.0.1 https://radustanciu.ro/
# Undo: cp /etc/caddy/Caddyfile.bak-phase3 /etc/caddy/Caddyfile; rm /etc/caddy/sites/radustanciu.caddy; systemctl reload caddy
source "$(dirname "$0")/lib.sh"
require_root
[ -f /etc/caddy/Caddyfile.bak-phase3 ] || cp -a /etc/caddy/Caddyfile /etc/caddy/Caddyfile.bak-phase3
install -d -m 0750 -o root -g caddy /etc/caddy/sites
install_file "$HERE/files/radustanciu.caddy" /etc/caddy/sites/radustanciu.caddy 0640 root:caddy
cp -a /etc/caddy/Caddyfile /etc/caddy/Caddyfile.prev
install_file "$HERE/files/Caddyfile" /etc/caddy/Caddyfile 0644 root:root
if ! caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/tmp/caddy-validate.log 2>&1; then
  cp -a /etc/caddy/Caddyfile.prev /etc/caddy/Caddyfile; tail -5 /tmp/caddy-validate.log; die "caddy validate failed; Caddyfile restored"
fi
systemctl reload caddy
sleep 2
log "local test (self-signed, so -k):"
for h in radustanciu.ro www.radustanciu.ro radustanciu.com www.radustanciu.com; do
  printf '  %-22s %s\n' "$h" "$(curl -sk -o /dev/null -w '%{http_code} %{redirect_url}' --resolve "$h:443:127.0.0.1" "https://$h/")"
done
printf '  %-22s %s\n' "http radustanciu.ro" "$(curl -s -o /dev/null -w '%{http_code} %{redirect_url}' --resolve radustanciu.ro:80:127.0.0.1 http://radustanciu.ro/)"
