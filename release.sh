#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 vX.Y.Z"
  exit 1
fi

VERSION="$1"

if [[ ! -d "build/linux-x86" || ! -d "build/linux-arm" || ! -d "build/darwin-amd64" || ! -d "build/darwin-arm64" || ! -d "build/windows" ]]; then
  echo "Missing build artifacts. Run ./build.sh first."
  exit 1
fi

mkdir -p dist

tar -czf "dist/couchfusion_linux_amd64.tar.gz" -C build/linux-x86 couchfusion
tar -czf "dist/couchfusion_linux_arm64.tar.gz" -C build/linux-arm couchfusion
tar -czf "dist/couchfusion_darwin_amd64.tar.gz" -C build/darwin-amd64 couchfusion
tar -czf "dist/couchfusion_darwin_arm64.tar.gz" -C build/darwin-arm64 couchfusion
(cd build/windows && zip -q ../../dist/couchfusion_windows_amd64.zip couchfusion.exe)

echo "Release artifacts staged in dist/ for ${VERSION}"
