//! Long-running operations: component usage, delete (spec §6.1), generate via Codex (§6.2
//! simplified) and git commit. Each mutating operation runs as a job under the per-site lease.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use serde_json::{Map, Value, json};

use crate::agent::{AgentEvent, RunSpec, Usage, map_event};
use crate::config::SiteConfig;
use crate::dto::{ComponentSource, ComponentUsage, DevStatus, PageRef, RegistryChange, TrashEntry};
use crate::error::ApiError;
use crate::http::App;
use crate::jobs::JobCtx;
use crate::util::sha256_hex;
use crate::workspace::{
    CONTENT_DIR_REL, create_contained, diff_snapshots, inspect_registry, lint_builder_field_meta, new_component_ids, open_contained,
    read_contained, rename_contained, restart_reason, snapshot,
};

const AGENT_MAX_RUNTIME: Duration = Duration::from_secs(1800);
/// Upper bound for one `cf-registry@` run (the unit itself has `TimeoutStartSec=120`).
const REGISTRY_UNIT_TIMEOUT: Duration = Duration::from_secs(150);
const SOURCE_MAX_BYTES: usize = 256 * 1024;
/// After the registry updated, the watcher often runs once more (queued file events). Keep the
/// reload guard until the registry files have been unchanged this long, so that run is covered too.
const REGISTRY_QUIET: Duration = Duration::from_secs(4);
const REGISTRY_SETTLE_MAX: Duration = Duration::from_secs(15);

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
        return Err(ApiError::validation("Only app content components (app/components/content/*.vue) are supported here."));
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
        settle_registry(&app_dir).await;
        return Ok(ids);
    }
    ctx.warn("registry.fallback", "The registry watcher did not update in time; running the generator (cf-registry) once.");
    if let Some(cfg) = app.config.site(site)
        && let Err(err) = run_registry_unit(app, cfg).await
    {
        ctx.warn("registry.fallback", err);
    }
    let ids = poll_registry(&app_dir, Duration::from_secs(3), &pred)
        .await
        .inspect(|_| ctx.info("registry.updated", "Component registry updated."))
        .ok_or_else(|| ApiError::filesystem("The component registry did not update."))?;
    settle_registry(&app_dir).await;
    Ok(ids)
}

/// Waits until `component-definitions.{ts,json}` have not changed for [`REGISTRY_QUIET`]
/// (at most [`REGISTRY_SETTLE_MAX`]), so the watcher's follow-up run finishes while the reload
/// guard is still held and the builder gets exactly one (bridge) reload.
async fn settle_registry(app_dir: &Path) {
    let files = [
        app_dir.join("app/content-builder/component-definitions.ts"),
        app_dir.join(crate::workspace::MANIFEST_REL),
    ];
    let stamp = || files.iter().map(|f| std::fs::metadata(f).and_then(|m| m.modified()).ok()).collect::<Vec<_>>();
    let start = tokio::time::Instant::now();
    let mut last = stamp();
    let mut quiet_since = tokio::time::Instant::now();
    while start.elapsed() < REGISTRY_SETTLE_MAX {
        tokio::time::sleep(Duration::from_millis(250)).await;
        let now = stamp();
        if now != last {
            last = now;
            quiet_since = tokio::time::Instant::now();
        } else if quiet_since.elapsed() >= REGISTRY_QUIET {
            return;
        }
    }
}

