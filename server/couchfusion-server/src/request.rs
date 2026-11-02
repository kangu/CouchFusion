//! LLM Request (contract `docs/superpowers/specs/2026-11-02-llm-request-contract.md`).
//!
//! A generic prompt: Codex may change app files and proposes page operations. Pages live in
//! CouchDB, which the sandbox cannot reach, so the control plane reads page context from the
//! dev server's control routes, hands it to Codex as files in `.couchfusion/request/`, checks
//! the proposal through the dev server and stores it. Nothing touches CouchDB until the user
//! applies the proposal (`llm.request.apply`).

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use serde::Deserialize;
use serde_json::{Map, Value, json};

use crate::agent::{RunKind, RunSpec};
use crate::config::SiteConfig;
use crate::dto::{DevStatus, JobSnapshot, RegistryChange};
use crate::error::ApiError;
use crate::http::App;
use crate::jobs::JobCtx;
use crate::ops::{AgentMarker, tail_agent, wait_registry, write_private};
use crate::workspace::{
    create_contained, dependency_change, diff_snapshots, lint_builder_field_meta, mkdir_contained, new_component_ids, open_contained,
    read_contained, remove_tree_contained, restart_reason, snapshot_all,
};

pub const JOB_REQUEST: &str = "llm.request";
pub const JOB_APPLY: &str = "llm.request.apply";

const FOLDER: &str = ".couchfusion";
const REQUEST_REL: &str = ".couchfusion/request";
const OPERATIONS_REL: &str = ".couchfusion/request/operations.json";
const MAX_PROMPT: usize = 16384;
/// Page documents handed to Codex; above this only the current page's documents are written.
const MAX_CONTEXT_BYTES: usize = 20 * 1024 * 1024;
const MAX_CONTROL_RESPONSE: usize = 64 * 1024 * 1024;
const MAX_PROPOSAL_BYTES: u64 = 5 * 1024 * 1024;
const MAX_OPERATIONS: usize = 50;
const MAX_SUMMARY_CHARS: usize = 4000;
const OPS: &[&str] = &["page.create", "page.update", "page.move", "page.copyContent", "page.publication"];

// ---------------------------------------------------------------- dev server access

fn dev_token(app: &App, site: &SiteConfig) -> Result<String, ApiError> {
    if app.dev.status(&site.id) != DevStatus::Ready {
        return Err(ApiError::conflict("Start the dev server first: page requests go through it."));
    }
    app.dev.token(&site.id).ok_or_else(|| ApiError::conflict("The dev server has no control token yet; restart it from the dev page."))
}

async fn control_get(app: &App, site: &SiteConfig, path: &str, timeout: Duration) -> Result<Value, ApiError> {
    let token = dev_token(app, site)?;
    let (status, body) = app
        .dev
        .control_get_limited(site, path, &token, timeout, MAX_CONTROL_RESPONSE)
        .await
        .map_err(|e| ApiError::new("process", format!("The dev server did not answer {path}: {e:#}")))?;
    if status == 404 {
        return Err(missing_routes());
    }
    if looks_like_html(&body) {
        return Err(missing_routes());
    }
    if status != 200 {
        return Err(ApiError::new("process", format!("The dev server answered {path} with HTTP {status}.")));
    }
    serde_json::from_str(&body).map_err(|e| ApiError::new("process", format!("The dev server sent invalid JSON for {path}: {e}")))
}

/// Without the layer's routes, Nuxt's catch-all content page answers with HTML (often 200).
fn looks_like_html(body: &str) -> bool {
    let start = body.trim_start();
    start.starts_with('<')
}

fn missing_routes() -> ApiError {
    ApiError::new("process", "The dev server has no page control routes. Update the content layer (layers/content) and restart dev.")
}

