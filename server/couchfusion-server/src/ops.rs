//! Long-running operations: component usage, delete (spec §6.1), generate via Codex (§6.2
//! simplified) and git commit. Each mutating operation runs as a job under the per-site lease.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use serde_json::{Map, Value, json};

use crate::agent::{AgentEvent, RunSpec, map_event};
use crate::config::SiteConfig;
use crate::dto::{ComponentUsage, DevStatus, PageRef, RegistryChange};
use crate::error::ApiError;
use crate::http::App;
use crate::jobs::JobCtx;
use crate::util::sha256_hex;
use crate::workspace::{
    CONTENT_DIR_REL, diff_snapshots, inspect_registry, lint_builder_field_meta, new_component_ids, open_contained, read_contained,
    rename_contained, restart_reason, snapshot,
};

const AGENT_MAX_RUNTIME: Duration = Duration::from_secs(1800);

// ---------------------------------------------------------------- usage

struct Resolved {
    source: String,
    sha256: String,
}

fn resolve_component(app_dir: &Path, component_id: &str) -> Result<Resolved, ApiError> {
    if !crate::workspace::valid_resource_id(component_id) {
        return Err(ApiError::validation("Invalid component id."));
    }
    let registry = inspect_registry(app_dir);
    if !registry.usable() {
        return Err(ApiError::new("config", registry.problem.unwrap_or_else(|| format!("Component registry is {}.", registry.status))));
    }
    let component = registry.find(component_id).ok_or_else(|| ApiError::not_found("Component not found in the registry."))?;
    if !component.source.starts_with(&format!("{CONTENT_DIR_REL}/")) {
        return Err(ApiError::validation("Only app content components (app/components/content/*.vue) can be deleted here."));
    }
    let bytes = read_contained(app_dir, &component.source).map_err(|_| ApiError::validation("The component source is unsafe or unavailable."))?;
    Ok(Resolved { source: component.source.clone(), sha256: sha256_hex(&bytes) })
}

async fn git_state(app: &App, app_dir: &Path, source: &str) -> &'static str {
    if !app_dir.join(".git").exists() {
        return "unknown";
    }
    let Ok(out) = app.git.run(app_dir, &["status", "--porcelain=v1", "--", source]).await else { return "unknown" };
    if out.code != 0 {
        return "unknown";
    }
    let text = String::from_utf8_lossy(&out.stdout);
    let line = text.lines().next().unwrap_or("");
    if line.is_empty() {
        "tracked_clean"
    } else if line.starts_with("??") {
        "untracked"
    } else {
        "tracked_modified"
    }
}

/// Pages using the component, from the dev server's control route. None if the scan failed.
async fn page_usage(app: &App, site: &SiteConfig, component_id: &str) -> Option<Vec<PageRef>> {
    if app.dev.status(&site.id) != DevStatus::Ready {
        return None;
    }
    let token = app.dev.token(&site.id)?;
    let path = format!("/__couchfusion/control/component-usage?id={}", urlencode(component_id));
    let (status, body) = app.dev.control_get(site, &path, &token, Duration::from_secs(20)).await.ok()?;
    if status != 200 {
        return None;
    }
    let v: Value = serde_json::from_str(&body).ok()?;
    serde_json::from_value(v.get("pages")?.clone()).ok()
}

fn urlencode(s: &str) -> String {
    s.bytes()
        .map(|b| if b.is_ascii_alphanumeric() || b"-_.~".contains(&b) { (b as char).to_string() } else { format!("%{b:02X}") })
        .collect()
}

pub async fn usage(app: &App, site: &SiteConfig, component_id: &str) -> Result<ComponentUsage, ApiError> {
    let app_dir = app.config.app_dir(&site.id);
    let resolved = {
        let dir = app_dir.clone();
        let id = component_id.to_string();
        tokio::task::spawn_blocking(move || resolve_component(&dir, &id)).await.map_err(|e| ApiError::internal(e.to_string()))??
    };
    let code_references = {
        let dir = app_dir.clone();
        let id = component_id.to_string();
        let src = resolved.source.clone();
        tokio::task::spawn_blocking(move || crate::workspace::code_references(&dir, &id, &src)).await.unwrap_or_default()
    };
    let pages = page_usage(app, site, component_id).await;
    Ok(ComponentUsage {
        component_id: component_id.to_string(),
        source: resolved.source.clone(),
        sha256: resolved.sha256,
        git: git_state(app, &app_dir, &resolved.source).await,
        page_scan: if pages.is_some() { "ok" } else { "unavailable" },
        pages: pages.unwrap_or_default(),
        code_references,
    })
}

