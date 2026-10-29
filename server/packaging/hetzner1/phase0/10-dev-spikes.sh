#!/usr/bin/env bash
# Step 10 — start the sandboxed dev server and run spikes S1 and S5.
#   S1: nuxt dev + registry watcher work inside cf-dev@ (manifest updates within ~1 s of a .vue add/delete)
#   S5: memory use of the dev server
# The probe component is created and removed again; the registry files end up as before
# (apart from the watcher rewriting them), all inside the VPS copy.
# Prereqs: steps 01-08 done, and the dev env file filled in (CouchDB data present, D1).
source "$(dirname "$0")/lib.sh"
require_root

UNIT=cf-dev@$CF_APP.service
APP_DIR=$CF_WORKSPACE/apps/$CF_APP
MANIFEST=$APP_DIR/app/content-builder/component-definitions.json
PROBE=$APP_DIR/app/components/content/ZzPhase0Probe.vue

grep -q '^COUCHDB_ADMIN_AUTH=.\+' "$CF_ETC/env/$CF_APP.dev.env" || die "fill $CF_ETC/env/$CF_APP.dev.env first (sudoedit)"

systemctl start "$UNIT"
log "waiting for 127.0.0.1:$CF_DEV_PORT (first compile can take a few minutes)…"
# Nuxt dev answers 503 while Vite is still building, so wait for a real 200.
for i in $(seq 1 300); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$CF_DEV_PORT/" || true)
  [ "$code" = 200 ] && break
  systemctl is-active -q "$UNIT" || { journalctl -u "$UNIT" -n 60 --no-pager; die "$UNIT stopped"; }
  sleep 1
done
log "dev answered HTTP $code after ${i}s"

# The registry watcher does an initial run first; probing during it races the generator.
log "waiting for the registry watcher to be idle…"
for _ in $(seq 1 60); do
  journalctl -u "$UNIT" -o cat --no-pager | grep -q '\[registry\] Watching directories' && break
  sleep 1
done
sleep 2

has_probe() { jq -e '.components[] | select(.id=="zz-phase0-probe")' "$MANIFEST" >/dev/null 2>&1; }

log "S1: adding probe component"
sudo -u cfdev -g cfsrc tee "$PROBE" >/dev/null <<'VUE'
<script setup lang="ts">
withDefaults(defineProps<{ title?: string }>(), { title: 'Phase 0 probe' })
</script>

<template>
  <section>{{ title }}</section>
</template>
VUE
t0=$(date +%s%N)
for _ in $(seq 1 150); do has_probe && break; sleep 0.1; done
if has_probe; then
  log "S1 add: manifest updated in $(( ($(date +%s%N) - t0) / 1000000 )) ms"
  rm -f "$PROBE"
  t0=$(date +%s%N)
  for _ in $(seq 1 150); do has_probe || break; sleep 0.1; done
  if has_probe; then warn "S1 delete: probe still in manifest after 15 s"; else log "S1 delete: manifest updated in $(( ($(date +%s%N) - t0) / 1000000 )) ms"; fi
else
  warn "S1 add: manifest not updated within 15 s"
  rm -f "$PROBE"
fi
log "registry log for this test:"
journalctl -u "$UNIT" -o cat --since '-40s' --no-pager | grep -E '^\[registry\]' | sed 's/^/  /'

log "S5: memory"
systemctl show "$UNIT" -p MemoryCurrent -p MemoryPeak -p MemorySwapCurrent | sed 's/^/  /'
free -h | sed 's/^/  /'

log "recent dev log:"
journalctl -u "$UNIT" -n 25 --no-pager | sed 's/^/  /'
log "S2 (manual): open https://hetzner1.tailb7471b.ts.net:$CF_DEV_SLOT/k/ on the laptop, edit a .vue on the VPS, and check it updates without a reload"