async fn control_post(app: &App, site: &SiteConfig, path: &str, body: &Value, timeout: Duration) -> Result<(u16, Value), ApiError> {
    let token = dev_token(app, site)?;
    let bytes = serde_json::to_vec(body).map_err(|e| ApiError::internal(e.to_string()))?;
    let (status, text) = app
        .dev
        .control_post(site, path, &token, bytes, timeout, MAX_CONTROL_RESPONSE)
        .await
        .map_err(|e| ApiError::new("process", format!("The dev server did not answer {path}: {e:#}")))?;
    if status == 404 || looks_like_html(&text) {
        return Err(missing_routes());
    }
    let value = serde_json::from_str(&text).unwrap_or_else(|_| json!({ "message": text.chars().take(500).collect::<String>() }));
    Ok((status, value))
}

/// `GET {site}/pages`: the page list for the request form.
pub async fn pages(app: &App, site: &SiteConfig) -> Result<Value, ApiError> {
    control_get(app, site, "/__couchfusion/control/pages", Duration::from_secs(30)).await
}

// ---------------------------------------------------------------- request folder

/// The request folder exists only while a request job runs; dropped (removed) on every outcome.
/// All file operations go through fds without following symlinks, because the app dir is
/// writable by Codex and the dev server.
struct RequestFolder {
    app_dir: PathBuf,
}

impl RequestFolder {
    fn create(app_dir: &Path) -> std::io::Result<Self> {
        remove_tree_contained(app_dir, REQUEST_REL)?;
        mkdir_contained(app_dir, FOLDER, 0o775)?;
        match create_contained(app_dir, ".couchfusion/.gitignore", b"*\n", 0o664) {
            Ok(()) => {}
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {}
            Err(e) => return Err(e),
        }
        mkdir_contained(app_dir, REQUEST_REL, 0o775)?;
        mkdir_contained(app_dir, &format!("{REQUEST_REL}/pages"), 0o775)?;
        Ok(Self { app_dir: app_dir.to_path_buf() })
    }

    fn write(&self, name: &str, bytes: &[u8]) -> std::io::Result<()> {
        create_contained(&self.app_dir, &format!("{REQUEST_REL}/{name}"), bytes, 0o664)
    }
}

impl Drop for RequestFolder {
    fn drop(&mut self) {
        if let Err(err) = remove_tree_contained(&self.app_dir, REQUEST_REL) {
            tracing::warn!("removing {}/{REQUEST_REL}: {err}", self.app_dir.display());
        }
    }
}

#[derive(Debug, Default, Deserialize)]
struct PageDocumentsResponse {
    #[serde(default)]
    pages: Vec<PageDocuments>,
}

#[derive(Debug, Deserialize)]
struct PageDocuments {
    path: String,
    #[serde(default)]
    documents: Vec<Value>,
}

/// Writes `context.json` and one `pages/NNN.json` per page; returns the base revisions
/// (`docId → rev`) of every document handed to Codex.
fn write_context(folder: &RequestFolder, list: &Value, docs: PageDocumentsResponse, current_path: Option<&str>) -> std::io::Result<(BTreeMap<String, String>, bool)> {
    let mut base_revs = BTreeMap::new();
    let total: usize = docs.pages.iter().map(|p| serde_json::to_vec(&p.documents).map(|b| b.len()).unwrap_or(0)).sum();
    let truncated = total > MAX_CONTEXT_BYTES;
    let mut files: BTreeMap<String, String> = BTreeMap::new();
    for (index, page) in docs.pages.iter().enumerate() {
        if truncated && Some(page.path.as_str()) != current_path {
            continue;
        }
        let name = format!("pages/{:03}.json", index + 1);
        let bytes = serde_json::to_vec_pretty(&json!({ "path": page.path, "documents": page.documents })).unwrap_or_default();
        folder.write(&name, &bytes)?;
        files.insert(page.path.clone(), name);
        for doc in &page.documents {
            if let (Some(id), Some(rev)) = (doc.get("id").and_then(Value::as_str), doc.get("rev").and_then(Value::as_str)) {
                base_revs.insert(id.to_string(), rev.to_string());
            }
        }
    }
    let pages: Vec<Value> = list
        .get("pages")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .map(|mut p| {
            if let Some(path) = p.get("path").and_then(Value::as_str)
                && let Some(file) = files.get(path)
                && let Some(obj) = p.as_object_mut()
            {
                obj.insert("file".into(), Value::String(file.clone()));
            }
            p
        })
        .collect();
    let context = json!({
        "currentPath": current_path,
        "defaultLocale": list.get("defaultLocale").cloned().unwrap_or(Value::Null),
        "locales": list.get("locales").cloned().unwrap_or(json!([])),
        "truncated": truncated,
        "pages": pages,
    });
    folder.write("context.json", &serde_json::to_vec_pretty(&context).unwrap_or_default())?;
    Ok((base_revs, truncated))
}

