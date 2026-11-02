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

/// Phase 2: keep runs short. Type-checking a Nuxt app took ~2.5 of 3.5 minutes on hetzner1.
pub const VALIDATION_LINE: &str = "Validation for this run: run only the registry generator (`bun run registry:build`). Do not run `nuxi typecheck`, `vue-tsc`, `nuxt build`, lint, or tests; the control plane validates the registry itself.";

/// Token usage reported by codex `turn.completed`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Usage {
    pub input_tokens: u64,
    pub cached_input_tokens: u64,
    pub output_tokens: u64,
}

/// What a run is for: a new component (Phase 1) or a generic request (contract 2026-11-02).
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum RunKind {
    #[default]
    Component,
    Request,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RunSpec {
    pub job_id: String,
    pub prompt: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub thread_id: Option<String>,
    #[serde(default)]
    pub kind: RunKind,
    /// `codex exec -m` (from `[agent] model`); None = Codex's default.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    /// `-c model_reasoning_effort=…` (from `[agent] reasoning_effort`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reasoning_effort: Option<String>,
}

pub fn build_prompt(user_prompt: &str) -> String {
    format!(
        "{DEVELOPER_INSTRUCTIONS}\n{VPS_LINE}\n{UNATTENDED_LINE}\n{VALIDATION_LINE}\n\nFollow this first-party skill:\n\n{SKILL}\n\n---\n\nComponent request:\n\n{}\n",
        user_prompt.trim()
    )
}

/// Generic request: app files plus proposed page operations (pages live in CouchDB, which the
/// sandbox cannot reach; the user reviews and applies the proposal).
pub const REQUEST_INSTRUCTIONS: &str = "You are handling a change request for a CouchFusion site (Nuxt app with the content layer). Work only inside the selected application root. You may create or change any file there except dependency manifests and lockfiles (package.json, bun.lock, bun.lockb, package-lock.json, pnpm-lock.yaml, yarn.lock); add no dependencies. Do not stage, commit, branch, tag, reset, clean, restore, or otherwise change Git history or index state. Preserve existing uncommitted work and avoid unrelated rewrites. Stay within CouchFusion site work: pages, content components, styles, layouts and app code. Requests about servers, deployments, other apps, git, credentials or anything outside this app are out of scope: change nothing and say so in the summary. Do not write task reports, implementation_results notes or other documentation about this run unless the request asks for documentation; the control plane records the run itself.";

pub const REQUEST_PAGES: &str = "Pages are stored in a database you cannot access. A read-only copy is in `.couchfusion/request/`: `context.json` lists every page (path, title, locales, publication state, redirect) and names the file in `.couchfusion/request/pages/` that holds its documents (one per locale, with `body` in minimark form: `{\"type\":\"minimal\",\"value\":[...]}`, where each node is `[tag, props, ...children]` or a text string). `currentPath` is the page the user has open in the builder, if known; \"this page\" means that page. Never edit files in `.couchfusion/request/` except `operations.json`.\n\nTo change pages, write `.couchfusion/request/operations.json` (UTF-8 JSON, at most 50 operations):\n\n{\"summary\": \"one short paragraph for a non-technical user: what you changed in files and what page changes you propose\", \"operations\": [\n  {\"op\": \"page.create\", \"path\": \"/new-page\", \"title\": \"…\", \"seo\": {\"title\": \"…\", \"description\": \"…\", \"image\": null}, \"body\": {\"type\": \"minimal\", \"value\": []}, \"layout\": {}, \"publicationState\": \"draft\"},\n  {\"op\": \"page.update\", \"path\": \"/about\", \"locale\": \"en\", \"title\": \"…\", \"seo\": {\"title\": \"…\", \"description\": \"…\", \"image\": null}, \"body\": {\"type\": \"minimal\", \"value\": []}, \"layout\": {}},\n  {\"op\": \"page.move\", \"from\": \"/old\", \"to\": \"/new\", \"keepRedirect\": true},\n  {\"op\": \"page.copyContent\", \"from\": \"/b\", \"to\": \"/a\"},\n  {\"op\": \"page.publication\", \"path\": \"/x\", \"state\": \"draft\"}\n]}\n\nRules: `page.create` adds a new page in the default locale (title and body required; seo and layout optional; it stays a draft unless you set `publicationState` to `published`, which you do only when the request asks for a public page); base its body and layout on an existing page of the same kind, and later operations in the same list may update it (e.g. add translations with `page.update` + `locale`). `page.update` changes one locale (omit `locale` for the default locale) and needs at least one of title, seo (partial is fine), body, layout; when you change a body, start from the existing document and keep everything you were not asked to change. `page.move` gives a page a new URL (keepRedirect leaves a redirect at the old URL). `page.copyContent` makes page `to` show the body and layout of page `from` while keeping its own URL, title and SEO (this is what \"replace route A with route B\" means). `page.publication` sets `published` or `draft`. Pages cannot be deleted. In bodies use only components registered in `app/content-builder/component-definitions.json` (registry id or PascalCase name) and plain HTML. If you create a new content component for a page, build it first (following the component skill below), then reference it. The user reviews the operations and applies them; never say that pages were already changed. Always write `operations.json`, with an empty `operations` list when no page changes are needed.";

pub fn build_request_prompt(user_prompt: &str) -> String {
    format!(
        "{REQUEST_INSTRUCTIONS}\n{VPS_LINE}\n{UNATTENDED_LINE}\n{VALIDATION_LINE}\n\n{REQUEST_PAGES}\n\nFor content components follow this first-party skill:\n\n{SKILL}\n\n---\n\nRequest:\n\n{}\n",
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
    if let Some(model) = &spec.model {
        if !crate::config::valid_model(model) {
            bail!("invalid model {model:?}");
        }
        cmd.args(["-m", model]);
    }
    if let Some(effort) = &spec.reasoning_effort {
        if !crate::config::REASONING_EFFORTS.contains(&effort.as_str()) {
            bail!("invalid reasoning effort {effort:?}");
        }
        cmd.args(["-c", &format!("model_reasoning_effort=\"{effort}\"")]);
    }
    if let Some(thread) = &spec.thread_id {
        cmd.args(["resume", thread]);
    }
    cmd.arg("-");
    cmd.stdin(Stdio::piped()).stdout(Stdio::inherit()).stderr(Stdio::inherit());
    let mut child = cmd.spawn().context("spawning codex")?;
    {
        let mut stdin = child.stdin.take().context("codex stdin")?;
        let prompt = match (spec.thread_id.is_some(), spec.kind) {
            (true, _) => trimmed.to_string(),
            (false, RunKind::Component) => build_prompt(trimmed),
            (false, RunKind::Request) => build_request_prompt(trimmed),
        };
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
    TurnCompleted { text: String, usage: Option<Usage> },
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
            let usage = u.is_object().then(|| Usage {
                input_tokens: u["input_tokens"].as_u64().unwrap_or(0),
                cached_input_tokens: u["cached_input_tokens"].as_u64().unwrap_or(0),
                output_tokens: u["output_tokens"].as_u64().unwrap_or(0),
            });
            let text = match usage {
                Some(u) => format!("Codex finished (tokens in {}, out {}).", u.input_tokens, u.output_tokens),
                None => "Codex finished.".to_string(),
            };
            Some(AgentEvent::TurnCompleted { text, usage })
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
        assert!(events.iter().any(|e| matches!(e, AgentEvent::TurnCompleted { text, usage: Some(u) } if text.contains("in 38692") && u.input_tokens == 38692)));
        assert!(events.iter().any(|e| matches!(e, AgentEvent::Event { code: "codex.reasoning", level: "debug", .. })));
        assert!(events.iter().any(|e| matches!(e, AgentEvent::Event { code: "codex.error", .. })));
        // unknown types ignored: 12 lines, item.started lines and something.new dropped
        assert_eq!(events.len(), 9);
    }

    #[test]
    fn usage_capture() {
        let e = map_event(r#"{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":20}}"#, Path::new("/"));
        assert_eq!(
            e,
            Some(AgentEvent::TurnCompleted {
                text: "Codex finished (tokens in 100, out 20).".into(),
                usage: Some(Usage { input_tokens: 100, cached_input_tokens: 40, output_tokens: 20 })
            })
        );
        let e = map_event(r#"{"type":"turn.completed"}"#, Path::new("/"));
        assert_eq!(e, Some(AgentEvent::TurnCompleted { text: "Codex finished.".into(), usage: None }));
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
        assert!(p.contains(VALIDATION_LINE));
        assert!(p.contains("Do not run `nuxi typecheck`"));
        assert!(p.contains("couchfusion-component-builder") || p.contains("builderFieldMeta"));
        assert!(p.trim_end().ends_with("A pricing table"));
    }

    #[test]
    fn request_runs_pass_model_and_request_prompt() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        std::fs::create_dir_all(d.join("apps/demo")).unwrap();
        std::fs::create_dir_all(d.join("spec")).unwrap();
        let spec = RunSpec {
            job_id: "j".into(),
            prompt: "replace /a with /b".into(),
            thread_id: None,
            kind: RunKind::Request,
            model: Some("gpt-5.6-sol".into()),
            reasoning_effort: Some("high".into()),
        };
        std::fs::write(d.join("spec/demo.json"), serde_json::to_string(&spec).unwrap()).unwrap();
        let fake = d.join("codex");
        std::fs::write(&fake, format!("#!/bin/sh\necho \"$@\" > {0}/args\ncat > {0}/stdin\n", d.display())).unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&fake, std::fs::Permissions::from_mode(0o755)).unwrap();
        assert_eq!(run_agent(&RunnerArgs { app: "demo".into(), spec_dir: d.join("spec"), workspace: d.to_path_buf(), codex: fake.clone() }).unwrap(), 0);
        let args = std::fs::read_to_string(d.join("args")).unwrap();
        assert!(args.contains("-m gpt-5.6-sol"));
        assert!(args.contains("model_reasoning_effort=\"high\""));
        let stdin = std::fs::read_to_string(d.join("stdin")).unwrap();
        assert!(stdin.starts_with(REQUEST_INSTRUCTIONS));
        assert!(stdin.contains(".couchfusion/request/operations.json"));
        assert!(stdin.trim_end().ends_with("replace /a with /b"));
        // An invalid model in the spec is refused before codex runs.
        let bad = RunSpec { model: Some("--dangerously".into()), ..spec };
        std::fs::write(d.join("spec/demo.json"), serde_json::to_string(&bad).unwrap()).unwrap();
        assert!(run_agent(&RunnerArgs { app: "demo".into(), spec_dir: d.join("spec"), workspace: d.to_path_buf(), codex: fake }).is_err());
    }

    #[test]
    fn runner_feeds_prompt_on_stdin_and_closes_it() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        std::fs::create_dir_all(d.join("apps/demo")).unwrap();
        std::fs::create_dir_all(d.join("spec")).unwrap();
        let spec = RunSpec { job_id: "j".into(), prompt: "make a hero".into(), thread_id: None, kind: RunKind::Component, model: None, reasoning_effort: None };
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
