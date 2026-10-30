//! Codex integration (Phase 1: `codex exec --json`, no approvals, sandboxed).
//!
//! * `agent-runner` runs inside `cf-agent@<app>.service` as `cfagent`: it reads the run spec,
//!   builds the prompt and runs codex with the prompt on stdin, then closes stdin. Its stdout is
//!   the unit's `StandardOutput=append:` file, which the server tails.
//! * `map_event` turns codex JSONL events into job events (raw commands are never shown).

use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

use anyhow::{Context, bail};
use serde::{Deserialize, Serialize};
use serde_json::Value;

const SKILL: &str = include_str!("../../../internal/agent/prompt/skill/couchfusion-component-builder/SKILL.md");

/// Verbatim from `internal/agent/prompt/prompt.go` `DeveloperInstructions()`, plus the VPS line.
pub const DEVELOPER_INSTRUCTIONS: &str = "You are generating or updating CouchFusion Content components. Modify files only within the selected application root. Preserve all pre-existing uncommitted work and avoid unrelated formatting or rewrites. Do not stage, commit, branch, tag, reset, clean, restore, or otherwise change Git history or index state. Dependency manifest or lockfile changes require explicit consent for this generation before editing or running a mutating package command. Network access requires a separate explicit approval. Inspect applicable repository instructions and established Content component, builder metadata, style, test, and registry conventions. Run focused validation using installed project tooling. Update source component metadata and use the canonical registry generator; do not hand-edit generated registry TypeScript or JSON. Report relative changed files and validation results, then leave review and commit to the user.";

pub const VPS_LINE: &str = "The dev server is managed by CouchFusion; never start nuxt dev or bun run dev.";

/// Unattended-run line: repository instructions that ask the user questions must not block.
pub const UNATTENDED_LINE: &str = "This is a non-interactive run: do not ask questions. Where repository instructions say to stop and ask, the answers are: the target is this application; the component is a dynamic content-builder component (props and slots exposed); use the existing styling conventions; mark user-facing copy as localized where i18n is enabled; add no dependencies.";

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RunSpec {
    pub job_id: String,
    pub prompt: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub thread_id: Option<String>,
}

pub fn build_prompt(user_prompt: &str) -> String {
    format!(
        "{DEVELOPER_INSTRUCTIONS}\n{VPS_LINE}\n{UNATTENDED_LINE}\n\nFollow this first-party skill:\n\n{SKILL}\n\n---\n\nComponent request:\n\n{}\n",
        user_prompt.trim()
    )
}

pub struct RunnerArgs {
    pub app: String,
    pub spec_dir: PathBuf,
    pub workspace: PathBuf,
    pub codex: PathBuf,
}

/// `couchfusion-server agent-runner --app <app>`: never returns on success paths other than via
/// the codex exit code.
pub fn run_agent(args: &RunnerArgs) -> anyhow::Result<i32> {
    if !crate::config::valid_slug(&args.app) {
        bail!("invalid app {:?}", args.app);
    }
    let spec_path = args.spec_dir.join(format!("{}.json", args.app));
    let spec: RunSpec = serde_json::from_str(&std::fs::read_to_string(&spec_path).with_context(|| format!("reading {}", spec_path.display()))?)
        .context("parsing run spec")?;
    let trimmed = spec.prompt.trim();
    if trimmed.is_empty() || trimmed.len() > 16384 {
        bail!("prompt must be 1–16384 bytes");
    }
    let app_dir = args.workspace.join("apps").join(&args.app);
    let mut cmd = Command::new(&args.codex);
    cmd.arg("exec");
    cmd.args(["--json", "--skip-git-repo-check", "-C"]).arg(&app_dir);
    cmd.args(["--sandbox", "workspace-write", "-c", "approval_policy=\"never\"", "-c", "sandbox_workspace_write.network_access=false", "-c", "notify=[]"]);
    if let Some(thread) = &spec.thread_id {
        cmd.args(["resume", thread]);
    }
    cmd.arg("-");
    cmd.stdin(Stdio::piped()).stdout(Stdio::inherit()).stderr(Stdio::inherit());
    let mut child = cmd.spawn().context("spawning codex")?;
    {
        let mut stdin = child.stdin.take().context("codex stdin")?;
        let prompt = if spec.thread_id.is_some() { trimmed.to_string() } else { build_prompt(trimmed) };
        stdin.write_all(prompt.as_bytes())?;
        // stdin is dropped (closed) here, so codex does not wait for more input.
    }
    let status = child.wait()?;
    Ok(status.code().unwrap_or(1))
}

