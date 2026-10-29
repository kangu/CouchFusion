//! Unit runner: `systemd` (systemctl/journalctl via argv, never a shell) on hetzner1, and
//! `process_group` (child processes in their own group) for macOS and tests.

use std::collections::{BTreeMap, HashMap};
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use anyhow::{Context, bail};
use tokio::io::{AsyncBufReadExt, AsyncRead, BufReader};
use tokio::process::Command;
use tokio::sync::{broadcast, mpsc};

use crate::config::{Config, LocalUnit, RunnerKind};

/// Current state of a unit, normalised across backends.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct UnitState {
    /// systemd ActiveState: active | activating | deactivating | inactive | failed.
    pub active: String,
    pub main_pid: Option<u32>,
    pub memory: Option<u64>,
    /// systemd Result (success, exit-code, oom-kill, …).
    pub result: String,
    pub exit_status: Option<i32>,
}

impl UnitState {
    pub fn running(&self) -> bool {
        matches!(self.active.as_str(), "active" | "activating" | "reloading")
    }
}

#[derive(Debug, Clone)]
pub struct LogLine {
    /// syslog priority (0–7); ≤ 3 is shown as stderr.
    pub priority: u8,
    pub message: String,
}

struct LocalProc {
    pgid: i32,
    logs: broadcast::Sender<LogLine>,
    exit: Arc<Mutex<Option<i32>>>,
}

pub struct Units {
    kind: RunnerKind,
    systemctl: PathBuf,
    journalctl: PathBuf,
    local_defs: BTreeMap<String, LocalUnit>,
    local: Mutex<HashMap<String, LocalProc>>,
}

impl Units {
    pub fn new(config: &Config) -> Arc<Self> {
        Arc::new(Self {
            kind: config.server.runner,
            systemctl: config.toolchain.systemctl.clone(),
            journalctl: config.toolchain.journalctl.clone(),
            local_defs: config.process_group.clone(),
            local: Mutex::new(HashMap::new()),
        })
    }

    pub async fn start(&self, unit: &str) -> anyhow::Result<()> {
        check_unit_name(unit)?;
        match self.kind {
            RunnerKind::Systemd => self.systemctl(&["start", unit]).await,
            RunnerKind::ProcessGroup => self.local_start(unit),
        }
    }

    pub async fn stop(&self, unit: &str) -> anyhow::Result<()> {
        check_unit_name(unit)?;
        match self.kind {
            RunnerKind::Systemd => self.systemctl(&["stop", unit]).await,
            RunnerKind::ProcessGroup => self.local_stop(unit).await,
        }
    }

    pub async fn restart(&self, unit: &str) -> anyhow::Result<()> {
        check_unit_name(unit)?;
        match self.kind {
            RunnerKind::Systemd => self.systemctl(&["restart", unit]).await,
            RunnerKind::ProcessGroup => {
                self.local_stop(unit).await?;
                self.local_start(unit)
            }
        }
    }

    pub async fn state(&self, unit: &str) -> anyhow::Result<UnitState> {
        check_unit_name(unit)?;
        match self.kind {
            RunnerKind::Systemd => {
                let out = Command::new(&self.systemctl)
                    .args(["show", unit, "-p", "ActiveState,MainPID,MemoryCurrent,Result,ExecMainStatus"])
                    .stdin(Stdio::null())
                    .output()
                    .await
                    .context("running systemctl show")?;
                Ok(parse_show(&String::from_utf8_lossy(&out.stdout)))
            }
            RunnerKind::ProcessGroup => Ok(self.local_state(unit)),
        }
    }

    /// Streams new log lines of a unit until the receiver is dropped.
    pub fn follow_logs(self: &Arc<Self>, unit: &str) -> mpsc::Receiver<LogLine> {
        let (tx, rx) = mpsc::channel(512);
        let unit = unit.to_string();
        let this = self.clone();
        tokio::spawn(async move {
            match this.kind {
                RunnerKind::Systemd => {
                    let child = Command::new(&this.journalctl)
                        .args(["-u", &unit, "-o", "json", "-f", "-n", "30", "--no-pager"])
                        .stdin(Stdio::null())
                        .stdout(Stdio::piped())
                        .stderr(Stdio::null())
                        .kill_on_drop(true)
                        .spawn();
                    let Ok(mut child) = child else { return };
                    let Some(stdout) = child.stdout.take() else { return };
                    let mut lines = BufReader::new(stdout).lines();
                    while let Ok(Some(line)) = lines.next_line().await {
                        if let Some(log) = parse_journal_line(&line)
                            && tx.send(log).await.is_err()
                        {
                            break;
                        }
                    }
                }
                RunnerKind::ProcessGroup => loop {
                    let sub = this.local.lock().expect("local lock").get(&unit).map(|p| p.logs.subscribe());
                    let Some(mut rx) = sub else {
                        tokio::time::sleep(Duration::from_millis(500)).await;
                        if tx.is_closed() {
                            return;
                        }
                        continue;
                    };
                    loop {
                        match rx.recv().await {
                            Ok(line) => {
                                if tx.send(line).await.is_err() {
                                    return;
                                }
                            }
                            Err(broadcast::error::RecvError::Lagged(_)) => continue,
                            Err(broadcast::error::RecvError::Closed) => break,
                        }
                    }
                },
            }
        });
        rx
    }