// ---------------------------------------------------------------- registry wait

/// Waits until the manifest is valid and `pred(ids)` holds; falls back to running the layer
/// generator once. Returns the final id list.
async fn wait_registry(app: &App, ctx: &JobCtx, site: &str, pred: impl Fn(&[String]) -> bool) -> Result<Vec<String>, ApiError> {
    let app_dir = app.config.app_dir(site);
    let timeout = Duration::from_secs(app.config.server.registry_timeout_secs);
    ctx.info("registry.wait", "Waiting for the component registry to update…");
    if let Some(ids) = poll_registry(&app_dir, timeout, &pred).await {
        ctx.info("registry.updated", "Component registry updated.");
        return Ok(ids);
    }
    ctx.warn("registry.fallback", "The registry watcher did not update in time; running the generator directly.");
    let script = app.config.workspace.root.join("layers/content/scripts/generate-component-registry.mjs");
    let out = tokio::process::Command::new(&app.config.toolchain.bun)
        .arg(&script)
        .arg(format!("--app={site}"))
        .current_dir(&app.config.workspace.root)
        .stdin(std::process::Stdio::null())
        .output()
        .await
        .map_err(|e| ApiError::filesystem(format!("Could not run the registry generator: {e}")))?;
    if !out.status.success() {
        ctx.warn("registry.fallback", String::from_utf8_lossy(&out.stderr).chars().take(2000).collect::<String>());
    }
    poll_registry(&app_dir, Duration::from_secs(3), &pred)
        .await
        .inspect(|_| ctx.info("registry.updated", "Component registry updated."))
        .ok_or_else(|| ApiError::filesystem("The component registry did not update."))
}

