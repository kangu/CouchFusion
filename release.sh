#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 vX.Y.Z"
  exit 1
fi

VERSION="$1"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Version must use vX.Y.Z format."
  exit 1
fi

BINARIES=(
  build/linux-x86/couchfusion
  build/linux-arm/couchfusion
  build/darwin-amd64/couchfusion
  build/darwin-arm64/couchfusion
  build/windows/couchfusion.exe
)
for binary in "${BINARIES[@]}"; do
  [[ -f "$binary" ]] || { echo "Missing $binary. Run ./build.sh first."; exit 1; }
  grep -aFq "CouchFusion Workbench" "$binary" || { echo "$binary does not contain the embedded UI."; exit 1; }
done

rm -rf dist
mkdir -p dist

tar -czf "dist/couchfusion_linux_amd64.tar.gz" -C build/linux-x86 couchfusion
tar -czf "dist/couchfusion_linux_arm64.tar.gz" -C build/linux-arm couchfusion
tar -czf "dist/couchfusion_darwin_amd64.tar.gz" -C build/darwin-amd64 couchfusion
tar -czf "dist/couchfusion_darwin_arm64.tar.gz" -C build/darwin-arm64 couchfusion
(cd build/windows && zip -q ../../dist/couchfusion_windows_amd64.zip couchfusion.exe)

ARCHIVES=(
  dist/couchfusion_linux_amd64.tar.gz
  dist/couchfusion_linux_arm64.tar.gz
  dist/couchfusion_darwin_amd64.tar.gz
  dist/couchfusion_darwin_arm64.tar.gz
  dist/couchfusion_windows_amd64.zip
)
for archive in "${ARCHIVES[@]}"; do
  [[ -s "$archive" ]] || { echo "Release archive is missing or empty: $archive"; exit 1; }
done

echo "Release artifacts staged in dist/ for ${VERSION}"