    async fn systemctl(&self, args: &[&str]) -> anyhow::Result<()> {
        let out = Command::new(&self.systemctl)
            .args(args)
            .stdin(Stdio::null())
            .output()
            .await
            .context("running systemctl")?;
        if !out.status.success() {
            bail!("systemctl {} failed: {}", args.join(" "), String::from_utf8_lossy(&out.stderr).trim());
        }
        Ok(())
    }

    fn local_start(&self, unit: &str) -> anyhow::Result<()> {
        if self.local_state(unit).running() {
            return Ok(());
        }
        let def = self.local_defs.get(unit).with_context(|| format!("no process_group definition for {unit}"))?;
        let (program, args) = def.argv.split_first().context("empty argv")?;
        let mut cmd = Command::new(program);
        cmd.args(args).current_dir(&def.cwd).stdin(Stdio::null()).stderr(Stdio::piped()).process_group(0);
        for file in &def.env_files {
            if let Ok(text) = std::fs::read_to_string(file) {
                for (k, v) in parse_env_file(&text) {
                    cmd.env(k, v);
                }
            }
        }
        cmd.envs(&def.env);
        match &def.stdout_append {
            Some(path) => {
                let file = std::fs::OpenOptions::new().create(true).append(true).open(path)?;
                cmd.stdout(Stdio::from(file));
            }
            None => {
                cmd.stdout(Stdio::piped());
            }
        }
        let mut child = cmd.spawn().with_context(|| format!("spawning {unit}"))?;
        let pgid = child.id().context("child pid")? as i32;
        let (logs, _) = broadcast::channel(512);
        if let Some(out) = child.stdout.take() {
            pump(out, 6, logs.clone());
        }
        if let Some(err) = child.stderr.take() {
            pump(err, 3, logs.clone());
        }
        let exit = Arc::new(Mutex::new(None));
        let exit_clone = exit.clone();
        tokio::spawn(async move {
            let code = child.wait().await.ok().and_then(|s| s.code()).unwrap_or(-1);
            *exit_clone.lock().expect("exit lock") = Some(code);
        });
        self.local.lock().expect("local lock").insert(unit.to_string(), LocalProc { pgid, logs, exit });
        Ok(())
    }

