#!/bin/sh
# Fake `codex exec --json … -C <dir> … -`: reads the prompt from stdin (must be closed by the
# runner), creates one content component and prints codex-style JSONL events.
dir=""
while [ $# -gt 0 ]; do [ "$1" = "-C" ] && { dir="$2"; shift; }; shift; done
cat > /dev/null
echo '{"type":"thread.started","thread_id":"fake-thread-1"}'
echo '{"type":"turn.started"}'
# Stay busy briefly so the smoke test can observe the agent marker while the job runs.
sleep 1.5
f="$dir/app/components/content/PricingTable.vue"
cat > "$f" <<'VUE'
<script setup lang="ts">
withDefaults(defineProps<{
  title?: string
  tone?: 'light' | 'dark'
}>(), { title: 'Pricing', tone: 'light' })
</script>
<template><section>{{ title }}</section></template>
VUE
printf '{"type":"item.completed","item":{"id":"item_1","type":"file_change","changes":[{"path":"%s","kind":"add"}],"status":"completed"}}\n' "$f"
echo '{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"bash -lc \"cat app/components/content/Phil.vue\"","aggregated_output":"","exit_code":0,"status":"completed"}}'
echo '{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Created PricingTable.vue."}}'
echo '{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":30,"output_tokens":20}}'