async fn poll_registry(app_dir: &Path, timeout: Duration, pred: &impl Fn(&[String]) -> bool) -> Option<Vec<String>> {
    let deadline = tokio::time::Instant::now() + timeout;
    loop {
        let dir = app_dir.to_path_buf();
        let reg = tokio::task::spawn_blocking(move || inspect_registry(&dir)).await.ok()?;
        if reg.usable() {
            let ids = reg.ids();
            if pred(&ids) {
                return Some(ids);
            }
        }
        if tokio::time::Instant::now() > deadline {
            return None;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
}

// ---------------------------------------------------------------- delete

/// Synchronous checks (409/422 answered directly), then a `component.delete` job.
pub async fn delete_component(
    app: Arc<App>,
    site: SiteConfig,
    component_id: String,
    expected_sha256: String,
    acknowledge_usage: bool,
) -> Result<crate::dto::JobSnapshot, ApiError> {
    let usage = usage(&app, &site, &component_id).await?;
    if usage.sha256 != expected_sha256 {
        let mut f = Map::new();
        f.insert("sha256".into(), Value::String(usage.sha256.clone()));
        return Err(ApiError::conflict("The file changed since you looked at it.").with_fields(f));
    }
    let used = !usage.pages.is_empty() || !usage.code_references.is_empty() || usage.page_scan == "unavailable";
    if used && !acknowledge_usage {
        let mut f = Map::new();
        f.insert("pages".into(), serde_json::to_value(&usage.pages).unwrap_or_default());
        f.insert("codeReferences".into(), serde_json::to_value(&usage.code_references).unwrap_or_default());
        f.insert("pageScan".into(), Value::String(usage.page_scan.into()));
        return Err(ApiError::conflict("The component is in use (or page usage could not be checked). Acknowledge to delete anyway.").with_fields(f));
    }
    let ctx = app.jobs.create("component.delete", &app.config.workspace.id, &site.id)?;
    let snapshot = ctx.snapshot();
    let git = usage.git;
    tokio::spawn(async move {
        if let Err(err) = run_delete(&app, &ctx, &site, &component_id, &expected_sha256, git).await {
            ctx.fail(&err, None);
        }
    });
    Ok(snapshot)
}

async fn run_delete(app: &App, ctx: &JobCtx, site: &SiteConfig, component_id: &str, expected: &str, git: &str) -> Result<(), ApiError> {
    ctx.start();
    let app_dir = app.config.app_dir(&site.id);
    let resolved = resolve_component(&app_dir, component_id)?;
    ctx.info("component.resolve", format!("{component_id} → {}", resolved.source));
    if resolved.sha256 != expected {
        return Err(ApiError::conflict("The file changed since you looked at it."));
    }
    let file = open_contained(&app_dir, &resolved.source).map_err(|_| ApiError::validation("The component source is unsafe or unavailable."))?;
    let trash_root = app.config.trash_dir().join(&ctx.id);
    let target = trash_root.join(&resolved.source);
    std::fs::create_dir_all(target.parent().expect("parent")).map_err(|e| ApiError::filesystem(format!("Creating trash: {e}")))?;
    rename_contained(&file, &target).map_err(|e| ApiError::filesystem(format!("Moving the file to trash failed: {e}")))?;
    let meta = json!({"jobId": ctx.id, "siteId": site.id, "componentId": component_id, "source": resolved.source, "sha256": resolved.sha256, "git": git, "deletedAt": crate::util::now_rfc3339()});
    let _ = std::fs::write(trash_root.join("meta.json"), serde_json::to_vec_pretty(&meta).unwrap_or_default());
    let _ = app.store.record_trash(&ctx.id, &site.id, &resolved.source, &target.to_string_lossy(), &resolved.sha256, git);
    ctx.info("component.trash", format!("Moved {} to trash ({}).", resolved.source, ctx.id));

    let id = component_id.to_string();
    wait_registry(app, ctx, &site.id, move |ids| !ids.contains(&id)).await?;
    app.hub.registry(&site.id, RegistryChange { added: vec![], removed: vec![component_id.to_string()] });

    let mut result = Map::new();
    result.insert("componentId".into(), Value::String(component_id.to_string()));
    result.insert("trashId".into(), Value::String(ctx.id.clone()));
    ctx.info("component.deleted", format!("Deleted {component_id}."));
    ctx.succeed(result);
    spawn_health_check(app, site);
    Ok(())
}

/// After a file change: if the dev unit died, restart it once.
fn spawn_health_check(app: &App, site: &SiteConfig) {
    let dev = app.dev.clone();
    let units = app.units.clone();
    let site = site.clone();
    tokio::spawn(async move {
        tokio::time::sleep(Duration::from_secs(3)).await;
        if dev.status(&site.id) == DevStatus::Ready
            && let Ok(state) = units.state(&site.dev_unit).await
            && !state.running()
        {
            tracing::warn!("{} stopped after a component change; restarting once", site.dev_unit);
            let _ = dev.restart(&site.id).await;
        }
    });
}

// ---------------------------------------------------------------- generate

pub async fn generate_component(app: Arc<App>, site: SiteConfig, prompt: String, idempotency_key: String) -> Result<crate::dto::JobSnapshot, ApiError> {
    let trimmed = prompt.trim();
    if trimmed.is_empty() || trimmed.len() > 16384 {
        return Err(ApiError::validation("The prompt must be 1–16384 bytes."));
    }
    if idempotency_key.is_empty() || idempotency_key.len() > 128 {
        return Err(ApiError::validation("An Idempotency-Key header of 1–128 characters is required."));
    }
    let key = format!("generate:{}:{idempotency_key}", site.id);
    if let Ok(Some(job_id)) = app.store.idempotent_job(&key)
        && let Some(job) = app.jobs.snapshot(&job_id)
    {
        return Ok(job);
    }
    let ctx = app.jobs.create("component.generate", &app.config.workspace.id, &site.id)?;
    let _ = app.store.remember_idempotency(&key, &ctx.id);
    let snapshot = ctx.snapshot();
    let prompt = trimmed.to_string();
    tokio::spawn(async move {
        let mut result = Map::new();
        if let Err(err) = run_generate(&app, &ctx, &site, &prompt, &mut result).await {
            ctx.fail(&err, (!result.is_empty()).then_some(result));
        }
    });
    Ok(snapshot)
}

fn write_private(path: &Path, bytes: &[u8], mode: u32) -> std::io::Result<()> {
    use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    let mut f = std::fs::OpenOptions::new().create(true).truncate(true).write(true).mode(mode).open(path)?;
    std::io::Write::write_all(&mut f, bytes)?;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode))
}

