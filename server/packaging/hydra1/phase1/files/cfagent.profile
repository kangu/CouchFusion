# ~/.profile for cfagent (installed by couchfusion phase1 03-units.sh).
# Codex runs commands with a login shell, and /etc/profile resets PATH for non-root users,
# so the pinned CouchFusion toolchain (node, bun, codex) is added back here.
export PATH="/opt/couchfusion/toolchain/bin:$PATH"