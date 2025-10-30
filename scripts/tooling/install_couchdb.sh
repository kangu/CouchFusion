#!/usr/bin/env bash
set -e

# --- ASK FOR ADMIN PASSWORD ---
read -s -p "Enter new CouchDB admin password: " ADMIN_PASS
echo ""
if [ -z "$ADMIN_PASS" ]; then
  echo "❌ Admin password cannot be empty."
  exit 1
fi

# --- DEFAULT CONFIGURATION ---
COUCH_MODE="standalone"  # can be "clustered"
COUCH_BIND="127.0.0.1"  # can be "0.0.0.0" for listening
COUCH_COOKIE=$(openssl rand -hex 16)  # generates a random 32-char hex string
ADMIN_USER="admin"  # this is hardcoded from the CouchDB installation

# --- DISPLAY SETTINGS ---
echo ""
echo "🧠 CouchDB Installation Configuration"
echo "-------------------------------------"
echo " Mode:             ${COUCH_MODE}"
echo " Bind address:     ${COUCH_BIND}"
echo " Erlang cookie:    ${COUCH_COOKIE}"
echo " Admin username:   ${ADMIN_USER}"
echo " Admin password:   [hidden]"
echo "-------------------------------------"
echo ""

# --- INSTALL PREREQUISITES ---
sudo apt update -y
sudo apt install -y curl apt-transport-https gnupg git debconf-utils openssl

# --- ADD COUCHDB REPOSITORY ---
curl -fsSL https://couchdb.apache.org/repo/keys.asc \
  | gpg --dearmor \
  | sudo tee /usr/share/keyrings/couchdb-archive-keyring.gpg >/dev/null

source /etc/os-release
echo "deb [signed-by=/usr/share/keyrings/couchdb-archive-keyring.gpg] https://apache.jfrog.io/artifactory/couchdb-deb/ ${VERSION_CODENAME} main" \
  | sudo tee /etc/apt/sources.list.d/couchdb.list >/dev/null

sudo apt update -y

# --- PRESEED COUCHDB CONFIGURATION ---
echo "couchdb couchdb/mode select ${COUCH_MODE}" | sudo debconf-set-selections
echo "couchdb couchdb/bindaddress string ${COUCH_BIND}" | sudo debconf-set-selections
echo "couchdb couchdb/cookie string ${COUCH_COOKIE}" | sudo debconf-set-selections
echo "couchdb couchdb/adminpass password ${ADMIN_PASS}" | sudo debconf-set-selections
echo "couchdb couchdb/adminpass_again password ${ADMIN_PASS}" | sudo debconf-set-selections

# --- INSTALL COUCHDB SILENTLY ---
echo "🚀 Installing CouchDB non-interactively..."
sudo DEBIAN_FRONTEND=noninteractive apt install -y couchdb

# --- RESTART AND VERIFY ---
sudo systemctl restart couchdb
echo "Waiting for CouchDB to be available..."
sleep 2

# --- VERIFY INSTALLATION ---
echo ""
echo "🔍 Verifying CouchDB..."
if curl -fsS "http://${ADMIN_USER}:${ADMIN_PASS}@127.0.0.1:5984/" >/dev/null; then
  echo "✅ CouchDB is ready at: http://127.0.0.1:5984/"

  # --- INSTALL PHOTON ---

  echo "Installing Photon UI..."
  couch="http://${ADMIN_USER}:${ADMIN_PASS}@127.0.0.1:5984"; \
  head="-H Content-Type:application/json"; \
  curl $head -X PUT $couch/photon; curl https://raw.githubusercontent.com/ermouth/couch-photon/master/photon.json | \
  curl $head -X PUT $couch/photon/_design/photon -d @- ; curl $head -X PUT $couch/photon/_security -d '{}' ; \
  curl $head -X PUT $couch/_node/_local/_config/csp/attachments_enable -d '"false"' ; \
  curl $head -X PUT $couch/_node/_local/_config/chttpd_auth/same_site -d '"lax"' ; \
  couch=''; head='';

  echo "✅ Photon is available at: http://127.0.0.1:5984/photon/_design/photon/index.html"
else
  echo "❌ CouchDB setup failed or service not reachable."
  exit 1
fi
