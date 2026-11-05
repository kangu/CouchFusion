//! Read-only service status and recent logs for an allowlist of systemd units (Phase 2 §1–§2).
//!
//! Always shells out to the configured `systemctl`/`journalctl` (argv only, never a shell),
//! independent of the dev unit runner, so the same code serves hetzner1 and test fakes.
//! Nothing here starts, stops or restarts a unit; production deploys live in `release.rs`.

use std::path::PathBuf;
use std::process::Stdio;

use tokio::process::Command;

use crate::config::Config;
use crate::dto::{ActivityEvent, ServiceSnapshot};
use crate::units::parse_journal_line;
use crate::util::Redactor;

const SHOW_PROPS: &str = "Id,Description,ActiveState,SubState,MainPID,MemoryCurrent,ActiveEnterTimestamp,User,UnitFileState";
const SYSTEM_UNITS: &[&str] = &["caddy.service", "couchdb.service", "couchfusion.service", "tailscaled.service", "polkit.service", "ssh.service"];

pub struct Services {
    systemctl: PathBuf,
    journalctl: PathBuf,
    units: Vec<String>,
}

impl Services {
    pub fn new(config: &Config) -> Self {
        Self {
            systemctl: config.toolchain.systemctl.clone(),
            journalctl: config.toolchain.journalctl.clone(),
            units: config.services.units.clone(),
        }
    }

    pub fn allowed(&self, unit: &str) -> bool {
        self.units.iter().any(|u| u == unit)
    }

    pub async fn list(&self) -> Vec<ServiceSnapshot> {
        let futures = self.units.iter().map(|u| self.show(u));
        futures_util::future::join_all(futures).await
    }

    /// Status of one unit (not limited to the allowlist; callers pass configured units only).
    pub async fn show(&self, unit: &str) -> ServiceSnapshot {
        let out = Command::new(&self.systemctl)
            .args(["show", unit, "-p", SHOW_PROPS, "--timestamp=unix"])
            .stdin(Stdio::null())
            .output()
            .await;
        let text = out.map(|o| String::from_utf8_lossy(&o.stdout).into_owned()).unwrap_or_default();
        parse_service(unit, &text)
    }

    /// Last `lines` journal entries of an allowlisted unit, oldest first, redacted.
    pub async fn logs(&self, unit: &str, lines: u32) -> anyhow::Result<Vec<ActivityEvent>> {
        let out = Command::new(&self.journalctl)
            .args(["-u", unit, "-n", &lines.to_string(), "-o", "json", "--no-pager"])
            .stdin(Stdio::null())
            .output()
            .await?;
        let redactor = Redactor::new(vec![]);
        Ok(String::from_utf8_lossy(&out.stdout)
            .lines()
            .filter_map(|line| {
                let log = parse_journal_line(line)?;
                let time = serde_json::from_str::<serde_json::Value>(line)
                    .ok()
                    .and_then(|v| v["__REALTIME_TIMESTAMP"].as_str().and_then(|t| t.parse::<i64>().ok()))
                    .map(micros_to_rfc3339)
                    .unwrap_or_default();
                Some((log, time))
            })
            .enumerate()
            .map(|(i, (log, time))| ActivityEvent {
                id: i as u64 + 1,
                time,
                code: "journal".into(),
                text: redactor.redact(&log.message),
                level: None,
                stream: Some(if log.priority <= 3 { "stderr".into() } else { "stdout".into() }),
            })
            .collect())
    }
}

fn micros_to_rfc3339(micros: i64) -> String {
    time::OffsetDateTime::from_unix_timestamp_nanos(i128::from(micros) * 1000)
        .map(crate::util::format_time)
        .unwrap_or_default()
}

fn kind_of(unit: &str) -> &'static str {
    if unit.starts_with("cf-site@") || unit.starts_with("cf-build@") {
        "prod"
    } else if unit.starts_with("cf-") {
        "dev"
    } else if SYSTEM_UNITS.contains(&unit) {
        "system"
    } else {
        "adopted"
    }
}

/// Parses `systemctl show -p … --timestamp=unix` output.
pub fn parse_service(unit: &str, text: &str) -> ServiceSnapshot {
    let mut s = ServiceSnapshot {
        unit: unit.to_string(),
        description: String::new(),
        kind: kind_of(unit),
        active_state: "unknown".into(),
        sub_state: "unknown".into(),
        main_pid: None,
        memory_current_bytes: None,
        since: None,
        user: None,
        starts_at_boot: None,
    };
    for line in text.lines() {
        let Some((k, v)) = line.split_once('=') else { continue };
        match k {
            "Description" => s.description = v.to_string(),
            "ActiveState" if !v.is_empty() => s.active_state = v.to_string(),
            "SubState" if !v.is_empty() => s.sub_state = v.to_string(),
            "MainPID" => s.main_pid = v.parse().ok().filter(|p| *p > 0),
            "MemoryCurrent" => s.memory_current_bytes = v.parse().ok(),
            // enabled / enabled-runtime start at boot; disabled, static, indirect, masked do not.
            "UnitFileState" if !v.is_empty() => s.starts_at_boot = Some(v.starts_with("enabled")),
            "ActiveEnterTimestamp" => {
                s.since = v
                    .strip_prefix('@')
                    .and_then(|t| t.parse::<i64>().ok())
                    .filter(|t| *t > 0)
                    .and_then(|t| time::OffsetDateTime::from_unix_timestamp(t).ok())
                    .map(crate::util::format_time);
            }
            "User" if !v.is_empty() => s.user = Some(v.to_string()),
            _ => {}
        }
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_show_output() {
        let s = parse_service(
            "kangu.service",
            "Id=kangu.service\nDescription=manual deployment\nActiveState=active\nSubState=running\nMainPID=4097570\nMemoryCurrent=104857600\nActiveEnterTimestamp=@1793300000\nUser=radu\n",
        );
        assert_eq!(s.kind, "adopted");
        assert_eq!(s.active_state, "active");
        assert_eq!(s.sub_state, "running");
        assert_eq!(s.main_pid, Some(4097570));
        assert_eq!(s.memory_current_bytes, Some(104857600));
        assert_eq!(s.since.as_deref(), Some("2026-10-29T18:53:20Z"));
        assert_eq!(s.user.as_deref(), Some("radu"));
        assert_eq!(s.starts_at_boot, None);
        assert_eq!(parse_service("cf-site@x.service", "UnitFileState=enabled\n").starts_at_boot, Some(true));
        assert_eq!(parse_service("cf-dev@x.service", "UnitFileState=disabled\n").starts_at_boot, Some(false));
    }

    #[test]
    fn unknown_or_inactive_units() {
        let s = parse_service("couchdb.service", "ActiveState=inactive\nSubState=dead\nMainPID=0\nMemoryCurrent=[not set]\nActiveEnterTimestamp=\nUser=\n");
        assert_eq!(s.kind, "system");
        assert_eq!(s.main_pid, None);
        assert_eq!(s.memory_current_bytes, None);
        assert_eq!(s.since, None);
        assert_eq!(s.user, None);
        let s = parse_service("cf-dev@x.service", "");
        assert_eq!((s.kind, s.active_state.as_str()), ("dev", "unknown"));
    }
}
