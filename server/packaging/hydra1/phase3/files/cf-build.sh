#!/usr/bin/env bash
# /usr/local/libexec/couchfusion/cf-build.sh <snapshot> — runs inside cf-build@<app> as cfdev.
# The snapshot keeps the workspace layout (apps/<app>, layers, cli-content).
set -euo pipefail
snap=$(readlink -f "$1")
app=${CF_APP:?}
[ -f "$snap/apps/$app/package.json" ] || { echo "no app in $snap" >&2; exit 2; }
for dir in "$snap/layers" "$snap/apps/$app"; do
  [ -f "$dir/package.json" ] || continue
  echo "== bun install --frozen-lockfile in ${dir#$snap/}"
  (cd "$dir" && bun install --frozen-lockfile)
done
echo "== nuxt build in apps/$app"
cd "$snap/apps/$app"
# Calling nuxt directly skips the app's legacy postbuild (git_output.sh).
node node_modules/nuxt/bin/nuxt.mjs build
test -f .output/server/index.mjs
echo "== build ok"
