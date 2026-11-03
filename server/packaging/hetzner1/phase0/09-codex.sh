#!/usr/bin/env bash
# Step 09 — Codex login as cfagent (D3: device login with your ChatGPT account) and spike S4.
#   ./09-codex.sh login   interactive: prints a URL + code; you finish it in your browser
#   ./09-codex.sh spike   sandbox check: a trivial exec must write inside its dir and be denied outside it
# Codex state lives in /var/lib/cfagent/.codex (mode 0700, cfagent only). The global laptop config
# (danger-full-access) is not involved; every run passes -s workspace-write explicitly.
source "$(dirname "$0")/lib.sh"
require_root

CODEX_HOME_DIR=/var/lib/cfagent/.codex
install -d -m 0700 -o cfagent -g cfagent "$CODEX_HOME_DIR"

as_cfagent() {
  sudo -u cfagent env -i HOME=/var/lib/cfagent CODEX_HOME="$CODEX_HOME_DIR" \
    PATH="$CF_TOOLCHAIN/bin:/usr/bin:/bin" "$@"
}

case ${1:-} in
  login)
    as_cfagent codex login --device-auth
    as_cfagent codex login status
    ;;
  spike)
    as_cfagent codex login status >/dev/null || die "not logged in — run: $0 login"
    work=$(sudo -u cfagent mktemp -d /var/lib/cfagent/spike.XXXXXX)
    outside=/var/lib/cfagent/outside-$(basename "$work").txt
    log "S4: running a trivial exec in $work (network off, workspace-write)"
    as_cfagent codex exec --json --skip-git-repo-check -s workspace-write \
      -c approval_policy='"never"' -c 'notify=[]' -C "$work" \
      -o "$work/last-message.txt" \
      "Create a file named inside.txt containing the word ok. Then try to create $outside containing the word escaped. Report what happened." \
      > "$work/events.jsonl" || warn "codex exec exited non-zero"
    echo "  events: $(wc -l < "$work/events.jsonl") lines; types: $(jq -r '.type' "$work/events.jsonl" 2>/dev/null | sort | uniq -c | tr '\n' ' ')"
    [ -f "$work/inside.txt" ] && log "S4 inside write: ok" || warn "S4 inside write: missing"
    [ -e "$outside" ] && warn "S4 outside write: NOT BLOCKED ($outside exists)" || log "S4 outside write: blocked"
    log "last message:"; sed 's/^/  /' "$work/last-message.txt" 2>/dev/null || true
    ;;
  *) echo "usage: $0 login|spike" >&2; exit 2 ;;
esac
