#!/usr/bin/env bash
# Step 11 — spike S3: can `tailscale serve` target a unix socket, and does it overwrite
# client-sent Tailscale-User-* headers? (Decides how the control plane trusts identity.)
# Runs a tiny header-echo server on a root-only socket for ~2 minutes on tailnet port 9199,
# then removes the mapping. Nothing else is changed.
# From the laptop, while this runs:
#   curl -s https://hetzner1.tailb7471b.ts.net:9199/
#   curl -s -H 'Tailscale-User-Login: forged@example.com' https://hetzner1.tailb7471b.ts.net:9199/
source "$(dirname "$0")/lib.sh"
require_root

SOCK=/run/cf-spike-s3.sock
PORT=9199
rm -f "$SOCK"

python3 -I - "$SOCK" <<'PY' &
import http.server, socketserver, sys, os, json
path = sys.argv[1]
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps({k: v for k, v in self.headers.items() if k.lower().startswith(("tailscale", "host", "x-forwarded"))}, indent=1).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers(); self.wfile.write(body)
    def address_string(self): return "unix"
class S(socketserver.UnixStreamServer):
    def get_request(self):
        req, _ = super().get_request(); return req, ("unix", 0)
s = S(path, H); os.chmod(path, 0o600)
s.timeout = 1
import time; end = time.time() + 120
while time.time() < end: s.handle_request()
PY
server=$!
trap 'tailscale serve --https=$PORT off >/dev/null 2>&1 || true; kill $server 2>/dev/null || true; rm -f "$SOCK"' EXIT

sleep 1
if tailscale serve --bg --https=$PORT "unix:$SOCK"; then
  log "S3a: unix target accepted. Run the two curl commands from the laptop within 2 minutes."
  log "S3b passes if the forged request shows your real login, not forged@example.com."
else
  warn "S3a: unix target NOT supported — use the TCP + nft-uid fallback (spec §4.2)"
fi
wait $server || true
