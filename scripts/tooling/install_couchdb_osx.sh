#!/usr/bin/env bash
set -euo pipefail

# --- CONSTANTS ---
ADMIN_USER="admin"   # fixed per your requirement

ADMIN_PASS="${ADMIN_PASS:-}"

usage() {
  cat <<'EOF'
Usage:
  ADMIN_PASS=<pass> ./install-couchdb.sh
or run without env var to be prompted (a strong password will be generated if you leave it blank).
EOF
}

prompt_for_pass() {
  if [[ -z "${ADMIN_PASS}" ]]; then
    read -rsp "Admin password for user 'admin' (leave empty to auto-generate): " ADMIN_PASS || true
    echo ""
    if [[ -z "${ADMIN_PASS}" ]]; then
      ADMIN_PASS="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24)"
      echo "Generated strong password."
      GENERATED_PASS=1
    else
      GENERATED_PASS=0
    fi
  else
    GENERATED_PASS=0
  fi
}

echo "🔍 Checking for Homebrew..."
BREW_BIN="$(command -v brew || true)"
if [[ -z "${BREW_BIN}" ]]; then
  echo "🍺 Homebrew not found. Installing..."
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  if [[ -x /opt/homebrew/bin/brew ]]; then
    BREW_BIN="/opt/homebrew/bin/brew"
  elif [[ -x /usr/local/bin/brew ]]; then
    BREW_BIN="/usr/local/bin/brew"
  else
    echo "❌ Brew installation path not found."
    exit 1
  fi
  echo >> "${HOME}/.zprofile"
  echo "eval \"\$(${BREW_BIN} shellenv)\"" >> "${HOME}/.zprofile"
  eval "$(${BREW_BIN} shellenv)"
  echo "✅ Homebrew installed and shell configured."
else
  echo "✅ Homebrew is already installed."
fi

# Ensure brew is on PATH for this shell
if ! command -v brew >/dev/null 2>&1; then
  if [[ -x /opt/homebrew/bin/brew ]]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
  elif [[ -x /usr/local/bin/brew ]]; then
    eval "$(/usr/local/bin/brew shellenv)"
  fi
fi

echo "🔄 Updating Homebrew..."
brew update

echo "📦 Installing CouchDB..."
brew install couchdb

BREW_PREFIX="$(brew --prefix)"
CONF_FILE="$BREW_PREFIX/etc/local.ini"
LOCAL_D="$BREW_PREFIX/etc/local.d"
LOG_FILE="$BREW_PREFIX/var/log/couchdb/couch.log"

mkdir -p "$LOCAL_D"
touch "$CONF_FILE"

prompt_for_pass

echo "🧩 Configuring admin + single_node in $CONF_FILE ..."
# Ensure [admins]
if ! grep -q '^\[admins\]' "$CONF_FILE"; then
  {
    echo ""
    echo "[admins]"
  } >> "$CONF_FILE"
fi

# Set/replace admin line (macOS sed needs -i '')
if grep -q "^${ADMIN_USER} *= *" "$CONF_FILE"; then
  sed -i '' "s|^${ADMIN_USER} *= *.*|${ADMIN_USER} = ${ADMIN_PASS}|g" "$CONF_FILE"
else
  echo "${ADMIN_USER} = ${ADMIN_PASS}" >> "$CONF_FILE"
fi

# Ensure [couchdb] single_node = true
if ! grep -q '^\[couchdb\]' "$CONF_FILE"; then
  {
    echo ""
    echo "[couchdb]"
  } >> "$CONF_FILE"
fi
if grep -q '^single_node *= *' "$CONF_FILE"; then
  sed -i '' "s|^single_node *= *.*|single_node = true|g" "$CONF_FILE"
else
  echo "single_node = true" >> "$CONF_FILE"
fi
chmod 0644 "$CONF_FILE"

echo "⚙️  Writing single-node network config into $LOCAL_D/10-single-node.ini ..."
cat > "$LOCAL_D/10-single-node.ini" <<'EOF'
[chttpd]
bind_address = 0.0.0.0
port = 5984

[cluster]
n = 1
EOF
chmod 0644 "$LOCAL_D/10-single-node.ini"

echo "🧰 Restarting CouchDB..."
brew services stop couchdb >/dev/null 2>&1 || true
brew services start couchdb
sleep 2

echo "✅ Verifying CouchDB is up and credentials work..."
if curl -sS -u "${ADMIN_USER}:${ADMIN_PASS}" http://127.0.0.1:5984/_up | grep -q '"status":"ok"'; then
  :
else
  if ! curl -sS -u "${ADMIN_USER}:${ADMIN_PASS}" http://127.0.0.1:5984/ | grep -q '"couchdb"'; then
    echo "❌ Could not verify CouchDB with provided credentials."
    echo "   Check logs: $LOG_FILE"
    exit 1
  fi
fi

# Ensure system DBs exist (safety net)
COUCH="http://${ADMIN_USER}:${ADMIN_PASS}@127.0.0.1:5984"
ensure_db () {
  local db="$1"
  if [[ "$(curl -s -o /dev/null -w '%{http_code}' "$COUCH/$db")" != "200" ]]; then
    curl -sSf -X PUT "$COUCH/$db" >/dev/null
  fi
}
echo "🧱 Ensuring system databases exist..."
ensure_db "_users"
ensure_db "_replicator"
ensure_db "_global_changes"

# --- INSTALL PHOTON ---
echo "💡 Installing Photon UI..."
HEAD_CT=(-H "Content-Type: application/json")

# Create DB (ignore "already exists")
if ! curl -sSf -X PUT "$COUCH/photon" >/dev/null 2>&1; then
  EXIST_CODE="$(curl -s -o /dev/null -w '%{http_code}' -X GET "$COUCH/photon")" || true
  if [[ "$EXIST_CODE" != "200" ]]; then
    echo "❌ Could not create or access 'photon' database (HTTP $EXIST_CODE)."
    exit 1
  fi
fi

# Upload design doc
curl -sS https://raw.githubusercontent.com/ermouth/couch-photon/master/photon.json \
  | curl -sSf "${HEAD_CT[@]}" -X PUT "$COUCH/photon/_design/photon" -d @-

# Optional tweaks
curl -sSf "${HEAD_CT[@]}" -X PUT "$COUCH/_node/_local/_config/csp/attachments_enable" -d '"false"'
curl -sSf "${HEAD_CT[@]}" -X PUT "$COUCH/_node/_local/_config/chttpd_auth/same_site" -d '"lax"'

echo ""
echo "🎉 CouchDB is installed, secured, and running (single node)."
echo "----------------------------------------------"
echo "• Admin UI:        http://127.0.0.1:5984/_utils/"
echo "• Admin username:  ${ADMIN_USER}"
if [[ "${GENERATED_PASS:-0}" -eq 1 ]]; then
  echo "• Admin password:  ${ADMIN_PASS}"
fi
echo "• Bound address:   0.0.0.0"
echo "• Port:            5984"
echo "• Photon UI:       http://127.0.0.1:5984/photon/_design/photon/index.html"
echo "• Logs:            $LOG_FILE"
echo "----------------------------------------------"
echo "Tip: Binding to 0.0.0.0 exposes CouchDB on your network; restrict access via firewall or reverse proxy."