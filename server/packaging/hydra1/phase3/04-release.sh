#!/usr/bin/env bash
# Step 04 — manual production release (spec §7.2), until the control plane's release.deploy job
# exists. Usage:
#   04-release.sh deploy            snapshot the workspace, build, release, switch, restart, check
#   04-release.sh rollback [<rel>]  switch back (default: the release before the current one)
#   04-release.sh activate <rel>    switch to an already-built release (checked, rolls back on failure)
#   04-release.sh status
# Repository code runs only as cfdev (cf-build@); files are copied and switched as cfcp; the
# restart goes through polkit as cfcp, the way the control plane will do it.
source "$(dirname "$0")/lib.sh"
require_root
UNIT=cf-site@$CF_APP.service
REL=$CF_WWW/releases

current_release() { local t; t=$(readlink "$CF_WWW/current" 2>/dev/null) || return 0; echo "${t#releases/}"; }

switch_to() { # <release dir name>
  [ -f "$REL/$1/server/index.mjs" ] || die "release $1 has no server/index.mjs"
  as_cfcp ln -sfn "releases/$1" "$CF_WWW/current.new"
  as_cfcp mv -T "$CF_WWW/current.new" "$CF_WWW/current"
  log "current -> releases/$1"
}

restart_and_check() {
  systemctl enable "$UNIT" >/dev/null 2>&1 || true
  (cd / && sudo -u cfcp systemctl --no-ask-password restart "$UNIT") || return 1
  local i code ok=0
  for i in $(seq 1 30); do systemctl is-active --quiet "$UNIT" && break; sleep 1; done
  systemctl is-active --quiet "$UNIT" || { warn "$UNIT not active after 30 s"; return 1; }
  for i in $(seq 1 45); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "http://127.0.0.1:$CF_PROD_PORT/" || true)
    if [ "${code:-000}" != 000 ] && [ "$code" -lt 500 ]; then ok=$((ok+1)); [ $ok -ge 3 ] && { log "health: 3 × HTTP $code"; return 0; }; else ok=0; fi
    sleep 1
  done
  warn "health check failed (last HTTP ${code:-none})"; return 1
}

prune() {
  local cur; cur=$(current_release)
  ls -1 "$REL" | sort | head -n -"$CF_KEEP_RELEASES" | while read -r r; do
    [ "$r" = "$cur" ] && continue; as_cfcp rm -rf "${REL:?}/$r"; log "pruned release $r"; done
  ls -1 "$CF_BUILD" | grep -E '^[0-9]{8}T[0-9]{6}Z$' | sort | head -n -"$CF_KEEP_SNAPSHOTS" | while read -r s; do
    as_cfcp rm -rf "${CF_BUILD:?}/$s"; log "pruned snapshot $s"; done
}

repo_state() { # <repo path> -> "sha dirty"
  local r=$1 sha dirty=false
  sha=$(as_cfcp git -c safe.directory="$r" -C "$r" rev-parse HEAD 2>/dev/null || echo unknown)
  [ -n "$(as_cfcp git -c safe.directory="$r" -C "$r" status --porcelain 2>/dev/null | head -1)" ] && dirty=true
  echo "$sha $dirty"
}

