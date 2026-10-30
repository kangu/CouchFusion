#!/usr/bin/env bash
# Local smoke test of couchfusion-server on macOS/Linux (process_group runner, fake dev server,
# fake codex). Usage: ./run-local.sh [workdir]   (default: a fresh mktemp dir)
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
SERVER_DIR=$(cd "$HERE/../.." && pwd)
REPO=$(cd "$SERVER_DIR/../.." && pwd)            # nuxt-apps
WORK=${1:-$(mktemp -d)}
: "${WORK:?}"
BIN=$SERVER_DIR/target/debug/couchfusion-server
CP=19180; SLOT=19181; DEV=14401
HOSTNAME_TS=hetzner1.tailb7471b.ts.net
LOGIN=smoke@example.com
pass=0; fail=0
ok() { echo "  ok   $*"; pass=$((pass+1)); }
bad() { echo "  FAIL $*"; fail=$((fail+1)); }
expect() { local what=$1 want=$2 got=$3; if [ "$got" = "$want" ]; then ok "$what ($got)"; else bad "$what: want $want, got $got"; fi; }

(cd "$SERVER_DIR" && cargo build -q)
rm -rf "${WORK:?}/ws" "${WORK:?}/state" "${WORK:?}/run" "${WORK:?}/spa"
mkdir -p "$WORK/ws/apps/demo" "$WORK/state/agent-out" "$WORK/run/agent" "$WORK/run/dev" "$WORK/spa"
APP=$WORK/ws/apps/demo
mkdir -p "$APP/app/components/content" "$APP/app/content-builder" "$APP/app/pages"
cp "$REPO/apps/radustanciu/app/components/content/"*.vue "$APP/app/components/content/"
echo '{"name":"demo","scripts":{"dev":"nuxt dev","registry:build":"x"}}' > "$APP/package.json"
echo '<!doctype html><html><head><script>window.x=1</script></head><body>spa</body></html>' > "$WORK/spa/index.html"
git -C "$APP" init -q -b main && git -C "$APP" add -A && git -C "$APP" -c user.name=t -c user.email=t@t commit -qm init

cat > "$WORK/server.toml" <<TOML
[server]
state_dir = "$WORK/state"
run_dir = "$WORK/run"
spa_dir = "$WORK/spa"
public_host = "$HOSTNAME_TS"
control_port = $CP
runner = "process_group"
dev_ready_timeout_secs = 20
registry_timeout_secs = 8
[auth]
allowed_logins = ["$LOGIN"]
[workspace]
id = "hetzner1"
name = "hetzner1"
root = "$WORK/ws"
repos = ["apps/demo"]
[toolchain]
git = "/usr/bin/git"
bun = "/usr/bin/false"
[[sites]]
id = "demo"
slot_port = $SLOT
dev_port = $DEV
dev_unit = "cf-dev@demo.service"
agent_unit = "cf-agent@demo.service"
[process_group."cf-dev@demo.service"]
argv = ["python3", "$HERE/fake-dev.py"]
cwd = "$APP"
env_files = ["$WORK/run/dev/demo.env"]
[process_group."cf-agent@demo.service"]
argv = ["$BIN", "agent-runner", "--app", "demo", "--spec-dir", "$WORK/run/agent", "--workspace", "$WORK/ws", "--codex", "$HERE/fake-codex.sh"]
cwd = "$WORK"
stdout_append = "$WORK/state/agent-out/demo.jsonl"
TOML

"$BIN" serve --config "$WORK/server.toml" > "$WORK/server.log" 2>&1 &
SPID=$!
trap 'kill $SPID 2>/dev/null; pkill -f "$HERE/fake-dev.py" 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do curl -s -o /dev/null "http://127.0.0.1:$CP/" && break; sleep 0.1; done

cd "$WORK/run"   # sockets are addressed relatively (macOS sun_path limit)
SOCK=ts-$CP.sock
TS=(--unix-socket "$SOCK" -H "X-Forwarded-Host: $HOSTNAME_TS:$CP" -H "Tailscale-User-Login: $LOGIN")
U=http://localhost/api/v1
J=(-H 'Content-Type: application/json')
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
field() { jq -r ".data.$1 // empty"; }

