//! Server configuration (`/etc/couchfusion/server.toml`).

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use anyhow::{Context, bail};
use serde::Deserialize;

/// Which unit backend runs dev servers and agent runs.
#[derive(Debug, Clone, Copy, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum RunnerKind {
    /// `systemctl` / `journalctl` (hetzner1).
    Systemd,
    /// Local child processes in their own process group (macOS, tests).
    ProcessGroup,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Config {
    pub server: ServerConfig,
    pub auth: AuthConfig,
    pub workspace: WorkspaceConfig,
    #[serde(default)]
    pub toolchain: ToolchainConfig,
    #[serde(default)]
    pub sites: Vec<SiteConfig>,
    /// Local unit definitions for the `process_group` runner, keyed by unit name.
    #[serde(default)]
    pub process_group: BTreeMap<String, LocalUnit>,
    /// Read-only service status/logs (Phase 2).
    #[serde(default)]
    pub services: ServicesConfig,
}

/// Units whose status and recent logs the control plane may show (read-only).
#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ServicesConfig {
    #[serde(default = "default_service_units")]
    pub units: Vec<String>,
}

impl Default for ServicesConfig {
    fn default() -> Self {
        Self { units: default_service_units() }
    }
}

fn default_service_units() -> Vec<String> {
    [
        "kangu.service",
        "clients-kangu.service",
        "clients-kangu-dev.service",
        "mindarchy.service",
        "caddy.service",
        "couchdb.service",
        "email-sender.service",
        "cf-dev@radustanciu.service",
        "couchfusion.service",
    ]
    .iter()
    .map(|s| s.to_string())
    .collect()
}