async fn run_generate(app: &App, ctx: &JobCtx, site: &SiteConfig, prompt: &str, result: &mut Map<String, Value>) -> Result<(), ApiError> {
    ctx.start();
    let app_dir = app.config.app_dir(&site.id);
    let before = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || snapshot(&dir)).await.map_err(|e| ApiError::internal(e.to_string()))?
    };
    ctx.info("codex.baseline", format!("Recorded a baseline of {} files.", before.len()));

    let spec = RunSpec { job_id: ctx.id.clone(), prompt: prompt.to_string(), thread_id: None };
    write_private(&app.config.agent_spec(&site.id), &serde_json::to_vec(&spec).unwrap_or_default(), 0o644)
        .map_err(|e| ApiError::filesystem(format!("Writing the agent run spec: {e}")))?;
    let out_path = app.config.agent_out(&site.id);
    write_private(&out_path, b"", 0o600).map_err(|e| ApiError::filesystem(format!("Preparing agent output: {e}")))?;

    app.units.start(&site.agent_unit).await.map_err(|e| ApiError::new("process", format!("Could not start {}: {e:#}", site.agent_unit)))?;
    ctx.info("codex.start", "Codex is working on the component…");

    let outcome = tail_agent(app, ctx, site, &out_path, &app_dir).await;
    if let Some(thread) = &outcome.thread_id {
        result.insert("threadId".into(), Value::String(thread.clone()));
    }

    let after = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || snapshot(&dir)).await.map_err(|e| ApiError::internal(e.to_string()))?
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
    let deps = crate::workspace::dependency_change(&changes);
    if !deps.is_empty() {
        ctx.warn("policy.dependency_change", format!("Dependency files changed: {}. Review the diff; nothing was reverted.", deps.join(", ")));
        return Err(ApiError::new("validation", "Policy check failed: the agent changed package.json or a lockfile."));
    }
    let created = new_component_ids(&changes);
    let Some((component_id, source)) = created.first().cloned() else {
        return Err(ApiError::new("validation", "Codex did not create a new app/components/content/*.vue file."));
    };
    result.insert("componentId".into(), Value::String(component_id.clone()));

    let mut lint = Vec::new();
    for (_, src) in &created {
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

    let id = component_id.clone();
    wait_registry(app, ctx, &site.id, move |ids| ids.contains(&id)).await?;
    app.hub.registry(&site.id, RegistryChange { added: vec![component_id.clone()], removed: vec![] });
    ctx.info("component.generated", format!("Created {component_id} ({source})."));
    ctx.succeed(std::mem::take(result));
    Ok(())
}

#[derive(Default)]
struct AgentOutcome {
    thread_id: Option<String>,
    completed: bool,
    failure: Option<String>,
    sandbox_failure: Option<String>,
}

