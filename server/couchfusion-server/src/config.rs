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
    /// Remotes the control plane may fetch from, pull from and push to.
    #[serde(default)]
    pub git: GitConfig,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct GitConfig {
    #[serde(default)]
    pub remotes: Vec<GitRemoteConfig>,
    /// Where credential files live. Defaults to `$CREDENTIALS_DIRECTORY` (systemd `LoadCredential=`).
    #[serde(default)]
    pub credentials_dir: Option<PathBuf>,
    /// Allow `file://` remote URLs (local tests only).
    #[serde(default)]
    pub allow_file_remotes: bool,
}

/// One remote of one workspace repository.
#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct GitRemoteConfig {
    /// Workspace-relative repo path, e.g. `apps/radustanciu` (must be in `workspace.repos`).
    pub repo: String,
    #[serde(default = "default_remote_name")]
    pub remote: String,
    /// Expected `git remote get-url <remote>`; the credential is only used if it matches.
    pub url: String,
    /// File name inside the credentials directory (git credential format: `username=…`, `password=…`).
    #[serde(default)]
    pub credential: Option<String>,
    /// Branches that may be pushed (never forced).
    #[serde(default)]
    pub branches: Vec<String>,
}

fn default_remote_name() -> String {
    "origin".to_string()
}

impl GitRemoteConfig {
    /// `scheme://host[:port]` of the URL, used to scope the credential helper.
    pub fn url_origin(&self) -> Option<String> {
        let (scheme, rest) = self.url.split_once("://")?;
        let host = rest.split('/').next()?;
        (!host.is_empty()).then(|| format!("{scheme}://{host}"))
    }
}

/// Branch names accepted for push: conservative subset of git's ref-name rules.
pub fn valid_branch_name(name: &str) -> bool {
    !name.is_empty()
        && name.len() <= 100
        && !name.starts_with(['-', '/', '.'])
        && !name.ends_with(['/', '.'])
        && !name.contains("..")
        && !name.contains("//")
        && !name.ends_with(".lock")
        && name.chars().all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | '.' | '/'))
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
    /// Production releases (Phase 3). Absent = no deploy/rollback for this site.
    #[serde(default)]
    pub prod: Option<ProdConfig>,
}

/// Production site served from `<www_dir>/current` by `unit`, built by `build_unit` (spec §7.2).
#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ProdConfig {
    /// `cf-site@<id>.service`.
    pub unit: String,
    /// Oneshot `cf-build@<id>.service`; builds `<build_dir>/next` as cfdev.
    pub build_unit: String,
    /// Loopback port the production server listens on (health checks).
    pub port: u16,
    /// Holds `releases/` and the `current` symlink (owned by cfcp).
    pub www_dir: PathBuf,
    /// Snapshots of the workspace for builds (cfcp writes, cfdev builds).
    pub build_dir: PathBuf,
    #[serde(default = "default_keep_releases")]
    pub keep_releases: usize,
    #[serde(default = "default_keep_snapshots")]
    pub keep_snapshots: usize,
    #[serde(default = "default_build_timeout")]
    pub build_timeout_secs: u64,
    /// Admission: free memory (MemAvailable) needed to start a build.
    #[serde(default = "default_min_memory")]
    pub min_memory_mib: u64,
    /// Admission: free disk on `build_dir` needed to start a build.
    #[serde(default = "default_min_disk")]
    pub min_disk_gib: u64,
    /// Path probed for the health check (3 consecutive answers below 500).
    #[serde(default = "default_probe_path")]
    pub probe_path: String,
}