deploy() {
  local mem disk ts snap sha dirty rel prev
  mem=$(mem_available_mib); disk=$(df --output=avail -BG /var | tail -1 | tr -dc 0-9)
  [ "$mem" -ge 1229 ] || die "admission: MemAvailable ${mem} MiB < 1.2 GiB (stop dev from the UI first)"
  [ "$disk" -ge 3 ] || die "admission: ${disk} GiB free on /var < 3 GiB"
  ts=$(date -u +%Y%m%dT%H%M%SZ); snap=$CF_BUILD/$ts
  log "snapshot -> $snap"
  as_cfcp install -d -m 2770 "$snap"
  for repo in "apps/$CF_APP" layers cli-content; do
    [ -d "$CF_WORKSPACE/$repo" ] || continue
    as_cfcp mkdir -p "$snap/$(dirname "$repo")"
    as_cfcp rsync -a --delete --chmod=Dg+rwxs,Fg+rw \
      --exclude=node_modules --exclude=.nuxt --exclude=.nuxt-public --exclude=.nuxt-admin --exclude=.output --exclude=.output_admin --exclude='.env*' \
      --exclude=.git --exclude=test-results --exclude='._*' \
      "$CF_WORKSPACE/$repo/" "$snap/$repo/"
  done
  read -r sha dirty <<<"$(repo_state "$CF_WORKSPACE/apps/$CF_APP")"
  read -r lsha ldirty <<<"$(repo_state "$CF_WORKSPACE/layers")"
  printf '{"app":"%s","snapshot":"%s","source":"worktree","repos":{"apps/%s":{"head":"%s","dirty":%s},"layers":{"head":"%s","dirty":%s}}}\n' \
    "$CF_APP" "$ts" "$CF_APP" "$sha" "$dirty" "$lsha" "$ldirty" | as_cfcp tee "$snap/release.json" >/dev/null
  as_cfcp ln -sfn "$ts" "$CF_BUILD/next"

  log "build (cf-build@$CF_APP as cfdev; follow with: journalctl -fu cf-build@$CF_APP)"
  local t0=$SECONDS
  if ! (cd / && sudo -u cfcp systemctl --no-ask-password start "cf-build@$CF_APP.service"); then
    journalctl -u "cf-build@$CF_APP" -n 40 --no-pager -o cat
    die "build failed; production unchanged"
  fi
  log "build ok in $((SECONDS - t0)) s"

  rel="$ts-${sha:0:12}"; [ "$dirty" = true ] && rel="$rel-dirty"
  log "release -> $REL/$rel"
  as_cfcp cp -r --preserve=mode,timestamps "$snap/apps/$CF_APP/.output" "$REL/$rel"
  as_cfcp cp "$snap/release.json" "$REL/$rel/release.json"
  as_cfcp chmod -R g+rX,g-w,o-rwx "$REL/$rel"

  prev=$(current_release)
  switch_to "$rel"
  if restart_and_check; then
    log "deployed $rel (previous: ${prev:-none})"
    prune
  else
    if [ -n "$prev" ]; then
      warn "rolling back to $prev"; switch_to "$prev"; restart_and_check || warn "previous release is unhealthy too"
      journalctl -u "$UNIT" -n 30 --no-pager -o cat
      die "prod.rolled_back: $rel failed its health check"
    fi
    journalctl -u "$UNIT" -n 30 --no-pager -o cat
    die "first release $rel failed its health check (nothing to roll back to)"
  fi
}

rollback() {
  local cur target=${1:-}
  cur=$(current_release)
  if [ -z "$target" ]; then
    target=$(ls -1 "$REL" | sort | grep -B1 -x "$cur" | head -1)
    [ -n "$target" ] && [ "$target" != "$cur" ] || die "no release before $cur"
  fi
  switch_to "$target"
  restart_and_check || { warn "rollback target unhealthy; switching back to $cur"; switch_to "$cur"; restart_and_check; die "rollback failed"; }
  log "rolled back $cur -> $target"
}

status() {
  log "current: $(current_release || echo none)"
  ls -1 "$REL" 2>/dev/null | sort | sed 's/^/  /'
  systemctl show "$UNIT" -p ActiveState -p SubState -p MemoryCurrent --no-pager
}

activate() { # <rel>: switch to an already-built release and check it (rolls back on failure)
  local rel=${1:?release name} prev; prev=$(current_release)
  switch_to "$rel"
  if restart_and_check; then log "activated $rel (previous: ${prev:-none})"; prune; return 0; fi
  journalctl -u "$UNIT" -n 30 --no-pager -o cat
  if [ -n "$prev" ]; then switch_to "$prev"; restart_and_check || true; die "prod.rolled_back: $rel failed its health check"; fi
  die "$rel failed its health check (nothing to roll back to)"
}

case "${1:-}" in
  deploy) deploy ;;
  activate) activate "${2:-}" ;;
  rollback) rollback "${2:-}" ;;
  status) status ;;
  *) die "usage: $0 deploy | rollback [<release>] | activate <release> | status" ;;
esac
