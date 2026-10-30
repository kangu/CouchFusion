//! `couchfusion-server`: CouchFusion control plane (Phase 1 MVP).
//!
//! Subcommands:
//! * `serve`        — control plane + dev slot proxies (runs as `cfcp` under couchfusion.service)
//! * `login-url`    — prints a single-use tunnel login link (run as `cfcp`)
//! * `agent-runner` — runs `codex exec` for one app (runs as `cfagent` under cf-agent@.service)
//! * `doctor`       — checks the installation

mod agent;
mod api;
mod config;
mod dev;
mod dto;
mod error;
mod events;
mod git;
mod http;
mod jobs;
mod ops;
mod proxy;
mod services;
mod store;
mod units;
mod util;
mod workspace;

use std::path::PathBuf;
use std::process::ExitCode;

use clap::{Parser, Subcommand};

const DEFAULT_CONFIG: &str = "/etc/couchfusion/server.toml";

#[derive(Parser)]
#[command(name = "couchfusion-server", version, about = "CouchFusion control plane")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Run the control plane and dev slot proxies.
    Serve {
        #[arg(long, default_value = DEFAULT_CONFIG)]
        config: PathBuf,
    },
    /// Print a single-use login link for the SSH tunnel (valid 10 minutes).
    LoginUrl {
        #[arg(long, default_value = DEFAULT_CONFIG)]
        config: PathBuf,
    },
    /// Run codex for one app from its run spec (used by cf-agent@.service).
    AgentRunner {
        #[arg(long)]
        app: String,
        #[arg(long, default_value = "/run/couchfusion/agent")]
        spec_dir: PathBuf,
        #[arg(long, default_value = "/srv/couchfusion/workspace")]
        workspace: PathBuf,
        #[arg(long, default_value = "/opt/couchfusion/toolchain/bin/codex")]
        codex: PathBuf,
    },
    /// Check configuration, paths and tools.
    Doctor {
        #[arg(long, default_value = DEFAULT_CONFIG)]
        config: PathBuf,
    },
}

fn main() -> ExitCode {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()))
        .with_target(false)
        .with_writer(std::io::stderr)
        .init();
    let cli = Cli::parse();
    let result = match cli.command {
        Command::Serve { config } => serve(config),
        Command::LoginUrl { config } => login_url(config),
        Command::AgentRunner { app, spec_dir, workspace, codex } => {
            return match agent::run_agent(&agent::RunnerArgs { app, spec_dir, workspace, codex }) {
                Ok(code) => ExitCode::from(code.clamp(0, 255) as u8),
                Err(err) => {
                    eprintln!("agent-runner: {err:#}");
                    ExitCode::from(2)
                }
            };
        }
        Command::Doctor { config } => doctor(config),
    };
    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("error: {err:#}");
            ExitCode::FAILURE
        }
    }
}

fn serve(path: PathBuf) -> anyhow::Result<()> {
    let config = config::Config::load(&path)?;
    let runtime = tokio::runtime::Builder::new_multi_thread().enable_all().build()?;
    runtime.block_on(async move {
        let app = http::App::new(config)?;
        tracing::info!("couchfusion-server {} starting", env!("CARGO_PKG_VERSION"));
        http::serve(app).await
    })
}

fn login_url(path: PathBuf) -> anyhow::Result<()> {
    let config = config::Config::load(&path)?;
    let store = store::Store::open(&config.db_path())?;
    let token = util::random_token();
    store.create_login_token(&token)?;
    println!("http://localhost:{}/auth/exchange?token={token}", config.server.control_port);
    eprintln!("(single use, valid for 10 minutes; open it through: ssh -L {0}:127.0.0.1:{0} -L 9181:127.0.0.1:9181 hetzner1)", config.server.control_port);
    Ok(())
}

fn doctor(path: PathBuf) -> anyhow::Result<()> {
    use std::os::unix::fs::MetadataExt;
    let config = config::Config::load(&path)?;
    let mut failures = 0;
    let mut check = |ok: bool, what: &str| {
        println!("{} {what}", if ok { "ok  " } else { "FAIL" });
        if !ok {
            failures += 1;
        }
    };
    check(true, &format!("config {}", path.display()));
    check(config.server.spa_dir.join("index.html").is_file(), &format!("SPA at {}", config.server.spa_dir.display()));
    check(store::Store::open(&config.db_path()).is_ok(), &format!("state db {}", config.db_path().display()));
    check(config.workspace.root.join("apps").is_dir(), &format!("workspace {}", config.workspace.root.display()));
    for (name, p) in [("bun", &config.toolchain.bun), ("git", &config.toolchain.git), ("codex", &config.toolchain.codex)] {
        check(p.exists(), &format!("{name} at {}", p.display()));
    }
    if config.server.runner == config::RunnerKind::Systemd {
        check(config.toolchain.systemctl.exists(), "systemctl");
        check(config.toolchain.journalctl.exists(), "journalctl");
    }
    let trash = config.trash_dir();
    std::fs::create_dir_all(&trash).ok();
    let same_fs = match (std::fs::metadata(&trash), std::fs::metadata(&config.workspace.root)) {
        (Ok(a), Ok(b)) => a.dev() == b.dev(),
        _ => false,
    };
    check(same_fs, "trash and workspace on the same filesystem (rename-based delete)");
    for site in &config.sites {
        let app = config.app_dir(&site.id);
        let reg = workspace::inspect_registry(&app);
        check(reg.usable(), &format!("{}: component registry {} ({} components)", site.id, reg.status, reg.components.len()));
        check(!app.join(".env").exists(), &format!("{}: no .env in the workspace", site.id));
    }
    if failures > 0 {
        anyhow::bail!("{failures} check(s) failed");
    }
    Ok(())
}