// ---------------------------------------------------------------- proposal

#[derive(Debug, PartialEq)]
struct Proposal {
    summary: Option<String>,
    operations: Vec<Value>,
}

/// Reads and shape-checks `operations.json`. `Ok(None)` = Codex wrote no file.
fn read_proposal(app_dir: &Path) -> Result<Option<Proposal>, String> {
    let file = match open_contained(app_dir, OPERATIONS_REL) {
        Ok(f) => f,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(format!("operations.json could not be opened safely: {e}")),
    };
    let len = file.file.metadata().map(|m| m.len()).unwrap_or(u64::MAX);
    if len > MAX_PROPOSAL_BYTES {
        return Err(format!("operations.json is {} MB; the limit is 5 MB.", len / (1024 * 1024)));
    }
    drop(file);
    let bytes = read_contained(app_dir, OPERATIONS_REL).map_err(|e| format!("operations.json could not be read: {e}"))?;
    if bytes.len() as u64 > MAX_PROPOSAL_BYTES {
        return Err("operations.json is over 5 MB.".into());
    }
    parse_proposal(&bytes).map(Some)
}

fn parse_proposal(bytes: &[u8]) -> Result<Proposal, String> {
    let value: Value = serde_json::from_slice(bytes).map_err(|e| format!("operations.json is not valid JSON: {e}"))?;
    let obj = value.as_object().ok_or("operations.json must be an object with summary and operations.")?;
    let summary = obj.get("summary").and_then(Value::as_str).map(|s| s.trim().chars().take(MAX_SUMMARY_CHARS).collect::<String>()).filter(|s| !s.is_empty());
    let operations = match obj.get("operations") {
        None | Some(Value::Null) => vec![],
        Some(Value::Array(ops)) => ops.clone(),
        Some(_) => return Err("operations must be a list.".into()),
    };
    if operations.len() > MAX_OPERATIONS {
        return Err(format!("{} operations proposed; at most {MAX_OPERATIONS} are allowed.", operations.len()));
    }
    for (index, op) in operations.iter().enumerate() {
        let kind = op.get("op").and_then(Value::as_str).unwrap_or("");
        if !OPS.contains(&kind) {
            return Err(format!("Operation {} has an unknown type {kind:?}.", index + 1));
        }
    }
    Ok(Proposal { summary, operations })
}

/// Short description of an operation for the UI (bodies are never shipped to the browser).
fn describe(op: &Value) -> Value {
    let s = |k: &str| op.get(k).and_then(Value::as_str).unwrap_or("").to_string();
    let kind = s("op");
    let (path, text) = match kind.as_str() {
        "page.update" => {
            let fields: Vec<&str> = ["title", "seo", "body", "layout"].into_iter().filter(|f| op.get(*f).is_some_and(|v| !v.is_null())).collect();
            let locale = op.get("locale").and_then(Value::as_str).map(|l| format!(" ({l})")).unwrap_or_default();
            (s("path"), format!("Update {}{locale}: {}", s("path"), fields.join(", ")))
        }
        "page.create" => {
            let state = op.get("publicationState").and_then(Value::as_str).unwrap_or("draft");
            (s("path"), format!("Create {} \"{}\" ({state})", s("path"), s("title")))
        }
        "page.move" => {
            let redirect = if op.get("keepRedirect").and_then(Value::as_bool).unwrap_or(false) { ", keep a redirect" } else { "" };
            (s("from"), format!("Move {} to {}{redirect}", s("from"), s("to")))
        }
        "page.copyContent" => (s("to"), format!("Replace the content of {} with {}", s("to"), s("from"))),
        "page.publication" => (s("path"), format!("Set {} to {}", s("path"), s("state"))),
        _ => (String::new(), kind.clone()),
    };
    json!({ "op": kind, "path": path, "text": text })
}