/// A job event derived from one codex JSONL line.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AgentEvent {
    Thread(String),
    Event { level: &'static str, code: &'static str, text: String },
    Files(Vec<String>),
    TurnCompleted(String),
    Failed(String),
    /// The sandbox could not be applied: fail closed, never retry unsandboxed.
    SandboxFailure(String),
}

/// Go label rules for commands: never show the raw command line.
fn command_label(command: &str) -> &'static str {
    let c = command.to_ascii_lowercase();
    if c.contains(" rg ") || c.contains("grep ") || c.contains(" rg\"") || c.contains("find ") {
        "Searching project files"
    } else if c.contains(" ls ") || c.contains("ls -") || c.contains(" tree ") {
        "Listing project files"
    } else if c.contains("cat ") || c.contains("sed -n") || c.contains("head ") || c.contains("tail ") || c.contains(" nl ") {
        "Reading project files"
    } else {
        "Running a project command"
    }
}

fn sandbox_failure(text: &str) -> bool {
    let t = text.to_ascii_lowercase();
    t.contains("sandbox") && (t.contains("not supported") || t.contains("failed to apply") || t.contains("unavailable") || t.contains("landlock") || t.contains("bwrap"))
}

pub fn map_event(line: &str, app_dir: &Path) -> Option<AgentEvent> {
    let v: Value = serde_json::from_str(line).ok()?;
    let kind = v["type"].as_str()?;
    match kind {
        "thread.started" => v["thread_id"].as_str().map(|t| AgentEvent::Thread(t.to_string())),
        "turn.started" => Some(AgentEvent::Event { level: "info", code: "codex.turn", text: "Codex started working.".into() }),
        "turn.completed" => {
            let u = &v["usage"];
            Some(AgentEvent::TurnCompleted(format!(
                "Codex finished (tokens in {}, out {}).",
                u["input_tokens"].as_u64().unwrap_or(0),
                u["output_tokens"].as_u64().unwrap_or(0)
            )))
        }
        "turn.failed" | "thread.failed" => {
            let msg = v["error"]["message"].as_str().or(v["message"].as_str()).unwrap_or("Codex turn failed.").to_string();
            Some(if sandbox_failure(&msg) { AgentEvent::SandboxFailure(msg) } else { AgentEvent::Failed(msg) })
        }
        "error" => {
            let msg = v["message"].as_str().unwrap_or("Codex reported an error.").to_string();
            Some(if sandbox_failure(&msg) { AgentEvent::SandboxFailure(msg) } else { AgentEvent::Event { level: "warn", code: "codex.error", text: msg } })
        }
        "item.completed" => {
            let item = &v["item"];
            match item["type"].as_str()? {
                "agent_message" => Some(AgentEvent::Event { level: "info", code: "codex.message", text: item["text"].as_str().unwrap_or("").to_string() }),
                "reasoning" => Some(AgentEvent::Event { level: "debug", code: "codex.reasoning", text: item["text"].as_str().unwrap_or("").to_string() }),
                "command_execution" => {
                    let label = command_label(item["command"].as_str().unwrap_or(""));
                    let exit = item["exit_code"].as_i64();
                    let text = match exit {
                        Some(0) | None => label.to_string(),
                        Some(code) => format!("{label} (exit {code})"),
                    };
                    let out = item["aggregated_output"].as_str().unwrap_or("");
                    if sandbox_failure(out) {
                        return Some(AgentEvent::SandboxFailure(format!("{label}: sandbox error")));
                    }
                    Some(AgentEvent::Event { level: "info", code: "codex.command", text })
                }
                "file_change" => {
                    let files = item["changes"]
                        .as_array()
                        .map(|a| {
                            a.iter()
                                .filter_map(|c| c["path"].as_str())
                                .map(|p| Path::new(p).strip_prefix(app_dir).map(|r| r.to_string_lossy().into_owned()).unwrap_or_else(|_| p.to_string()))
                                .collect()
                        })
                        .unwrap_or_default();
                    Some(AgentEvent::Files(files))
                }
                _ => None,
            }
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_recorded_transcript() {
        let fixture = include_str!("../tests/fixtures/codex-exec-s4.jsonl");
        let app = Path::new("/var/lib/cfagent/spike.78kaTM");
        let events: Vec<AgentEvent> = fixture.lines().filter_map(|l| map_event(l, app)).collect();
        assert_eq!(events[0], AgentEvent::Thread("01a1891c-68ae-7752-8a38-115b40c0e0a5".into()));
        assert!(matches!(&events[2], AgentEvent::Event { code: "codex.message", .. }));
        assert_eq!(events[3], AgentEvent::Files(vec!["inside.txt".into()]));
        // raw commands never appear
        assert!(events.iter().all(|e| !format!("{e:?}").contains("wc -c")));
        assert!(events.iter().any(|e| matches!(e, AgentEvent::Event { code: "codex.command", text, .. } if text == "Reading project files")));
        assert!(events.iter().any(|e| matches!(e, AgentEvent::TurnCompleted(t) if t.contains("in 38692"))));
        assert!(events.iter().any(|e| matches!(e, AgentEvent::Event { code: "codex.reasoning", level: "debug", .. })));
        assert!(events.iter().any(|e| matches!(e, AgentEvent::Event { code: "codex.error", .. })));
        // unknown types ignored: 12 lines, item.started lines and something.new dropped
        assert_eq!(events.len(), 9);
    }

    #[test]
    fn sandbox_errors_fail_closed() {
        let e = map_event(r#"{"type":"turn.failed","error":{"message":"sandbox not supported on this kernel (landlock)"}}"#, Path::new("/"));
        assert!(matches!(e, Some(AgentEvent::SandboxFailure(_))));
    }

    #[test]
    fn prompt_contains_parts() {
        let p = build_prompt("  A pricing table  ");
        assert!(p.starts_with(DEVELOPER_INSTRUCTIONS));
        assert!(p.contains(VPS_LINE));
        assert!(p.contains("couchfusion-component-builder") || p.contains("builderFieldMeta"));
        assert!(p.trim_end().ends_with("A pricing table"));
    }

    #[test]
    fn runner_feeds_prompt_on_stdin_and_closes_it() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        std::fs::create_dir_all(d.join("apps/demo")).unwrap();
        std::fs::create_dir_all(d.join("spec")).unwrap();
        let spec = RunSpec { job_id: "j".into(), prompt: "make a hero".into(), thread_id: None };
        std::fs::write(d.join("spec/demo.json"), serde_json::to_string(&spec).unwrap()).unwrap();
        // fake codex: records args and stdin, exits 0 only after stdin EOF
        let fake = d.join("codex");
        std::fs::write(&fake, format!("#!/bin/sh\necho \"$@\" > {0}/args\ncat > {0}/stdin\n", d.display())).unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&fake, std::fs::Permissions::from_mode(0o755)).unwrap();
        let code = run_agent(&RunnerArgs { app: "demo".into(), spec_dir: d.join("spec"), workspace: d.to_path_buf(), codex: fake }).unwrap();
        assert_eq!(code, 0);
        let args = std::fs::read_to_string(d.join("args")).unwrap();
        assert!(args.contains("exec --json --skip-git-repo-check -C"));
        assert!(args.contains("--sandbox workspace-write"));
        assert!(args.trim_end().ends_with(" -"));
        assert!(std::fs::read_to_string(d.join("stdin")).unwrap().ends_with("make a hero\n"));
    }
}