echo "== auth and guards"
expect "tcp without cookie → 401" 401 "$(code http://127.0.0.1:$CP/api/v1/bootstrap)"
expect "tailnet wrong login → 401" 401 "$(code --unix-socket "$SOCK" -H "X-Forwarded-Host: $HOSTNAME_TS:$CP" -H 'Tailscale-User-Login: evil@x' $U/bootstrap)"
expect "tailnet bad host → 403" 403 "$(code --unix-socket "$SOCK" -H "X-Forwarded-Host: evil.example:$CP" -H "Tailscale-User-Login: $LOGIN" $U/bootstrap)"
expect "tailnet bootstrap → 200" 200 "$(code "${TS[@]}" $U/bootstrap)"
curl -s "${TS[@]}" $U/bootstrap | jq -e --arg o "https://$HOSTNAME_TS:$SLOT" '.data.devOrigins == [$o]' >/dev/null && ok "bootstrap devOrigins per caller (tailnet)" || bad "bootstrap devOrigins"
LINK=$("$BIN" login-url --config "$WORK/server.toml" 2>/dev/null | sed -E "s#http://localhost:[0-9]+#http://127.0.0.1:$CP#")
JAR=$WORK/jar; rm -f "$JAR"
expect "login-url exchange → 303" 303 "$(code -c "$JAR" "$LINK")"
expect "login-url reuse → 401" 401 "$(code "$LINK")"
expect "tcp with cookie → 200" 200 "$(code -b "$JAR" http://127.0.0.1:$CP/api/v1/workspaces)"
curl -s -b "$JAR" http://127.0.0.1:$CP/api/v1/bootstrap | grep -q "\"http://127.0.0.1:$SLOT\"" && ok "bootstrap devOrigins per caller (tunnel)" || bad "tunnel devOrigins"
expect "mutation same-site → 403" 403 "$(code "${TS[@]}" "${J[@]}" -H 'Sec-Fetch-Site: same-site' -X POST -d '{}' $U/workspaces/hetzner1/sites/demo/dev/start)"
expect "mutation wrong origin → 403" 403 "$(code "${TS[@]}" "${J[@]}" -H "Origin: https://$HOSTNAME_TS:$SLOT" -X POST -d '{}' $U/workspaces/hetzner1/sites/demo/dev/start)"
expect "mutation without json → 415" 415 "$(code "${TS[@]}" -X POST -d '{}' $U/workspaces/hetzner1/sites/demo/dev/start)"
expect "unknown api route → 404" 404 "$(code "${TS[@]}" $U/nope)"
curl -s -D - "${TS[@]}" http://localhost/workspaces/x -o "$WORK/spa.html" | grep -qi "content-security-policy: .*nonce-" && ok "SPA fallback with nonce CSP" || bad "SPA CSP"
grep -q '<script nonce=' "$WORK/spa.html" && ok "nonce injected" || bad "nonce injection"

echo "== workspace and registry"
expect "workspace detail" 200 "$(code "${TS[@]}" $U/workspaces/hetzner1)"
expect "components (manifest missing before dev)" 200 "$(code "${TS[@]}" $U/workspaces/hetzner1/sites/demo/components)"

echo "== dev session"
curl -s "${TS[@]}" -N "$U/workspaces/hetzner1/sites/demo/events" > "$WORK/events.sse" &
EVPID=$!
MUT=("${TS[@]}" "${J[@]}" -H "Origin: https://$HOSTNAME_TS:$CP" -H 'Sec-Fetch-Site: same-origin')
expect "dev start → 202" 202 "$(code "${MUT[@]}" -X POST -d '{}' $U/workspaces/hetzner1/sites/demo/dev/start)"
st=""
for _ in $(seq 1 60); do st=$(curl -s "${TS[@]}" $U/workspaces/hetzner1/sites/demo/dev | field status); [ "$st" = ready ] && break; sleep 0.25; done
expect "dev becomes ready (control ping with token)" ready "$st"
grep -q '^CF_DEV_PORT=' "$WORK/run/dev/demo.env" && grep -q '^COUCHFUSION_CONTROL_TOKEN=' "$WORK/run/dev/demo.env" && ok "dev env file written" || bad "dev env file"
for _ in $(seq 1 30); do [ -f "$APP/app/content-builder/component-definitions.json" ] && break; sleep 0.2; done
sleep 0.5
curl -s "${TS[@]}" $U/workspaces/hetzner1/sites/demo/components | grep -q '"status":"ready"' && ok "registry ready" || bad "registry"