fn proposal_path(app: &App, job_id: &str) -> PathBuf {
    app.config.server.state_dir.join("requests").join(format!("{job_id}.json"))
}

// ---------------------------------------------------------------- request job

pub async fn start(app: Arc<App>, site: SiteConfig, prompt: String, current_path: Option<String>, idempotency_key: String) -> Result<JobSnapshot, ApiError> {
    let trimmed = prompt.trim();
    if trimmed.is_empty() || trimmed.len() > MAX_PROMPT {
        return Err(ApiError::validation("The request must be 1–16384 bytes."));
    }
    if idempotency_key.is_empty() || idempotency_key.len() > 128 {
        return Err(ApiError::validation("An Idempotency-Key header of 1–128 characters is required."));
    }
    let current_path = match current_path.map(|p| p.trim().to_string()).filter(|p| !p.is_empty()) {
        Some(p) if !p.starts_with('/') || p.len() > 512 || p.chars().any(char::is_control) => {
            return Err(ApiError::validation("currentPath must be a site path starting with /."));
        }
        other => other,
    };
    let key = format!("request:{}:{idempotency_key}", site.id);
    if let Ok(Some(job_id)) = app.store.idempotent_job(&key)
        && let Some(job) = app.jobs.snapshot(&job_id)
    {
        return Ok(job);
    }
    dev_token(&app, &site)?;
    let ctx = app.jobs.create(JOB_REQUEST, &app.config.workspace.id, &site.id)?;
    let _ = app.store.remember_idempotency(&key, &ctx.id);
    let snapshot = ctx.snapshot();
    let prompt = trimmed.to_string();
    tokio::spawn(async move {
        let mut result = Map::new();
        if let Err(err) = run(&app, &ctx, &site, &prompt, current_path.as_deref(), &mut result).await {
            ctx.fail(&err, (!result.is_empty()).then_some(result));
        }
    });
    Ok(snapshot)
}