/// Tails the agent's JSONL by byte offset until the unit stops.
async fn tail_agent(app: &App, ctx: &JobCtx, site: &SiteConfig, path: &PathBuf, app_dir: &Path) -> AgentOutcome {
    use tokio::io::{AsyncReadExt, AsyncSeekExt};
    let mut outcome = AgentOutcome::default();
    let mut offset: u64 = 0;
    let mut partial = String::new();
    let started = tokio::time::Instant::now();
    let mut cancel = ctx.cancel_signal();
    let mut stopped_seen = 0u32;
    loop {
        // Read new bytes.
        if let Ok(mut file) = tokio::fs::File::open(path).await
            && file.seek(std::io::SeekFrom::Start(offset)).await.is_ok()
        {
            let mut buf = Vec::new();
            if let Ok(n) = file.read_to_end(&mut buf).await {
                offset += n as u64;
                partial.push_str(&String::from_utf8_lossy(&buf));
            }
        }
        while let Some(pos) = partial.find('\n') {
            let line: String = partial.drain(..=pos).collect();
            match map_event(line.trim(), app_dir) {
                Some(AgentEvent::Thread(t)) => outcome.thread_id = Some(t),
                Some(AgentEvent::Event { level, code, text }) => ctx.event(level, code, text),
                Some(AgentEvent::Files(files)) => ctx.info("codex.files", format!("Changed: {}", files.join(", "))),
                Some(AgentEvent::TurnCompleted(text)) => {
                    outcome.completed = true;
                    ctx.info("codex.done", text);
                }
                Some(AgentEvent::Failed(msg)) => outcome.failure = Some(msg),
                Some(AgentEvent::SandboxFailure(msg)) => outcome.sandbox_failure = Some(msg),
                None => {}
            }
        }
        let running = app.units.state(&site.agent_unit).await.map(|s| s.running()).unwrap_or(false);
        if !running {
            stopped_seen += 1;
            // One more read after the unit stopped, so trailing lines are not lost.
            if stopped_seen >= 2 {
                break;
            }
        }
        if started.elapsed() > AGENT_MAX_RUNTIME {
            outcome.failure = Some("timed out after 30 minutes".into());
            let _ = app.units.stop(&site.agent_unit).await;
            break;
        }
        tokio::select! {
            _ = tokio::time::sleep(Duration::from_millis(400)) => {}
            changed = cancel.changed() => {
                if changed.is_ok() && *cancel.borrow() {
                    ctx.warn("codex.cancel", "Cancelling: stopping the agent unit.");
                    let _ = app.units.stop(&site.agent_unit).await;
                }
            }
        }
    }
    outcome
}

// ---------------------------------------------------------------- commit

pub async fn commit(app: Arc<App>, repo_id: String, message: String, paths: Vec<String>, expected_head: String) -> Result<crate::dto::JobSnapshot, ApiError> {
    let message = message.trim().to_string();
    if message.is_empty() || message.len() > 10_000 {
        return Err(ApiError::validation("The commit message must be 1–10000 characters."));
    }
    if paths.is_empty() || paths.len() > 1000 {
        return Err(ApiError::validation("Select 1–1000 files to commit."));
    }
    let repo = app.config.workspace.root.join(&repo_id);
    app.git.check_config(&repo).await?;
    let (_, files) = app.git.status(&repo).await?;
    for p in &paths {
        if crate::git::denied_path(p) {
            return Err(ApiError::validation(format!("Refusing to commit {p}.")));
        }
        if !files.iter().any(|f| &f.path == p) {
            return Err(ApiError::validation(format!("{p} has no changes to commit.")));
        }
    }
    let head = app.git.head(&repo).await.map(|h| h.sha).unwrap_or_default();
    if head != expected_head {
        let mut f = Map::new();
        f.insert("head".into(), Value::String(head));
        return Err(ApiError::conflict("The repository HEAD moved since you looked.").with_fields(f));
    }
    // App repos share the site lease with delete/generate; other repos get their own.
    let lease = repo_id.strip_prefix("apps/").map(str::to_string).unwrap_or_else(|| format!("repo:{repo_id}"));
    let ctx = app.jobs.create("git.commit", &app.config.workspace.id, &lease)?;
    let snapshot = ctx.snapshot();
    tokio::spawn(async move {
        ctx.start();
        ctx.info("git.commit", format!("Committing {} file(s) in {repo_id}…", paths.len()));
        match app.git.commit(&repo, &message, &paths).await {
            Ok(sha) => {
                ctx.info("git.commit", format!("Committed {}.", &sha[..sha.len().min(12)]));
                let mut r = Map::new();
                r.insert("sha".into(), Value::String(sha));
                ctx.succeed(r);
            }
            Err(err) => ctx.fail(&err, None),
        }
    });
    Ok(snapshot)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn url_encoding() {
        assert_eq!(urlencode("network-hero"), "network-hero");
        assert_eq!(urlencode("a b&c"), "a%20b%26c");
    }
}
