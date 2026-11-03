#!/usr/bin/env bash
# Step 05 — systemd slice, the cf-dev@ template, and the dev env file template.
# Nothing is started here. Existing units are not modified or restarted.
# Undo: rm /etc/systemd/system/{couchfusion.slice,cf-dev@.service}; systemctl daemon-reload
source "$(dirname "$0")/lib.sh"
require_root

install_file /etc/systemd/system/couchfusion.slice "[Unit]
Description=CouchFusion dev, agent and build workloads
Before=slices.target

[Slice]
MemoryHigh=1700M
MemoryMax=2G
MemorySwapMax=1G
CPUWeight=50"

install_file /etc/systemd/system/cf-dev@.service "[Unit]
Description=CouchFusion nuxt dev for %i
After=network.target couchdb.service couchfusion-nft.service
Requires=couchfusion-nft.service

[Service]
Type=simple
User=cfdev
Group=cfdev
SupplementaryGroups=cfsrc
Slice=couchfusion.slice
UMask=0002
WorkingDirectory=$CF_WORKSPACE/apps/%i
EnvironmentFile=$CF_ETC/env/%i.dev.env
Environment=HOME=/var/lib/cfdev
Environment=PATH=$CF_TOOLCHAIN/bin:/usr/bin:/bin
Environment=NODE_ENV=development
Environment=NUXT_TELEMETRY_DISABLED=1
# Bind loopback only, on an explicit port; never 'bun run dev' (its --host binds all interfaces).
ExecStart=$CF_TOOLCHAIN/bin/node node_modules/nuxt/bin/nuxt.mjs dev --host 127.0.0.1 --port \${CF_DEV_PORT}
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
KillMode=control-group
MemoryHigh=1100M
MemoryMax=1300M
CPUQuota=150%
Nice=5

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=$CF_WORKSPACE /var/lib/cfdev
# Git metadata stays out of reach of dev-server code (npm packages, Vite plugins).
InaccessiblePaths=-$CF_WORKSPACE/apps/%i/.git -$CF_WORKSPACE/layers/.git -$CF_WORKSPACE/cli-content/.git
InaccessiblePaths=-$CF_ETC/credentials -/var/lib/couchfusion -/var/lib/cfagent

[Install]
WantedBy=multi-user.target"

install -d -m 0755 -o root -g root "$CF_ETC" "$CF_ETC/env"
install -d -m 0700 -o root -g root "$CF_ETC/credentials"

ENV_FILE=$CF_ETC/env/$CF_APP.dev.env
if [ -f "$ENV_FILE" ]; then
  log "keeping existing $ENV_FILE (values are never printed)"
else
  install_file "$ENV_FILE" "# Dev env for $CF_APP — fill in values with: sudoedit $ENV_FILE
# Readable by root and the cfdev group only. Never copy this into the workspace as .env.
CF_DEV_PORT=$CF_DEV_PORT

# D1: dev shares the production CouchDB on hetzner1.
COUCHDB_URL=http://127.0.0.1:5984
COUCHDB_ADMIN_AUTH=
COUCHDB_COOKIE_SECRET=

PUBLIC_SITE_URL=https://hetzner1.tailb7471b.ts.net:$CF_DEV_SLOT

IMAGEKIT_PUBLIC_KEY=
IMAGEKIT_PRIVATE_KEY=
IMAGEKIT_URL_ENDPOINT=
NUXT_IMAGEKIT_FOLDER=

# Analytics stays off in dev.
NUXT_PUBLIC_UMAMI_WEBSITE_ID=
NUXT_PUBLIC_UMAMI_HOST_URL=

# Let Vite accept the tailnet Host header (spike S2).
__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS=hetzner1.tailb7471b.ts.net" 0640 root:cfdev
fi

systemctl daemon-reload
log "units installed; not started. Fill $ENV_FILE before step 10."