async fn run(app: &App, ctx: &JobCtx, site: &SiteConfig, prompt: &str, current_path: Option<&str>, result: &mut Map<String, Value>) -> Result<(), ApiError> {
    ctx.start();
    result.insert("currentPath".into(), json!(current_path));
    result.insert("model".into(), json!(app.config.agent.model));
    let _marker = AgentMarker::create(app.config.agent_marker(&site.id)).map_err(|e| ApiError::filesystem(format!("Creating the agent marker: {e}")))?;
    let app_dir = app.config.app_dir(&site.id);

    ctx.info("request.context", "Reading the pages from the dev server…");
    let list = control_get(app, site, "/__couchfusion/control/pages", Duration::from_secs(30)).await?;
    let docs: PageDocumentsResponse = serde_json::from_value(control_get(app, site, "/__couchfusion/control/page-documents?all=1", Duration::from_secs(60)).await?)
        .map_err(|e| ApiError::new("process", format!("Unexpected page-documents response: {e}")))?;
    let page_count = docs.pages.len();
    let folder = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || RequestFolder::create(&dir)).await.map_err(|e| ApiError::internal(e.to_string()))?
    }
    .map_err(|e| ApiError::filesystem(format!("Preparing .couchfusion/request: {e}")))?;
    let (base_revs, truncated) = write_context(&folder, &list, docs, current_path).map_err(|e| ApiError::filesystem(format!("Writing the page context: {e}")))?;
    ctx.info(
        "request.context",
        if truncated {
            format!("{page_count} pages listed; only the current page's documents were included (over 20 MB).")
        } else {
            format!("{page_count} pages handed to Codex ({} documents).", base_revs.len())
        },
    );

    let before = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || snapshot_all(&dir)).await.map_err(|e| ApiError::internal(e.to_string()))?
    };
    let spec = RunSpec {
        job_id: ctx.id.clone(),
        prompt: prompt.to_string(),
        thread_id: None,
        kind: RunKind::Request,
        model: app.config.agent.model.clone(),
        reasoning_effort: app.config.agent.reasoning_effort.clone(),
    };
    write_private(&app.config.agent_spec(&site.id), &serde_json::to_vec(&spec).unwrap_or_default(), 0o644)
        .map_err(|e| ApiError::filesystem(format!("Writing the agent run spec: {e}")))?;
    let out_path = app.config.agent_out(&site.id);
    write_private(&out_path, b"", 0o600).map_err(|e| ApiError::filesystem(format!("Preparing agent output: {e}")))?;
    app.units.start(&site.agent_unit).await.map_err(|e| ApiError::new("process", format!("Could not start {}: {e:#}", site.agent_unit)))?;
    ctx.info("codex.start", "Codex is working on the request…");

    let outcome = tail_agent(app, ctx, site, &out_path, &app_dir).await;
    if let Some(thread) = &outcome.thread_id {
        result.insert("threadId".into(), Value::String(thread.clone()));
    }
    if let Some(usage) = outcome.usage {
        result.insert("usage".into(), json!(usage));
        ctx.info("codex.usage", format!("Tokens: in {} ({} cached), out {}.", usage.input_tokens, usage.cached_input_tokens, usage.output_tokens));
    }
    let after = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || snapshot_all(&dir)).await.map_err(|e| ApiError::internal(e.to_string()))?
    };
    let changes = diff_snapshots(&before, &after);
    result.insert("changedFiles".into(), json!(changes.all()));
    if ctx.cancelled() {
        return Err(ApiError::new("process", "Cancelled."));
    }
    if let Some(reason) = outcome.sandbox_failure {
        return Err(ApiError::new("process", format!("Codex could not apply its sandbox; nothing was retried without it. ({reason})")));
    }
    if let Some(reason) = outcome.failure {
        return Err(ApiError::new("process", format!("Codex failed: {reason}")));
    }
    if !outcome.completed {
        return Err(ApiError::new("process", "Codex exited without completing its turn."));
    }
    let deps = dependency_change(&changes);
    if !deps.is_empty() {
        ctx.warn("policy.dependency_change", format!("Dependency files changed: {}. Review the diff; nothing was reverted.", deps.join(", ")));
        return Err(ApiError::new("validation", "Policy check failed: the agent changed package.json or a lockfile."));
    }

    // Component files: lint, then wait for the registry so page checks see new components.
    let mut lint = Vec::new();
    for src in changes.added.iter().chain(changes.modified.iter()).filter(|p| p.starts_with("app/components/content/") && p.ends_with(".vue")) {
        if let Ok(bytes) = read_contained(&app_dir, src) {
            for w in lint_builder_field_meta(&String::from_utf8_lossy(&bytes)) {
                ctx.warn("lint.builder_field_meta", format!("{src}: {w}"));
                lint.push(format!("{src}: {w}"));
            }
        }
    }
    result.insert("lint".into(), json!(lint));
    if let Some(reason) = restart_reason(&changes) {
        app.dev.set_needs_restart(&site.id, reason.clone());
        result.insert("needsRestart".into(), json!({ "reason": reason }));
    }
    let added: Vec<String> = new_component_ids(&changes).into_iter().map(|(id, _)| id).collect();
    let removed: Vec<String> = changes
        .deleted
        .iter()
        .filter(|p| p.starts_with("app/components/content/") && p.ends_with(".vue"))
        .filter_map(|p| Path::new(p).file_stem().map(|s| crate::util::kebab_case(&s.to_string_lossy())))
        .collect();
    let touched_components = changes.all().iter().any(|p| p.starts_with("app/components/content/"));
    if touched_components {
        let (want, gone) = (added.clone(), removed.clone());
        wait_registry(app, ctx, &site.id, move |ids| want.iter().all(|w| ids.contains(w)) && gone.iter().all(|g| !ids.contains(g))).await?;
        if !added.is_empty() || !removed.is_empty() {
            app.hub.registry(&site.id, RegistryChange { added: added.clone(), removed: removed.clone() });
        }
    }
    result.insert("addedComponents".into(), json!(added));

    // Proposal.
    let proposal = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || read_proposal(&dir)).await.map_err(|e| ApiError::internal(e.to_string()))?
    };
    drop(folder);
    let fallback_summary = outcome.last_message.map(|m| m.chars().take(MAX_SUMMARY_CHARS).collect::<String>());
    match proposal {
        Err(problem) => {
            ctx.warn("request.proposal", &problem);
            result.insert("summary".into(), json!(fallback_summary));
            result.insert("operations".into(), json!([]));
            result.insert("check".into(), json!({ "ok": false, "problems": [{ "index": null, "message": problem }], "preview": [] }));
        }
        Ok(None) => {
            ctx.info("request.proposal", "No page changes proposed.");
            result.insert("summary".into(), json!(fallback_summary));
            result.insert("operations".into(), json!([]));
        }
        Ok(Some(p)) => {
            result.insert("summary".into(), json!(p.summary.or(fallback_summary)));
            result.insert("operations".into(), Value::Array(p.operations.iter().map(describe).collect()));
            if p.operations.is_empty() {
                ctx.info("request.proposal", "No page changes proposed.");
            } else {
                ctx.info("request.check", format!("Checking {} proposed page operation(s)…", p.operations.len()));
                let body = json!({ "operations": p.operations, "baseRevs": base_revs });
                let (status, check) = control_post(app, site, "/__couchfusion/control/pages/check", &body, Duration::from_secs(60)).await?;
                if status != 200 {
                    ctx.warn("request.check", format!("The page check answered HTTP {status}."));
                }
                let ok = status == 200 && check.get("ok").and_then(Value::as_bool).unwrap_or(false);
                let problems = check.get("problems").cloned().unwrap_or_else(|| json!([{ "index": null, "message": check.get("message").cloned().unwrap_or(json!("The page check failed.")) }]));
                result.insert("check".into(), json!({ "ok": ok, "problems": problems, "preview": check.get("preview").cloned().unwrap_or(json!([])) }));
                if ok {
                    write_private(&proposal_path(app, &ctx.id), &serde_json::to_vec(&body).unwrap_or_default(), 0o600)
                        .map_err(|e| ApiError::filesystem(format!("Storing the proposal: {e}")))?;
                    result.insert("applyState".into(), json!("pending"));
                    ctx.info("request.proposal", format!("{} page change(s) ready to review and apply.", p.operations.len()));
                } else {
                    ctx.warn("request.proposal", "The proposed page changes have problems and cannot be applied.");
                }
            }
        }
    }
    ctx.succeed(std::mem::take(result));
    Ok(())
}

