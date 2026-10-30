#!/usr/bin/env bash
# Builds a static x86_64 Linux binary of couchfusion-server (for hetzner1) on macOS or Linux,
# and bundles it with the built SPA into dist/couchfusion-server-<ver>.tar.zst.
#
# Needs: rustup target x86_64-unknown-linux-musl, cargo-zigbuild, zig (on PATH or ~/.local/bin).
# The SPA must already be generated (cli-init/web: bun run generate → .output/public); pass
# --no-spa to package the binary only.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
TARGET=x86_64-unknown-linux-musl
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
WITH_SPA=1
[ "${1:-}" = "--no-spa" ] && WITH_SPA=0

cd "$HERE"
VER=$(sed -n 's/^version = "\(.*\)"/\1/p' couchfusion-server/Cargo.toml | head -1)
GIT=$(git -C "$HERE" rev-parse --short HEAD 2>/dev/null || echo nogit)
# Uncommitted builds get a timestamp so each one unpacks into its own release directory.
if [ -n "$(git -C "$HERE/.." status --porcelain -- server web 2>/dev/null)" ]; then GIT="$GIT-dirty$(date -u +%Y%m%d%H%M)"; fi
REL="$VER-$GIT"

cargo test -q
cargo zigbuild --release --target "$TARGET" -p couchfusion-server
BIN=target/$TARGET/release/couchfusion-server
file "$BIN" | grep -q "statically linked" || { echo "binary is not static" >&2; exit 1; }

STAGE=$(mktemp -d)
trap 'rm -rf "${STAGE:?}"' EXIT
mkdir -p "$STAGE/couchfusion-$REL"
cp "$BIN" "$STAGE/couchfusion-$REL/couchfusion-server"
if [ $WITH_SPA = 1 ]; then
  SPA="$HERE/../web/.output/public"
  [ -f "$SPA/index.html" ] || { echo "SPA not built at $SPA (cd ../web && bun run generate), or pass --no-spa" >&2; exit 1; }
  cp -R "$SPA" "$STAGE/couchfusion-$REL/web"
fi
mkdir -p dist
COPYFILE_DISABLE=1 tar --no-mac-metadata --zstd -cf "dist/couchfusion-server-$REL.tar.zst" -C "$STAGE" "couchfusion-$REL"
(cd dist && shasum -a 256 "couchfusion-server-$REL.tar.zst" > "couchfusion-server-$REL.tar.zst.sha256")
ls -lh "$BIN" "dist/couchfusion-server-$REL.tar.zst"
echo "release: $REL"