fn default_keep_releases() -> usize {
    5
}
fn default_keep_snapshots() -> usize {
    2
}
fn default_build_timeout() -> u64 {
    1200
}
fn default_min_memory() -> u64 {
    1229
}
fn default_min_disk() -> u64 {
    3
}
fn default_probe_path() -> String {
    "/".to_string()
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
            if let Some(prod) = &site.prod {
                for (unit, prefix) in [(&prod.unit, "cf-site@"), (&prod.build_unit, "cf-build@")] {
                    if unit != &format!("{prefix}{}.service", site.id) {
                        bail!("prod unit {unit:?} must be {prefix}{}.service", site.id);
                    }
                }
                if [self.server.control_port, site.slot_port, site.dev_port].contains(&prod.port) {
                    bail!("prod port {} collides with the control, slot or dev port", prod.port);
                }
                if !prod.www_dir.is_absolute() || !prod.build_dir.is_absolute() {
                    bail!("prod www_dir and build_dir must be absolute");
                }
                if prod.keep_releases < 2 || prod.keep_snapshots < 1 {
                    bail!("prod keep_releases must be ≥ 2 and keep_snapshots ≥ 1");
                }
                if !prod.probe_path.starts_with('/') {
                    bail!("prod probe_path must start with /");
                }
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
        for remote in &self.git.remotes {
            if !self.workspace.repos.contains(&remote.repo) {
                bail!("[[git.remotes]] repo {:?} is not in workspace.repos", remote.repo);
            }
            if !valid_slug(&remote.remote) {
                bail!("invalid git remote name {:?}", remote.remote);
            }
            let https = remote.url.starts_with("https://");
            let file = remote.url.starts_with("file://") && self.git.allow_file_remotes;
            if !(https || file) {
                bail!("git remote url {:?} must be https:// (file:// only with allow_file_remotes)", remote.url);
            }
            if remote.url.split("://").nth(1).is_some_and(|rest| rest.split('/').next().unwrap_or("").contains('@')) {
                bail!("git remote url {:?} must not embed credentials; use `credential`", remote.url);
            }
            if remote.url_origin().is_none() && https {
                bail!("git remote url {:?} has no host", remote.url);
            }
            if let Some(cred) = &remote.credential
                && !valid_slug(cred)
            {
                bail!("invalid git credential name {cred:?}");
            }
            if let Some(b) = remote.branches.iter().find(|b| !valid_branch_name(b)) {
                bail!("invalid push branch {b:?}");
            }
        }
        if self.git.remotes.iter().map(|r| &r.repo).collect::<std::collections::BTreeSet<_>>().len() != self.git.remotes.len() {
            bail!("at most one [[git.remotes]] entry per repo");
        }
        Ok(())
    }

    pub fn git_remote(&self, repo: &str) -> Option<&GitRemoteConfig> {
        self.git.remotes.iter().find(|r| r.repo == repo)
    }

    /// Absolute path of a credential file, from `[git] credentials_dir` or `$CREDENTIALS_DIRECTORY`.
    pub fn credential_path(&self, name: &str) -> Option<PathBuf> {
        let dir = self.git.credentials_dir.clone().or_else(|| std::env::var_os("CREDENTIALS_DIRECTORY").map(PathBuf::from))?;
        Some(dir.join(name))
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

    fn example() -> Config {
        toml::from_str(include_str!("../../packaging/hetzner1/phase1/server.toml")).unwrap()
    }

    fn remote(url: &str) -> GitRemoteConfig {
        GitRemoteConfig {
            repo: "apps/radustanciu".into(),
            remote: "origin".into(),
            url: url.into(),
            credential: Some("git-apps-radustanciu".into()),
            branches: vec!["main".into()],
        }
    }

    #[test]
    fn git_remote_validation() {
        let mut c = example();
        c.git.remotes = vec![remote("https://git2.kangu.ro/radu/radustanciu-src.git")];
        c.validate().unwrap();
        assert_eq!(c.git.remotes[0].url_origin().as_deref(), Some("https://git2.kangu.ro"));

        for bad in ["http://git2.kangu.ro/x.git", "https://radu:tok@git2.kangu.ro/x.git", "ssh://git@host/x.git", "file:///tmp/x.git"] {
            let mut c = example();
            c.git.remotes = vec![remote(bad)];
            assert!(c.validate().is_err(), "{bad} must be refused");
        }
        let mut c = example();
        c.git.allow_file_remotes = true;
        let mut r = remote("file:///tmp/x.git");
        r.credential = None;
        c.git.remotes = vec![r];
        c.validate().unwrap();

        let mut c = example();
        let mut r = remote("https://git2.kangu.ro/x.git");
        r.repo = "apps/other".into();
        c.git.remotes = vec![r];
        assert!(c.validate().is_err(), "repo must be in workspace.repos");

        let mut c = example();
        let mut r = remote("https://git2.kangu.ro/x.git");
        r.branches = vec!["main; rm -rf".into()];
        c.git.remotes = vec![r];
        assert!(c.validate().is_err());
    }

    #[test]
    fn branch_names() {
        for ok in ["main", "feat/x", "release-1.2", "a_b"] {
            assert!(valid_branch_name(ok), "{ok}");
        }
        for bad in ["", "-x", "/x", "x/", "a..b", "x.lock", "a b", "a//b", ".hidden", "x."] {
            assert!(!valid_branch_name(bad), "{bad}");
        }
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