// ---------------------------------------------------------------- apply job

pub async fn apply(app: Arc<App>, site: SiteConfig, request_job_id: String) -> Result<JobSnapshot, ApiError> {
    let request = app.jobs.snapshot(&request_job_id).ok_or_else(|| ApiError::not_found("Request not found."))?;
    if request.kind != JOB_REQUEST || request.site_id.as_deref() != Some(site.id.as_str()) {
        return Err(ApiError::not_found("Request not found."));
    }
    let key = format!("apply:{request_job_id}");
    if let Ok(Some(job_id)) = app.store.idempotent_job(&key)
        && let Some(job) = app.jobs.snapshot(&job_id)
    {
        return Ok(job);
    }
    let path = proposal_path(&app, &request_job_id);
    if !path.is_file() {
        return Err(ApiError::conflict("This request has no page changes to apply (or they were applied already)."));
    }
    dev_token(&app, &site)?;
    let ctx = app.jobs.create(JOB_APPLY, &app.config.workspace.id, &site.id)?;
    let _ = app.store.remember_idempotency(&key, &ctx.id);
    let snapshot = ctx.snapshot();
    tokio::spawn(async move {
        let mut result = Map::new();
        result.insert("requestJobId".into(), Value::String(request_job_id.clone()));
        if let Err(err) = run_apply(&app, &ctx, &site, &path, &mut result).await {
            ctx.fail(&err, Some(result));
        }
    });
    Ok(snapshot)
}

