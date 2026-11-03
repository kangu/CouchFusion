#!/usr/bin/env bash
# Step 10 — pin the Codex model for LLM work (New component and LLM request) and show it in the
# dev bar. Needs a couchfusion-server release with `llm-request` (install it first with
# phase1/02-release.sh). Adds an `[agent]` section to server.toml, validates it with the
# installed binary (restores the old file on failure), then restarts the control plane (dev
# units keep running and are re-adopted).
# Usage: 10-agent-model.sh [model] [reasoning_effort]   (default model: gpt-5.6-sol, effort unset)
# Undo: cp /etc/couchfusion/server.toml.bak-agent /etc/couchfusion/server.toml; systemctl restart couchfusion
source "$(dirname "$0")/lib.sh"
require_root
MODEL=${1:-gpt-5.6-sol}
EFFORT=${2:-}
TOML=$CF_ETC/server.toml
BIN=$CF_ROOT/current/couchfusion-server
"$BIN" --version
[[ "$MODEL" =~ ^[A-Za-z0-9._:][A-Za-z0-9._:-]{0,63}$ ]] || die "invalid model $MODEL"
case "$EFFORT" in ""|minimal|low|medium|high|xhigh) ;; *) die "invalid reasoning effort $EFFORT" ;; esac

[ -f "$TOML.bak-agent" ] || cp -a "$TOML" "$TOML.bak-agent"
cp -a "$TOML" "$TOML.prev"
# Drop an existing [agent] section (up to the next section), then append the new one.
awk 'BEGIN { skip = 0 } /^\[/ { skip = ($0 == "[agent]") } !skip { print }' "$TOML.prev" > "$TOML.new"
{
  echo ""
  echo "# Codex model for every LLM run (generate and request), shown in the dev bar (10-agent-model.sh)."
  echo "[agent]"
  echo "model = \"$MODEL\""
  [ -n "$EFFORT" ] && echo "reasoning_effort = \"$EFFORT\""
} >> "$TOML.new"
cat "$TOML.new" > "$TOML"; rm -f "$TOML.new"
log "server.toml [agent]: model=$MODEL${EFFORT:+ reasoning_effort=$EFFORT}"
if ! (cd / && sudo -u cfcp "$BIN" doctor --config "$TOML" >/tmp/cf-doctor.log 2>&1); then
  cp -a "$TOML.prev" "$TOML"; sed -n 1,30p /tmp/cf-doctor.log; die "doctor failed with the new config; server.toml restored"
fi
log "doctor ok"
systemctl restart couchfusion
sleep 2
systemctl is-active couchfusion