    async fn local_stop(&self, unit: &str) -> anyhow::Result<()> {
        let target = self.local.lock().expect("local lock").get(unit).map(|p| (p.pgid, p.exit.clone()));
        let Some((pgid, exit)) = target else { return Ok(()) };
        let pg = nix::unistd::Pid::from_raw(pgid);
        let _ = nix::sys::signal::killpg(pg, nix::sys::signal::Signal::SIGINT);
        for _ in 0..50 {
            if exit.lock().expect("exit lock").is_some() {
                return Ok(());
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
        let _ = nix::sys::signal::killpg(pg, nix::sys::signal::Signal::SIGKILL);
        Ok(())
    }

    fn local_state(&self, unit: &str) -> UnitState {
        let local = self.local.lock().expect("local lock");
        match local.get(unit) {
            None => UnitState { active: "inactive".into(), ..Default::default() },
            Some(p) => match *p.exit.lock().expect("exit lock") {
                None => UnitState { active: "active".into(), main_pid: Some(p.pgid as u32), result: "success".into(), ..Default::default() },
                Some(code) => UnitState {
                    active: if code == 0 { "inactive".into() } else { "failed".into() },
                    result: if code == 0 { "success".into() } else { "exit-code".into() },
                    exit_status: Some(code),
                    ..Default::default()
                },
            },
        }
    }
}

/// Drains a pipe in chunks; long lines are cut at 8 KiB instead of stalling.
fn pump(reader: impl AsyncRead + Unpin + Send + 'static, priority: u8, tx: broadcast::Sender<LogLine>) {
    tokio::spawn(async move {
        let mut reader = BufReader::new(reader);
        let mut buf = Vec::new();
        loop {
            buf.clear();
            match reader.read_until(b'\n', &mut buf).await {
                Ok(0) | Err(_) => break,
                Ok(_) => {
                    buf.truncate(8192);
                    let message = String::from_utf8_lossy(&buf).trim_end().to_string();
                    let _ = tx.send(LogLine { priority, message });
                }
            }
        }
    });
}

fn check_unit_name(unit: &str) -> anyhow::Result<()> {
    let ok = ["cf-dev@", "cf-agent@"].iter().any(|p| {
        unit.strip_prefix(p)
            .and_then(|rest| rest.strip_suffix(".service"))
            .is_some_and(crate::config::valid_slug)
    });
    if !ok {
        bail!("refusing to manage unit {unit:?}");
    }
    Ok(())
}

pub fn parse_show(text: &str) -> UnitState {
    let mut state = UnitState::default();
    for line in text.lines() {
        let Some((k, v)) = line.split_once('=') else { continue };
        match k {
            "ActiveState" => state.active = v.to_string(),
            "MainPID" => state.main_pid = v.parse().ok().filter(|p| *p > 0),
            "MemoryCurrent" => state.memory = v.parse().ok(),
            "Result" => state.result = v.to_string(),
            "ExecMainStatus" => state.exit_status = v.parse().ok(),
            _ => {}
        }
    }
    state
}

pub fn parse_journal_line(line: &str) -> Option<LogLine> {
    let value: serde_json::Value = serde_json::from_str(line).ok()?;
    let message = match &value["MESSAGE"] {
        serde_json::Value::String(s) => s.clone(),
        // journald encodes non-UTF-8 messages as byte arrays.
        serde_json::Value::Array(bytes) => {
            String::from_utf8_lossy(&bytes.iter().filter_map(|b| b.as_u64().map(|v| v as u8)).collect::<Vec<_>>()).into_owned()
        }
        _ => return None,
    };
    let priority = value["PRIORITY"].as_str().and_then(|p| p.parse().ok()).unwrap_or(6);
    Some(LogLine { priority, message })
}

pub fn parse_env_file(text: &str) -> Vec<(String, String)> {
    text.lines()
        .map(str::trim)
        .filter(|l| !l.is_empty() && !l.starts_with('#'))
        .filter_map(|l| l.split_once('='))
        .map(|(k, v)| (k.trim().to_string(), v.trim().trim_matches('"').to_string()))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unit_names_are_allowlisted() {
        assert!(check_unit_name("cf-dev@radustanciu.service").is_ok());
        assert!(check_unit_name("cf-agent@radustanciu.service").is_ok());
        assert!(check_unit_name("ssh.service").is_err());
        assert!(check_unit_name("cf-dev@../x.service").is_err());
        assert!(check_unit_name("cf-dev@Rad.service").is_err());
    }

    #[test]
    fn parses_systemctl_show() {
        let s = parse_show("ActiveState=active\nMainPID=123\nMemoryCurrent=[not set]\nResult=success\nExecMainStatus=0\n");
        assert!(s.running());
        assert_eq!(s.main_pid, Some(123));
        assert_eq!(s.memory, None);
        let s = parse_show("ActiveState=failed\nMainPID=0\nMemoryCurrent=4096\nResult=oom-kill\n");
        assert!(!s.running());
        assert_eq!(s.main_pid, None);
        assert_eq!(s.memory, Some(4096));
        assert_eq!(s.result, "oom-kill");
    }

    #[test]
    fn parses_journal_json() {
        let l = parse_journal_line(r#"{"MESSAGE":"hello","PRIORITY":"3"}"#).unwrap();
        assert_eq!((l.priority, l.message.as_str()), (3, "hello"));
        let l = parse_journal_line(r#"{"MESSAGE":[104,105]}"#).unwrap();
        assert_eq!(l.message, "hi");
        assert!(parse_journal_line("not json").is_none());
    }

    #[test]
    fn env_file_parsing() {
        let v = parse_env_file("# c\nA=1\n B = \"two\" \nbad\n");
        assert_eq!(v, vec![("A".into(), "1".into()), ("B".into(), "two".into())]);
    }
}