/// Runs the layer's registry generator as `cfdev` through the oneshot `cf-registry@<site>` unit.
/// Repository code is never executed as `cfcp` (agent/dev-writable code must not run as the
/// push-key holder). Returns a redacted error message on failure.
async fn run_registry_unit(app: &App, site: &SiteConfig) -> Result<(), String> {
    let unit = site.registry_unit();
    match app.units.run_oneshot(&unit, REGISTRY_UNIT_TIMEOUT).await {
        Ok(()) => Ok(()),
        Err(err) => {
            let redactor = crate::util::Redactor::new(vec![]);
            let tail = app.units.journal_tail(&unit, 15).await;
            let mut msg = format!("{unit} failed: {err:#}");
            if !tail.is_empty() {
                msg.push('\n');
                msg.push_str(&tail.iter().map(|l| redactor.redact(l)).collect::<Vec<_>>().join("\n"));
            }
            Err(msg.chars().take(4000).collect())
        }
    }
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
    // Hold the reload guard so the builder sees one controlled reload at the end, not Vite's.
    let _marker = AgentMarker::create(app.config.agent_marker(&site.id))
        .map_err(|e| ApiError::filesystem(format!("Creating the reload-guard marker: {e}")))?;
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

/// Marker file that exists while a component job (generate, delete, restore, rebuild) runs;
/// removed on drop, i.e. on every outcome. The dev server's gated Vite plugin suppresses its own
/// full reloads while it exists, so the builder only gets the bridge's single reload afterwards.
struct AgentMarker(PathBuf);

impl AgentMarker {
    fn create(path: PathBuf) -> std::io::Result<Self> {
        write_private(&path, b"", 0o640)?;
        Ok(Self(path))
    }
}

impl Drop for AgentMarker {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

/// Startup recovery: jobs that were running when the previous process died are failed, so no
/// marker may survive (it would block the builder's reloads forever).
pub fn clear_agent_markers(config: &crate::config::Config) {
    for site in &config.sites {
        let marker = config.agent_marker(&site.id);
        if std::fs::remove_file(&marker).is_ok() {
            tracing::info!("removed stale agent marker {}", marker.display());
        }
    }
}

async fn run_generate(app: &App, ctx: &JobCtx, site: &SiteConfig, prompt: &str, result: &mut Map<String, Value>) -> Result<(), ApiError> {
    ctx.start();
    let _marker = AgentMarker::create(app.config.agent_marker(&site.id))
        .map_err(|e| ApiError::filesystem(format!("Creating the agent marker: {e}")))?;
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
    if let Some(usage) = outcome.usage {
        result.insert("usage".into(), json!(usage));
        ctx.info(
            "codex.usage",
            format!("Tokens: in {} ({} cached), out {}.", usage.input_tokens, usage.cached_input_tokens, usage.output_tokens),
        );
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
    usage: Option<Usage>,
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
                Some(AgentEvent::TurnCompleted { text, usage }) => {
                    outcome.completed = true;
                    outcome.usage = usage.or(outcome.usage);
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

// ---------------------------------------------------------------- source

pub async fn component_source(app: &App, site: &SiteConfig, component_id: &str) -> Result<ComponentSource, ApiError> {
    let app_dir = app.config.app_dir(&site.id);
    let id = component_id.to_string();
    tokio::task::spawn_blocking(move || {
        let resolved = resolve_component(&app_dir, &id)?;
        let bytes = read_contained(&app_dir, &resolved.source).map_err(|_| ApiError::validation("The component source is unsafe or unavailable."))?;
        Ok(source_view(&id, &resolved.source, &bytes))
    })
    .await
    .map_err(|e| ApiError::internal(e.to_string()))?
}

/// Caps the content at 256 KiB on a UTF-8 character boundary.
fn source_view(component_id: &str, path: &str, bytes: &[u8]) -> ComponentSource {
    let text = String::from_utf8_lossy(bytes);
    let truncated = text.len() > SOURCE_MAX_BYTES;
    let mut end = text.len().min(SOURCE_MAX_BYTES);
    while !text.is_char_boundary(end) {
        end -= 1;
    }
    ComponentSource {
        component_id: component_id.to_string(),
        path: path.to_string(),
        sha256: sha256_hex(bytes),
        size: bytes.len() as u64,
        content: text[..end].to_string(),
        truncated,
    }
}

// ---------------------------------------------------------------- trash and restore

/// Component id of a trash entry: `meta.json` when present, else the registry rule (kebab case
/// of the file stem).
fn trash_component_id(app: &App, row: &crate::store::TrashRow) -> String {
    let meta = app.config.trash_dir().join(&row.job_id).join("meta.json");
    std::fs::read(&meta)
        .ok()
        .and_then(|b| serde_json::from_slice::<Value>(&b).ok())
        .and_then(|v| v["componentId"].as_str().map(str::to_string))
        .unwrap_or_else(|| {
            let stem = Path::new(&row.source).file_stem().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default();
            crate::util::kebab_case(&stem)
        })
}

/// Why an entry cannot be restored right now, if anything.
fn restore_problem(app: &App, row: &crate::store::TrashRow) -> Option<String> {
    if row.restored.is_some() {
        return Some("Already restored.".into());
    }
    if !crate::workspace::valid_source(&row.source) || !row.source.starts_with(&format!("{CONTENT_DIR_REL}/")) {
        return Some("The recorded source path is not a content component.".into());
    }
    let trashed = Path::new(&row.trash_path);
    if !trashed.starts_with(app.config.trash_dir()) || !trashed.is_file() {
        return Some("The trashed copy is missing.".into());
    }
    if std::fs::symlink_metadata(app.config.app_dir(&row.site).join(&row.source)).is_ok() {
        return Some(format!("A file already exists at {}.", row.source));
    }
    None
}

pub fn trash_list(app: &App, site: &SiteConfig) -> Result<Vec<TrashEntry>, ApiError> {
    let rows = app.store.trash_entries(&site.id).map_err(|e| ApiError::internal(e.to_string()))?;
    Ok(rows
        .iter()
        .map(|row| {
            let problem = restore_problem(app, row);
            TrashEntry {
                trash_id: row.job_id.clone(),
                component_id: trash_component_id(app, row),
                source: row.source.clone(),
                sha256: row.sha256.clone(),
                git: row.git.clone(),
                deleted_at: time::OffsetDateTime::from_unix_timestamp(row.created).map(crate::util::format_time).unwrap_or_default(),
                restorable: problem.is_none(),
                problem,
            }
        })
        .collect())
}

/// Synchronous checks (404/409), then a `component.restore` job.
pub async fn restore_component(app: Arc<App>, site: SiteConfig, trash_id: String) -> Result<crate::dto::JobSnapshot, ApiError> {
    if !crate::workspace::valid_resource_id(&trash_id) {
        return Err(ApiError::validation("Invalid trash id."));
    }
    let row = app
        .store
        .trash_entry(&trash_id)
        .map_err(|e| ApiError::internal(e.to_string()))?
        .filter(|r| r.site == site.id)
        .ok_or_else(|| ApiError::not_found("Trash entry not found."))?;
    if let Some(problem) = restore_problem(&app, &row) {
        return Err(ApiError::conflict(format!("Cannot restore: {problem}")));
    }
    let ctx = app.jobs.create("component.restore", &app.config.workspace.id, &site.id)?;
    let snapshot = ctx.snapshot();
    tokio::spawn(async move {
        if let Err(err) = run_restore(&app, &ctx, &site, &row).await {
            ctx.fail(&err, None);
        }
    });
    Ok(snapshot)
}

async fn run_restore(app: &App, ctx: &JobCtx, site: &SiteConfig, row: &crate::store::TrashRow) -> Result<(), ApiError> {
    ctx.start();
    // Hold the reload guard so the builder sees one controlled reload at the end, not Vite's.
    let _marker = AgentMarker::create(app.config.agent_marker(&site.id))
        .map_err(|e| ApiError::filesystem(format!("Creating the reload-guard marker: {e}")))?;
    if let Some(problem) = restore_problem(app, row) {
        return Err(ApiError::conflict(format!("Cannot restore: {problem}")));
    }
    let component_id = trash_component_id(app, row);
    let bytes = std::fs::read(&row.trash_path).map_err(|e| ApiError::filesystem(format!("Reading the trashed copy: {e}")))?;
    if sha256_hex(&bytes) != row.sha256 {
        return Err(ApiError::conflict("The trashed copy does not match the recorded sha256; not restoring."));
    }
    let app_dir = app.config.app_dir(&site.id);
    create_contained(&app_dir, &row.source, &bytes, 0o664).map_err(|e| match e.kind() {
        std::io::ErrorKind::AlreadyExists => ApiError::conflict(format!("A file already exists at {}.", row.source)),
        _ => ApiError::filesystem(format!("Restoring {}: {e}", row.source)),
    })?;
    ctx.info("component.restore", format!("Restored {} from trash ({}).", row.source, row.job_id));
    let _ = app.store.mark_trash_restored(&row.job_id);

    let id = component_id.clone();
    wait_registry(app, ctx, &site.id, move |ids| ids.contains(&id)).await?;
    app.hub.registry(&site.id, RegistryChange { added: vec![component_id.clone()], removed: vec![] });
    let mut result = Map::new();
    result.insert("componentId".into(), Value::String(component_id.clone()));
    result.insert("trashId".into(), Value::String(row.job_id.clone()));
    ctx.info("component.restored", format!("Restored {component_id}."));
    ctx.succeed(result);
    spawn_health_check(app, site);
    Ok(())
}

// ---------------------------------------------------------------- registry rebuild

pub async fn rebuild_registry(app: Arc<App>, site: SiteConfig) -> Result<crate::dto::JobSnapshot, ApiError> {
    let ctx = app.jobs.create("component-registry.rebuild", &app.config.workspace.id, &site.id)?;
    let snapshot = ctx.snapshot();
    tokio::spawn(async move {
        if let Err(err) = run_rebuild(&app, &ctx, &site).await {
            ctx.fail(&err, None);
        }
    });
    Ok(snapshot)
}

async fn run_rebuild(app: &App, ctx: &JobCtx, site: &SiteConfig) -> Result<(), ApiError> {
    ctx.start();
    // Hold the reload guard so the builder sees one controlled reload at the end, not Vite's.
    let _marker = AgentMarker::create(app.config.agent_marker(&site.id))
        .map_err(|e| ApiError::filesystem(format!("Creating the reload-guard marker: {e}")))?;
    let app_dir = app.config.app_dir(&site.id);
    let before = {
        let dir = app_dir.clone();
        tokio::task::spawn_blocking(move || inspect_registry(&dir).ids()).await.unwrap_or_default()
    };
    ctx.info("registry.rebuild", format!("Running {}…", site.registry_unit()));
    run_registry_unit(app, site).await.map_err(ApiError::filesystem)?;
    let ids = poll_registry(&app_dir, Duration::from_secs(5), &|_: &[String]| true)
        .await
        .ok_or_else(|| ApiError::filesystem("The registry generator ran, but the manifest is not usable."))?;
    let added: Vec<String> = ids.iter().filter(|i| !before.contains(i)).cloned().collect();
    let removed: Vec<String> = before.iter().filter(|i| !ids.contains(i)).cloned().collect();
    if !added.is_empty() || !removed.is_empty() {
        app.hub.registry(&site.id, RegistryChange { added, removed });
    }
    ctx.info("registry.updated", format!("Component registry rebuilt ({} components).", ids.len()));
    let mut result = Map::new();
    result.insert("components".into(), json!(ids.len()));
    ctx.succeed(result);
    Ok(())
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
    fn source_is_capped_on_char_boundary() {
        let small = source_view("x", "app/components/content/X.vue", b"<template>hi</template>");
        assert!(!small.truncated);
        assert_eq!(small.size, 23);
        assert_eq!(small.sha256.len(), 64);
        // 'é' is 2 bytes; put one across the cap.
        let mut big = vec![b'a'; SOURCE_MAX_BYTES - 1];
        big.extend("é".as_bytes());
        big.extend(b"tail");
        let v = source_view("x", "p", &big);
        assert!(v.truncated);
        assert_eq!(v.content.len(), SOURCE_MAX_BYTES - 1);
        assert_eq!(v.size, big.len() as u64);
    }

    #[test]
    fn stale_agent_markers_are_cleared_on_startup() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        let text = format!(
            "[server]\nstate_dir = \"{0}/state\"\nrun_dir = \"{0}/run\"\nspa_dir = \"{0}/spa\"\npublic_host = \"h\"\n[auth]\nallowed_logins = [\"a@b\"]\n[workspace]\nid = \"w\"\nname = \"w\"\nroot = \"{0}/ws\"\n[[sites]]\nid = \"demo\"\nslot_port = 9181\ndev_port = 4401\ndev_unit = \"cf-dev@demo.service\"\nagent_unit = \"cf-agent@demo.service\"\n",
            d.display()
        );
        let config: crate::config::Config = toml::from_str(&text).unwrap();
        let marker = config.agent_marker("demo");
        std::fs::create_dir_all(marker.parent().unwrap()).unwrap();
        std::fs::write(&marker, b"").unwrap();
        clear_agent_markers(&config);
        assert!(!marker.exists());
        // The guard removes its marker on drop, whatever the job outcome.
        {
            let _m = AgentMarker::create(marker.clone()).unwrap();
            assert!(marker.exists());
        }
        assert!(!marker.exists());
    }

    #[test]
    fn url_encoding() {
        assert_eq!(urlencode("network-hero"), "network-hero");
        assert_eq!(urlencode("a b&c"), "a%20b%26c");
    }
}