echo "== slot proxy"
SL=(--unix-socket "ts-$SLOT.sock" -H "X-Forwarded-Host: $HOSTNAME_TS:$SLOT" -H "Tailscale-User-Login: $LOGIN")
H=$(curl -s -D - "${SL[@]}" -H 'Cookie: cf_cp_s=secret; AuthSession=keep' http://localhost/some/page)
echo "$H" | grep -q '"host": "127.0.0.1:'"$DEV"'"' && ok "Host rewritten" || bad "Host rewrite"
echo "$H" | grep -q '"cookie": "AuthSession=keep"' && ok "session cookie stripped" || bad "cookie strip"
echo "$H" | grep -qi 'tailscale-user-login' && bad "tailscale header leaked" || ok "tailscale headers stripped"
echo "$H" | grep -qi '^x-frame-options' && bad "x-frame-options kept" || ok "x-frame-options removed"
echo "$H" | grep -qi "content-security-policy: frame-ancestors 'self' https://$HOSTNAME_TS:$CP" && ok "frame-ancestors set" || bad "frame-ancestors"
CE=$(curl -s -o /dev/null -D - -H 'Accept-Encoding: gzip, br' "${SL[@]}" http://localhost/some/page | tr -d '\r' | grep -i '^content-encoding:' || true)
[ -n "$CE" ] && ok "slot compresses ($CE)" || bad "slot compression"
expect "slot requires auth" 401 "$(code --unix-socket "ts-$SLOT.sock" -H "X-Forwarded-Host: $HOSTNAME_TS:$SLOT" http://localhost/)"
WS=$(python3 - "ts-$SLOT.sock" "$HOSTNAME_TS:$SLOT" "$LOGIN" <<'PY'
import socket, sys
s = socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); s.settimeout(5)
s.sendall(f"GET /_nuxt/ HTTP/1.1\r\nHost: localhost\r\nX-Forwarded-Host: {sys.argv[2]}\r\nTailscale-User-Login: {sys.argv[3]}\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n".encode())
head = b""
while b"\r\n\r\n" not in head:
    head += s.recv(1)
s.sendall(b"ping")
print(head.split(b"\r\n")[0].decode(), s.recv(100).decode())
PY
)
echo "$WS" | grep -q "101.*echo:ping" && ok "websocket passthrough ($WS)" || bad "websocket: $WS"

echo "== component usage and delete"
SHA=$(curl -s "${TS[@]}" $U/workspaces/hetzner1/sites/demo/components/phil/usage | field sha256)
[ ${#SHA} = 64 ] && ok "usage sha256" || bad "usage: $SHA"
curl -s "${TS[@]}" $U/workspaces/hetzner1/sites/demo/components/network-hero/usage | jq -e '.data.pages[0].id == "page-/"' >/dev/null && ok "usage pages via control route" || bad "usage pages"
expect "delete with wrong sha → 409" 409 "$(code "${MUT[@]}" -X DELETE -d '{"expectedSha256":"00","acknowledgeUsage":true}' $U/workspaces/hetzner1/sites/demo/components/phil)"
NSHA=$(curl -s "${TS[@]}" $U/workspaces/hetzner1/sites/demo/components/network-hero/usage | field sha256)
expect "delete used component unacknowledged → 409" 409 "$(code "${MUT[@]}" -X DELETE -d "{\"expectedSha256\":\"$NSHA\",\"acknowledgeUsage\":false}" $U/workspaces/hetzner1/sites/demo/components/network-hero)"
JOB=$(curl -s "${MUT[@]}" -X DELETE -d "{\"expectedSha256\":\"$SHA\",\"acknowledgeUsage\":true}" $U/workspaces/hetzner1/sites/demo/components/phil | field id)
js=""
for _ in $(seq 1 60); do js=$(curl -s "${TS[@]}" $U/jobs/$JOB | jq -r .data.status); [ "$js" = succeeded ] || [ "$js" = failed ] && break; sleep 0.25; done
expect "delete job" succeeded "$js"
[ -f "$WORK/state/trash/$JOB/app/components/content/Phil.vue" ] && [ ! -f "$APP/app/components/content/Phil.vue" ] && ok "file moved to trash" || bad "trash"
curl -s "${TS[@]}" $U/workspaces/hetzner1/sites/demo/components | grep -q '"id":"phil"' && bad "phil still registered" || ok "registry no longer lists phil"
curl -s "${TS[@]}" -N --max-time 2 "$U/jobs/$JOB/events" | grep -q 'event: job' && ok "job SSE replay" || bad "job SSE"

echo "== generate (fake codex)"
expect "generate without Idempotency-Key → 422" 422 "$(code "${MUT[@]}" -X POST -d '{"prompt":"pricing table"}' $U/workspaces/hetzner1/sites/demo/components/generate)"
GJ=$(curl -s "${MUT[@]}" -H 'Idempotency-Key: k1' -X POST -d '{"prompt":"pricing table"}' $U/workspaces/hetzner1/sites/demo/components/generate | field id)
GJ2=$(curl -s "${MUT[@]}" -H 'Idempotency-Key: k1' -X POST -d '{"prompt":"pricing table"}' $U/workspaces/hetzner1/sites/demo/components/generate | field id)
expect "idempotent retry returns same job" "$GJ" "$GJ2"
for _ in $(seq 1 120); do js=$(curl -s "${TS[@]}" $U/jobs/$GJ | jq -r .data.status); [ "$js" = succeeded ] || [ "$js" = failed ] && break; sleep 0.25; done
expect "generate job" succeeded "$js"
R=$(curl -s "${TS[@]}" $U/jobs/$GJ)
echo "$R" | jq -e '.data.result.componentId == "pricing-table"' >/dev/null && ok "generated componentId" || bad "componentId: $R"
echo "$R" | grep -q 'prop `tone`' && ok "builderFieldMeta lint warning" || bad "lint"
echo "$R" | grep -q 'cat app/components' && bad "raw command leaked" || ok "raw command hidden"

echo "== git"
expect "repos" 200 "$(code "${TS[@]}" $U/workspaces/hetzner1/repos)"
ST=$(curl -s "${TS[@]}" "$U/workspaces/hetzner1/repos/apps%2Fdemo/status")
echo "$ST" | grep -q 'PricingTable.vue' && ok "status lists new file" || bad "status: $ST"
HEAD=$(echo "$ST" | jq -r .data.head.sha)
expect "diff" 200 "$(code "${TS[@]}" "$U/workspaces/hetzner1/repos/apps%2Fdemo/diff?path=app/components/content/PricingTable.vue")"
CJ=$(curl -s "${MUT[@]}" -X POST -d "{\"message\":\"Add pricing table\",\"paths\":[\"app/components/content/PricingTable.vue\"],\"expectedHead\":\"$HEAD\"}" "$U/workspaces/hetzner1/repos/apps%2Fdemo/commit" | field id)
for _ in $(seq 1 40); do js=$(curl -s "${TS[@]}" $U/jobs/$CJ | jq -r .data.status); [ "$js" = succeeded ] || [ "$js" = failed ] && break; sleep 0.25; done
expect "commit job" succeeded "$js"
git -C "$APP" log -1 --format=%B | grep -q 'Committed-Via: couchfusion-cp' && ok "commit trailer" || bad "trailer"

echo "== site SSE"
sleep 0.5; kill $EVPID 2>/dev/null || true
grep -q '"type":"dev.status"' "$WORK/events.sse" && grep -q '"type":"registry.changed"' "$WORK/events.sse" && grep -q '"type":"job"' "$WORK/events.sse" && ok "site stream carried dev.status, registry.changed and job" || bad "site SSE"
grep -q '"type":"log"' "$WORK/events.sse" && ok "site stream carried dev logs" || bad "dev logs"

echo "== dev stop"
expect "dev stop → 202" 202 "$(code "${MUT[@]}" -X POST -d '{}' $U/workspaces/hetzner1/sites/demo/dev/stop)"
expect "slot shows boot page when down" 503 "$(code "${SL[@]}" -H 'Accept: text/html' http://localhost/)"

echo; echo "passed $pass, failed $fail  (work dir: $WORK, server log: $WORK/server.log)"
[ $fail -eq 0 ]
