#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

command -v bun >/dev/null 2>&1 || { echo "Bun is required to build the embedded UI." >&2; exit 1; }

(
  cd web
  bun install --frozen-lockfile
  bun run test:run
  bun run typecheck
  bun run generate
)

test -f web/.output/public/index.html || { echo "Nuxt generation did not produce index.html." >&2; exit 1; }
find internal/web/dist -mindepth 1 -delete
cp -R web/.output/public/. internal/web/dist/
grep -Fq "CouchFusion Workbench" internal/web/dist/index.html || { echo "Embedded UI marker is missing." >&2; exit 1; }

go test ./... -count=1

build_target() {
  local goos="$1" goarch="$2" output="$3" label="$4"
  mkdir -p "$(dirname "$output")"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$output" .
  grep -aFq "CouchFusion Workbench" "$output" || { echo "$label binary does not contain the embedded UI." >&2; exit 1; }
  echo "$label build complete"
}

build_target linux amd64 ./build/linux-x86/couchfusion "Linux amd64"
build_target linux arm64 ./build/linux-arm/couchfusion "Linux arm64"
build_target darwin amd64 ./build/darwin-amd64/couchfusion "macOS amd64"
build_target darwin arm64 ./build/darwin-arm64/couchfusion "macOS arm64"
build_target windows amd64 ./build/windows/couchfusion.exe "Windows amd64"
