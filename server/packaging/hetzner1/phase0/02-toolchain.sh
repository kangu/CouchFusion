#!/usr/bin/env bash
# Step 02 — pinned, root-owned, read-only toolchain under /opt/couchfusion/toolchain.
# Downloads only from nodejs.org, github.com/oven-sh/bun and the npm registry, and verifies checksums.
# Does not touch /opt/clients-node or /home/radu/.bun (used by existing services).
# Undo: rm -rf /opt/couchfusion/toolchain
source "$(dirname "$0")/lib.sh"
require_root

log "apt packages (build tools for native modules such as better-sqlite3)"
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
  build-essential python3 rsync zstd unzip curl ca-certificates acl jq >/dev/null

mkdir -p "$CF_TOOLCHAIN/bin" "$CF_TOOLCHAIN/dl"
cd "$CF_TOOLCHAIN/dl"

# --- node ---
NODE_DIR=$CF_TOOLCHAIN/node-v$NODE_VERSION
if [ ! -x "$NODE_DIR/bin/node" ]; then
  tarball=node-v$NODE_VERSION-linux-x64.tar.xz
  curl -fsSLO "https://nodejs.org/dist/v$NODE_VERSION/$tarball"
  curl -fsSL "https://nodejs.org/dist/v$NODE_VERSION/SHASUMS256.txt" -o node-SHASUMS256.txt
  grep " $tarball\$" node-SHASUMS256.txt | sha256sum -c -
  tar -xJf "$tarball" -C "$CF_TOOLCHAIN"
  mv "$CF_TOOLCHAIN/node-v$NODE_VERSION-linux-x64" "$NODE_DIR"
  log "installed node $NODE_VERSION"
else
  log "node $NODE_VERSION present"
fi

# --- bun ---
BUN_DIR=$CF_TOOLCHAIN/bun-v$BUN_VERSION
if [ ! -x "$BUN_DIR/bun" ]; then
  zip=bun-linux-x64.zip
  curl -fsSLO "https://github.com/oven-sh/bun/releases/download/bun-v$BUN_VERSION/$zip"
  curl -fsSL "https://github.com/oven-sh/bun/releases/download/bun-v$BUN_VERSION/SHASUMS256.txt" -o bun-SHASUMS256.txt
  grep " $zip\$" bun-SHASUMS256.txt | sha256sum -c -
  rm -rf bun-linux-x64 && unzip -q "$zip"
  mkdir -p "$BUN_DIR" && mv bun-linux-x64/bun "$BUN_DIR/bun"
  log "installed bun $BUN_VERSION"
else
  log "bun $BUN_VERSION present"
fi

# --- codex (official npm package, which ships the native linux binary) ---
CODEX_DIR=$CF_TOOLCHAIN/codex-v$CODEX_VERSION
if [ ! -x "$CODEX_DIR/bin/codex" ]; then
  PATH="$NODE_DIR/bin:$PATH" "$NODE_DIR/bin/npm" install --global --prefix "$CODEX_DIR" --no-fund --no-audit "@openai/codex@$CODEX_VERSION" >/dev/null
  log "installed codex $CODEX_VERSION"
else
  log "codex $CODEX_VERSION present"
fi

# --- stable bin/ symlinks (units and scripts only use these) ---
ln -sfn "$NODE_DIR/bin/node" "$CF_TOOLCHAIN/bin/node"
ln -sfn "$NODE_DIR/bin/npm" "$CF_TOOLCHAIN/bin/npm"
ln -sfn "$NODE_DIR/bin/npx" "$CF_TOOLCHAIN/bin/npx"
ln -sfn "$BUN_DIR/bun" "$CF_TOOLCHAIN/bin/bun"
ln -sfn "$BUN_DIR/bun" "$CF_TOOLCHAIN/bin/bunx"
ln -sfn "$CODEX_DIR/bin/codex" "$CF_TOOLCHAIN/bin/codex"

rm -rf "$CF_TOOLCHAIN/dl"
chown -R root:root "$CF_ROOT"
chmod -R go-w "$CF_ROOT"

log "versions:"
PATH=$CF_TOOLCHAIN/bin:$PATH
echo "  node  $(node --version)"
echo "  bun   $(bun --version)"
echo "  codex $(codex --version)"
