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
if [[ $app == radustanciu ]]; then
  # This checkout is a disposable snapshot. Point Vite at the current target's
  # generated TypeScript projects while Nuxt keeps its separate build caches.
  original=$(mktemp)
  cp tsconfig.json "$original"
  trap 'cp "$original" tsconfig.json; rm -f "$original"' EXIT
  for target in public admin; do
    sed "s|\\./\\.nuxt/|./.nuxt-$target/|g" "$original" >tsconfig.json
    COUCHFUSION_BUILD_TARGET=$target node node_modules/nuxt/bin/nuxt.mjs build
    dir=.output/dist
    [[ $target == public ]] || dir=.output_admin/dist
    test -f "$dir/server/index.mjs"
    node -e 'if (require(process.argv[1]).target !== process.argv[2]) process.exit(1)' "./$dir/route-inventory.json" "$target"
  done
  mv .output_admin/dist .output/admin
  mkdir -p .output/server
  cp "$(dirname "$(readlink -f "$0")")/paired-site.mjs" .output/server/index.mjs
else
  node node_modules/nuxt/bin/nuxt.mjs build
fi
test -f .output/server/index.mjs
echo "== build ok"