async fn run_apply(app: &App, ctx: &JobCtx, site: &SiteConfig, path: &Path, result: &mut Map<String, Value>) -> Result<(), ApiError> {
    ctx.start();
    let body: Value = serde_json::from_slice(&std::fs::read(path).map_err(|e| ApiError::filesystem(format!("Reading the proposal: {e}")))?)
        .map_err(|e| ApiError::internal(format!("The stored proposal is unreadable: {e}")))?;
    let count = body.get("operations").and_then(Value::as_array).map(Vec::len).unwrap_or(0);
    ctx.info("request.apply", format!("Applying {count} page operation(s)…"));
    let (status, answer) = control_post(app, site, "/__couchfusion/control/pages/apply", &body, Duration::from_secs(120)).await?;
    result.insert("applied".into(), answer.get("applied").cloned().unwrap_or(json!([])));
    let ok = status == 200 && answer.get("ok").and_then(Value::as_bool).unwrap_or(false);
    if ok {
        // One apply per proposal: keep it for the record, but under a name `apply` no longer finds.
        let _ = std::fs::rename(path, path.with_extension("applied.json"));
        ctx.info("request.applied", format!("Applied {count} page operation(s). Page history has the previous versions."));
        ctx.succeed(std::mem::take(result));
        return Ok(());
    }
    if status == 409 {
        let problems = answer.get("problems").cloned().unwrap_or(json!([]));
        result.insert("problems".into(), problems.clone());
        let first = problems.get(0).and_then(|p| p.get("message")).and_then(Value::as_str).unwrap_or("the pages changed since the request ran");
        return Err(ApiError::new("validation", format!("Nothing was applied: {first}")));
    }
    let applied = answer.get("applied").and_then(Value::as_array).map(Vec::len).unwrap_or(0);
    let failed = answer.get("failed").cloned().unwrap_or(Value::Null);
    let message = failed.get("message").and_then(Value::as_str).or_else(|| answer.get("message").and_then(Value::as_str)).unwrap_or("unknown error");
    result.insert("failed".into(), failed.clone());
    // Partial writes cannot be re-applied safely: retire the proposal.
    if applied > 0 {
        let _ = std::fs::rename(path, path.with_extension("partial.json"));
    }
    Err(ApiError::new("process", format!("Applied {applied} of {count} page operation(s), then failed: {message}")))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn proposal_shapes() {
        let p = parse_proposal(br#"{"summary":" Did it ","operations":[{"op":"page.move","from":"/a","to":"/b","keepRedirect":true}]}"#).unwrap();
        assert_eq!(p.summary.as_deref(), Some("Did it"));
        assert_eq!(p.operations.len(), 1);
        assert!(parse_proposal(br#"{"operations":[{"op":"page.delete","path":"/a"}]}"#).unwrap_err().contains("unknown type"));
        assert!(parse_proposal(b"nope").unwrap_err().contains("not valid JSON"));
        assert!(parse_proposal(br#"{"operations":{}}"#).unwrap_err().contains("must be a list"));
        let many = format!(r#"{{"operations":[{}]}}"#, vec![r#"{"op":"page.publication","path":"/a","state":"draft"}"#; 51].join(","));
        assert!(parse_proposal(many.as_bytes()).unwrap_err().contains("at most 50"));
        assert_eq!(parse_proposal(br#"{"summary":""}"#).unwrap(), Proposal { summary: None, operations: vec![] });
    }

    #[test]
    fn describes_operations_without_bodies() {
        let d = describe(&json!({"op":"page.update","path":"/about","locale":"ro","title":"X","body":{"type":"minimal","value":[["p",{},"secret body"]]}}));
        assert_eq!(d["text"], "Update /about (ro): title, body");
        assert!(!d.to_string().contains("secret body"));
        assert_eq!(describe(&json!({"op":"page.copyContent","from":"/b","to":"/a"}))["text"], "Replace the content of /a with /b");
        assert_eq!(describe(&json!({"op":"page.create","path":"/new","title":"New","body":{"type":"minimal","value":[]}}))["text"], "Create /new \"New\" (draft)");
        assert!(parse_proposal(br#"{"operations":[{"op":"page.create","path":"/n","title":"N","body":{"type":"minimal","value":[]}}]}"#).is_ok());
        assert_eq!(describe(&json!({"op":"page.move","from":"/a","to":"/b","keepRedirect":true}))["text"], "Move /a to /b, keep a redirect");
    }

    #[test]
    fn folder_is_written_and_removed_without_following_symlinks() {
        let tmp = tempfile::tempdir().unwrap();
        let app = tmp.path().join("app");
        let outside = tmp.path().join("outside");
        std::fs::create_dir_all(&app).unwrap();
        std::fs::create_dir_all(&outside).unwrap();
        std::fs::write(outside.join("keep.txt"), "x").unwrap();
        {
            let folder = RequestFolder::create(&app).unwrap();
            let list = json!({"defaultLocale":"en","locales":["en"],"pages":[{"path":"/about","title":"About"}]});
            let docs = PageDocumentsResponse { pages: vec![PageDocuments { path: "/about".into(), documents: vec![json!({"id":"page-/about","rev":"3-a","body":{}})] }] };
            let (revs, truncated) = write_context(&folder, &list, docs, Some("/about")).unwrap();
            assert_eq!(revs.get("page-/about").map(String::as_str), Some("3-a"));
            assert!(!truncated);
            let ctx: Value = serde_json::from_slice(&std::fs::read(app.join(".couchfusion/request/context.json")).unwrap()).unwrap();
            assert_eq!(ctx["pages"][0]["file"], "pages/001.json");
            assert_eq!(ctx["currentPath"], "/about");
            assert_eq!(std::fs::read_to_string(app.join(".couchfusion/.gitignore")).unwrap(), "*\n");
            // Codex plants a symlink to a directory outside and a FIFO-free proposal.
            std::os::unix::fs::symlink(&outside, app.join(".couchfusion/request/escape")).unwrap();
            std::fs::write(app.join(".couchfusion/request/operations.json"), r#"{"summary":"s","operations":[]}"#).unwrap();
            assert_eq!(read_proposal(&app).unwrap().unwrap().summary.as_deref(), Some("s"));
        }
        assert!(!app.join(".couchfusion/request").exists());
        assert!(outside.join("keep.txt").exists(), "a symlink inside the folder must not be followed on removal");
    }

    #[test]
    fn symlinked_proposal_is_refused() {
        let tmp = tempfile::tempdir().unwrap();
        let app = tmp.path().join("app");
        std::fs::create_dir_all(app.join(".couchfusion/request")).unwrap();
        std::fs::write(tmp.path().join("secret.json"), r#"{"operations":[]}"#).unwrap();
        std::os::unix::fs::symlink(tmp.path().join("secret.json"), app.join(".couchfusion/request/operations.json")).unwrap();
        assert!(read_proposal(&app).unwrap_err().contains("could not be opened safely"));
        // A symlinked request folder is not followed either.
        let app2 = tmp.path().join("app2");
        std::fs::create_dir_all(app2.join(".couchfusion")).unwrap();
        std::fs::create_dir_all(tmp.path().join("elsewhere")).unwrap();
        std::fs::write(tmp.path().join("elsewhere/operations.json"), "{}").unwrap();
        std::os::unix::fs::symlink(tmp.path().join("elsewhere"), app2.join(".couchfusion/request")).unwrap();
        assert!(read_proposal(&app2).is_err());
        let folder = RequestFolder::create(&app2).unwrap();
        drop(folder);
        assert!(tmp.path().join("elsewhere/operations.json").exists(), "removing a symlinked folder removes only the link");
    }
}