/// systemd unit names accepted in `[services]`: plain `.service` names, no paths or globs.
pub fn valid_service_unit(unit: &str) -> bool {
    let Some(stem) = unit.strip_suffix(".service") else { return false };
    !stem.is_empty()
        && stem.len() <= 120
        && !stem.starts_with('-')
        && stem.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'@' | b'.' | b'_' | b'-' | b':'))
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ServerConfig {
    /// sqlite, trash, agent output.
    pub state_dir: PathBuf,
    /// Unix sockets, dev env files, agent run specs.
    pub run_dir: PathBuf,
    /// Built SPA (`nuxt generate` output). Served with SPA fallback.
    pub spa_dir: PathBuf,
    /// Tailnet host name, e.g. `hetzner1.tailb7471b.ts.net`.
    pub public_host: String,
    #[serde(default = "default_control_port")]
    pub control_port: u16,
    #[serde(default = "default_runner")]
    pub runner: RunnerKind,
    /// Also listen on unix sockets for `tailscale serve` (identity headers trusted only there).
    #[serde(default = "default_true")]
    pub unix_listeners: bool,
    /// Also listen on 127.0.0.1 TCP for the SSH tunnel (cookie auth).
    #[serde(default = "default_true")]
    pub tcp_listeners: bool,
    /// Seconds to wait for a dev server to become ready.
    #[serde(default = "default_ready_timeout")]
    pub dev_ready_timeout_secs: u64,
    /// Seconds to wait for the registry watcher after a delete/generate.
    #[serde(default = "default_registry_timeout")]
    pub registry_timeout_secs: u64,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AuthConfig {
    /// Tailnet logins allowed on unix-socket listeners (`Tailscale-User-Login`).
    pub allowed_logins: Vec<String>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct WorkspaceConfig {
    pub id: String,
    pub name: String,
    pub root: PathBuf,
    /// Repositories (relative to `root`) exposed by the git API.
    #[serde(default)]
    pub repos: Vec<String>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ToolchainConfig {
    #[serde(default = "default_bun")]
    pub bun: PathBuf,
    #[serde(default = "default_git")]
    pub git: PathBuf,
    #[serde(default = "default_systemctl")]
    pub systemctl: PathBuf,
    #[serde(default = "default_journalctl")]
    pub journalctl: PathBuf,
    #[serde(default = "default_codex")]
    pub codex: PathBuf,
}

impl Default for ToolchainConfig {
    fn default() -> Self {
        Self {
            bun: default_bun(),
            git: default_git(),
            systemctl: default_systemctl(),
            journalctl: default_journalctl(),
            codex: default_codex(),
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SiteConfig {
    /// Folder name under `<root>/apps`.
    pub id: String,
    /// Public slot port (tailnet / tunnel), e.g. 9181.
    pub slot_port: u16,
    /// Loopback port of `nuxt dev`, e.g. 4401.
    pub dev_port: u16,
    pub dev_unit: String,
    pub agent_unit: String,
    /// Oneshot registry rebuild unit; defaults to `cf-registry@<id>.service`.
    #[serde(default)]
    pub registry_unit: Option<String>,
}

impl SiteConfig {
    pub fn registry_unit(&self) -> String {
        self.registry_unit.clone().unwrap_or_else(|| format!("cf-registry@{}.service", self.id))
    }
}

/// A unit run by the `process_group` runner.
#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct LocalUnit {
    pub argv: Vec<String>,
    pub cwd: PathBuf,
    #[serde(default)]
    pub env: BTreeMap<String, String>,
    /// Extra `KEY=VALUE` files read at start (like systemd `EnvironmentFile=`).
    #[serde(default)]
    pub env_files: Vec<PathBuf>,
    /// Append stdout here instead of the log stream (like `StandardOutput=append:`).
    #[serde(default)]
    pub stdout_append: Option<PathBuf>,
}

fn default_control_port() -> u16 {
    9180
}
fn default_runner() -> RunnerKind {
    RunnerKind::Systemd
}
fn default_true() -> bool {
    true
}
fn default_ready_timeout() -> u64 {
    180
}
fn default_registry_timeout() -> u64 {
    15
}
fn default_bun() -> PathBuf {
    PathBuf::from("/opt/couchfusion/toolchain/bin/bun")
}
fn default_git() -> PathBuf {
    PathBuf::from("/usr/bin/git")
}
fn default_systemctl() -> PathBuf {
    PathBuf::from("/usr/bin/systemctl")
}
fn default_journalctl() -> PathBuf {
    PathBuf::from("/usr/bin/journalctl")
}
fn default_codex() -> PathBuf {
    PathBuf::from("/opt/couchfusion/toolchain/bin/codex")
}

/// Site and unit names must match this (also enforced by the polkit rule).
pub fn valid_slug(value: &str) -> bool {
    let bytes = value.as_bytes();
    !bytes.is_empty()
        && bytes.len() <= 40
        && bytes[0].is_ascii_lowercase()
        && bytes.iter().all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || *b == b'-')
}

impl Config {
    pub fn load(path: &Path) -> anyhow::Result<Self> {
        let text = std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
        let config: Config = toml::from_str(&text).with_context(|| format!("parsing {}", path.display()))?;
        config.validate()?;
        Ok(config)
    }

    pub fn validate(&self) -> anyhow::Result<()> {
        if !self.workspace.root.is_absolute() {
            bail!("workspace.root must be absolute");
        }
        if self.auth.allowed_logins.is_empty() && self.server.unix_listeners {
            bail!("auth.allowed_logins must not be empty when unix listeners are enabled");
        }
        for site in &self.sites {
            if !valid_slug(&site.id) {
                bail!("invalid site id {:?}", site.id);
            }
            for (unit, prefix) in [(site.dev_unit.clone(), "cf-dev@"), (site.agent_unit.clone(), "cf-agent@"), (site.registry_unit(), "cf-registry@")] {
                if !unit.starts_with(prefix) || !unit.ends_with(".service") {
                    bail!("unit {unit:?} must be {prefix}<site>.service");
                }
            }
            if site.slot_port == self.server.control_port {
                bail!("slot port {} collides with the control port", site.slot_port);
            }
        }
        for unit in &self.services.units {
            if !valid_service_unit(unit) {
                bail!("invalid [services] unit {unit:?}");
            }
        }
        for repo in &self.workspace.repos {
            if repo.split('/').any(|seg| seg.is_empty() || seg == "." || seg == "..") {
                bail!("invalid repo path {repo:?}");
            }
        }
        Ok(())
    }

    pub fn site(&self, id: &str) -> Option<&SiteConfig> {
        self.sites.iter().find(|s| s.id == id)
    }

    pub fn site_by_slot(&self, port: u16) -> Option<&SiteConfig> {
        self.sites.iter().find(|s| s.slot_port == port)
    }

    pub fn app_dir(&self, site: &str) -> PathBuf {
        self.workspace.root.join("apps").join(site)
    }

    pub fn db_path(&self) -> PathBuf {
        self.server.state_dir.join("control.db")
    }

    pub fn trash_dir(&self) -> PathBuf {
        self.server.state_dir.join("trash")
    }

    pub fn agent_out(&self, site: &str) -> PathBuf {
        self.server.state_dir.join("agent-out").join(format!("{site}.jsonl"))
    }

    pub fn agent_spec(&self, site: &str) -> PathBuf {
        self.server.run_dir.join("agent").join(format!("{site}.json"))
    }

    pub fn dev_env_file(&self, site: &str) -> PathBuf {
        self.server.run_dir.join("dev").join(format!("{site}.env"))
    }

    /// Exists while a Codex generate job runs for `site`; the dev server (gated Vite plugin in
    /// layers/content) suppresses full reloads while it is present.
    pub fn agent_marker(&self, site: &str) -> PathBuf {
        self.server.run_dir.join("dev").join(format!("{site}.agent-running"))
    }

    pub fn socket_path(&self, port: u16) -> PathBuf {
        self.server.run_dir.join(format!("ts-{port}.sock"))
    }

    /// All ports the server listens on: control plus one per site slot.
    pub fn ports(&self) -> Vec<u16> {
        let mut ports = vec![self.server.control_port];
        ports.extend(self.sites.iter().map(|s| s.slot_port));
        ports
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn slug_rules() {
        assert!(valid_slug("radustanciu"));
        assert!(valid_slug("kids-store2"));
        assert!(!valid_slug(""));
        assert!(!valid_slug("Rad"));
        assert!(!valid_slug("-x"));
        assert!(!valid_slug("a/b"));
        assert!(!valid_slug("a..b."));
        assert!(!valid_slug(&"a".repeat(41)));
    }

    #[test]
    fn example_config_parses() {
        let text = include_str!("../../packaging/hetzner1/phase1/server.toml");
        let config: Config = toml::from_str(text).unwrap();
        config.validate().unwrap();
        assert_eq!(config.sites[0].id, "radustanciu");
        assert_eq!(config.ports(), vec![9180, 9181]);
        assert_eq!(config.sites[0].registry_unit(), "cf-registry@radustanciu.service");
        assert!(config.services.units.contains(&"kangu.service".to_string()));
    }

    #[test]
    fn service_unit_names() {
        assert!(valid_service_unit("kangu.service"));
        assert!(valid_service_unit("cf-dev@radustanciu.service"));
        assert!(!valid_service_unit("kangu"));
        assert!(!valid_service_unit("../x.service"));
        assert!(!valid_service_unit("a b.service"));
        assert!(!valid_service_unit("*.service"));
        assert!(!valid_service_unit("-x.service"));
    }
}
