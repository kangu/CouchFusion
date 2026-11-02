//! `/api/v1` routes (contract §2).

use std::sync::Arc;

use axum::Router;
use axum::extract::{Extension, Path, Query, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::Response;
use axum::routing::{delete, get, post};
use serde::Deserialize;
use serde_json::Value;

use crate::config::SiteConfig;
use crate::dto::*;
use crate::error::{ApiError, ApiResult, data};
use crate::events::{resume_cursor, sse_frame};
use crate::http::{App, Caller, JsonBody, sse_response};
use crate::util::now_rfc3339;

type S = State<Arc<App>>;

pub fn routes() -> Router<Arc<App>> {
    let site = "/api/v1/workspaces/{ws}/sites/{site}";
    Router::new()
        .route("/api/v1/bootstrap", get(bootstrap))
        .route("/api/v1/onboarding", get(onboarding))
        .route("/api/v1/workspaces", get(workspaces))
        .route("/api/v1/workspaces/{ws}", get(workspace))
        .route(&format!("{site}/components"), get(components))
        .route(&format!("{site}/components/generate"), post(generate))
        .route(&format!("{site}/components/trash"), get(trash_list))
        .route(&format!("{site}/components/trash/{{tid}}/restore"), post(trash_restore))
        .route(&format!("{site}/components/registry/rebuild"), post(registry_rebuild))
        .route(&format!("{site}/components/{{cid}}"), delete(delete_component))
        .route(&format!("{site}/components/{{cid}}/usage"), get(usage))
        .route(&format!("{site}/components/{{cid}}/source"), get(source))
        .route(&format!("{site}/dev"), get(dev_view))
        .route(&format!("{site}/dev/start"), post(dev_start))
        .route(&format!("{site}/dev/stop"), post(dev_stop))
        .route(&format!("{site}/dev/restart"), post(dev_restart))
        .route(&format!("{site}/events"), get(site_events))
        .route("/api/v1/jobs/{id}", get(job))
        .route("/api/v1/jobs/{id}/cancel", post(job_cancel))
        .route("/api/v1/jobs/{id}/events", get(job_events))
        .route("/api/v1/workspaces/{ws}/repos", get(repos))
        .route("/api/v1/workspaces/{ws}/repos/{repo}/status", get(repo_status))
        .route("/api/v1/workspaces/{ws}/repos/{repo}/diff", get(repo_diff))
        .route("/api/v1/workspaces/{ws}/repos/{repo}/commit", post(repo_commit))
        .route("/api/v1/workspaces/{ws}/repos/{repo}/fetch", post(repo_fetch))
        .route("/api/v1/workspaces/{ws}/repos/{repo}/pull", post(repo_pull))
        .route("/api/v1/workspaces/{ws}/repos/{repo}/push", post(repo_push))
        .route(&format!("{site}/pages"), get(site_pages))
        .route(&format!("{site}/requests"), post(request_create))
        .route(&format!("{site}/requests/{{jid}}/apply"), post(request_apply))
        .route(&format!("{site}/prod"), get(prod_view))
        .route(&format!("{site}/prod/deploy"), post(prod_deploy))
        .route(&format!("{site}/prod/rollback"), post(prod_rollback))
        .route("/api/v1/services", get(services))
        .route("/api/v1/services/{unit}/logs", get(service_logs))
}

fn check_ws(app: &App, ws: &str) -> Result<(), ApiError> {
    if ws == app.config.workspace.id { Ok(()) } else { Err(ApiError::not_found("Workspace not found.")) }
}

fn site_cfg(app: &App, ws: &str, site: &str) -> Result<SiteConfig, ApiError> {
    check_ws(app, ws)?;
    app.config.site(site).cloned().ok_or_else(|| ApiError::not_found("Site not found."))
}

fn repo_path(app: &App, ws: &str, repo: &str) -> Result<std::path::PathBuf, ApiError> {
    check_ws(app, ws)?;
    if !app.config.workspace.repos.iter().any(|r| r == repo) {
        return Err(ApiError::not_found("Repository not found."));
    }
    Ok(app.config.workspace.root.join(repo))
}

// ---- bootstrap / onboarding / workspaces ----

async fn bootstrap(State(app): S, Extension(caller): Extension<Caller>) -> Response {
    let origin = caller.origin();
    data(
        StatusCode::OK,
        Bootstrap {
            version: "1",
            backend: "rust",
            capabilities: vec![
                "dev-sessions",
                "component-delete",
                "component-generate",
                "git",
                "component-trash",
                "component-source",
                "registry-rebuild",
                "services",
                "git-remote",
                "prod-releases",
                "llm-request",
            ],
            dev_origins: app.config.sites.iter().map(|s| origin.origin_for_port(s.slot_port)).collect(),
            workspace_id: app.config.workspace.id.clone(),
            site_id: app.config.sites.first().map(|s| s.id.clone()).unwrap_or_default(),
            agent: AgentInfo { model: app.config.agent.model.clone(), reasoning_effort: app.config.agent.reasoning_effort.clone() },
        },
    )
}

async fn onboarding(State(app): S) -> Response {
    data(
        StatusCode::OK,
        OnboardingState {
            version: 1,
            status: "completed",
            workspace_id: Some(app.config.workspace.id.clone()),
            site_id: app.config.sites.first().map(|s| s.id.clone()),
            updated_at: now_rfc3339(),
        },
    )
}

fn workspace_summary(app: &App) -> WorkspaceSummary {
    let root = &app.config.workspace.root;
    let available = root.join("apps").is_dir();
    let created = std::fs::metadata(root).and_then(|m| m.created().or_else(|_| m.modified())).ok().map(|t| crate::util::format_time(t.into()));
    WorkspaceSummary {
        id: app.config.workspace.id.clone(),
        name: app.config.workspace.name.clone(),
        root: root.display().to_string(),
        available,
        problem: (!available).then(|| "The workspace root is unavailable.".to_string()),
        created_at: created.clone().unwrap_or_else(now_rfc3339),
        last_opened_at: now_rfc3339(),
    }
}

async fn workspaces(State(app): S) -> Response {
    data(StatusCode::OK, vec![workspace_summary(&app)])
}

fn inspect_site(app: &App, cfg: &SiteConfig) -> Site {
    let root = app.config.app_dir(&cfg.id);
    let pkg: Value = std::fs::read(root.join("package.json")).ok().and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or(Value::Null);
    let obj = |k: &str| pkg.get(k).and_then(Value::as_object).cloned().unwrap_or_default();
    let lockfiles = ["bun.lock", "bun.lockb", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"]
        .iter()
        .filter(|f| root.join(f).is_file())
        .map(|f| f.to_string())
        .collect();
    let mut warnings = vec![];
    if !root.join("package.json").is_file() {
        warnings.push(Warning { code: "package_missing".into(), message: "package.json is missing.".into() });
    }
    Site {
        id: cfg.id.clone(),
        name: cfg.id.clone(),
        root: root.display().to_string(),
        package_name: pkg.get("name").and_then(Value::as_str).map(str::to_string),
        package_manager: Some("bun".into()),
        modules: vec![],
        scripts: obj("scripts"),
        dependencies: obj("dependencies"),
        dev_dependencies: obj("devDependencies"),
        dependencies_installed: root.join("node_modules").is_dir(),
        lockfiles,
        warnings,
    }
}

async fn workspace(State(app): S, Path(ws): Path<String>) -> ApiResult {
    check_ws(&app, &ws)?;
    let s = workspace_summary(&app);
    let layers_root = app.config.workspace.root.join("layers");
    Ok(data(
        StatusCode::OK,
        WorkspaceDetail {
            id: s.id,
            name: s.name,
            root: s.root,
            available: s.available,
            problem: s.problem,
            sites: app.config.sites.iter().map(|c| inspect_site(&app, c)).collect(),
            layers: if layers_root.is_dir() { vec![Layer { id: "layers".into(), name: "layers".into(), root: layers_root.display().to_string() }] } else { vec![] },
            warnings: vec![],
        },
    ))
}

async fn components(State(app): S, Path((ws, site)): Path<(String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    let dir = app.config.app_dir(&cfg.id);
    let reg = tokio::task::spawn_blocking({
        let dir = dir.clone();
        move || crate::workspace::inspect_registry(&dir)
    })
    .await
    .map_err(|e| ApiError::internal(e.to_string()))?;
    let can_rebuild = std::fs::read(dir.join("package.json"))
        .ok()
        .and_then(|b| serde_json::from_slice::<Value>(&b).ok())
        .is_some_and(|p| p["scripts"]["registry:build"].is_string());
    Ok(data(StatusCode::OK, ComponentRegistryResponse { status: reg.status, components: reg.components, problem: reg.problem, can_rebuild }))
}

// ---- dev sessions ----

async fn dev_view(State(app): S, Extension(caller): Extension<Caller>, Path((ws, site)): Path<(String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::OK, app.dev.view(&cfg.id, &caller.origin())))
}

async fn dev_action(app: Arc<App>, caller: Caller, ws: String, site: String, action: &'static str) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    let result = match action {
        "start" => app.dev.start(&cfg.id).await,
        "stop" => app.dev.stop(&cfg.id).await,
        _ => app.dev.restart(&cfg.id).await,
    };
    result.map_err(|e| ApiError::new("process", format!("Could not {action} the dev server: {e:#}")))?;
    Ok(data(StatusCode::ACCEPTED, app.dev.view(&cfg.id, &caller.origin())))
}

async fn dev_start(State(app): S, Extension(c): Extension<Caller>, Path((ws, site)): Path<(String, String)>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    dev_action(app, c, ws, site, "start").await
}
async fn dev_stop(State(app): S, Extension(c): Extension<Caller>, Path((ws, site)): Path<(String, String)>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    dev_action(app, c, ws, site, "stop").await
}
async fn dev_restart(State(app): S, Extension(c): Extension<Caller>, Path((ws, site)): Path<(String, String)>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    dev_action(app, c, ws, site, "restart").await
}

#[derive(Deserialize)]
struct AfterQuery {
    after: Option<String>,
}

/// Serializes a site event for one subscriber: `dev.url` becomes the slot origin as that
/// subscriber reaches it (tunnel callers are never handed the tailnet origin).
pub fn site_event_json(event: &SiteEvent, slot_url: &str) -> String {
    match &event.dev {
        Some(dev) if dev.url != slot_url => {
            let mut e = event.clone();
            if let Some(d) = e.dev.as_mut() {
                d.url = slot_url.to_string();
            }
            serde_json::to_string(&e).unwrap_or_default()
        }
        _ => serde_json::to_string(event).unwrap_or_default(),
    }
}

async fn site_events(
    State(app): S,
    Extension(caller): Extension<Caller>,
    Path((ws, site)): Path<(String, String)>,
    Query(q): Query<AfterQuery>,
    headers: HeaderMap,
) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    let cursor = resume_cursor(headers.get("last-event-id").and_then(|v| v.to_str().ok()), q.after.as_deref());
    let sub = app.hub.subscribe(&cfg.id, cursor);
    let slot_url = caller.origin().origin_for_port(cfg.slot_port);
    Ok(sse_response(move |tx| async move {
        let mut rx = sub.rx;
        let mut last = sub.cursor;
        for e in sub.replay {
            last = e.seq;
            if tx.send(sse_frame(e.seq, "site", &site_event_json(&e, &slot_url))).await.is_err() {
                return;
            }
        }
        loop {
            match rx.recv().await {
                Ok(e) if e.seq > last => {
                    last = e.seq;
                    if tx.send(sse_frame(e.seq, "site", &site_event_json(&e, &slot_url))).await.is_err() {
                        return;
                    }
                }
                Ok(_) => {}
                Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
                Err(_) => return,
            }
        }
    }))
}

// ---- components ----

async fn usage(State(app): S, Path((ws, site, cid)): Path<(String, String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::OK, crate::ops::usage(&app, &cfg, &cid).await?))
}

async fn delete_component(State(app): S, Path((ws, site, cid)): Path<(String, String, String)>, JsonBody(req): JsonBody<DeleteComponentRequest>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    let job = crate::ops::delete_component(app, cfg, cid, req.expected_sha256, req.acknowledge_usage).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

async fn generate(State(app): S, Path((ws, site)): Path<(String, String)>, headers: HeaderMap, JsonBody(req): JsonBody<GenerateRequest>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    let key = headers.get("idempotency-key").and_then(|v| v.to_str().ok()).unwrap_or("").to_string();
    let job = crate::ops::generate_component(app, cfg, req.prompt, key).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

async fn site_pages(State(app): S, Path((ws, site)): Path<(String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::OK, crate::request::pages(&app, &cfg).await?))
}

async fn request_create(State(app): S, Path((ws, site)): Path<(String, String)>, headers: HeaderMap, JsonBody(req): JsonBody<LlmRequest>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    let key = headers.get("idempotency-key").and_then(|v| v.to_str().ok()).unwrap_or("").to_string();
    let job = crate::request::start(app, cfg, req.prompt, req.current_path, key).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

async fn request_apply(State(app): S, Path((ws, site, jid)): Path<(String, String, String)>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::ACCEPTED, crate::request::apply(app, cfg, jid).await?))
}

async fn source(State(app): S, Path((ws, site, cid)): Path<(String, String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::OK, crate::ops::component_source(&app, &cfg, &cid).await?))
}

async fn trash_list(State(app): S, Path((ws, site)): Path<(String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::OK, crate::ops::trash_list(&app, &cfg)?))
}

async fn trash_restore(State(app): S, Path((ws, site, tid)): Path<(String, String, String)>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::ACCEPTED, crate::ops::restore_component(app, cfg, tid).await?))
}

async fn registry_rebuild(State(app): S, Path((ws, site)): Path<(String, String)>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::ACCEPTED, crate::ops::rebuild_registry(app, cfg).await?))
}

// ---- production releases (Phase 3) ----

async fn prod_view(State(app): S, Path((ws, site)): Path<(String, String)>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::OK, crate::release::view(&app, &cfg).await?))
}

async fn prod_deploy(State(app): S, Path((ws, site)): Path<(String, String)>, JsonBody(req): JsonBody<DeployRequest>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::ACCEPTED, crate::release::deploy(app, cfg, req.confirm).await?))
}

async fn prod_rollback(State(app): S, Path((ws, site)): Path<(String, String)>, JsonBody(req): JsonBody<RollbackRequest>) -> ApiResult {
    let cfg = site_cfg(&app, &ws, &site)?;
    Ok(data(StatusCode::ACCEPTED, crate::release::rollback(app, cfg, req.release_id, req.confirm).await?))
}

// ---- services (read-only) ----

async fn services(State(app): S) -> Response {
    data(StatusCode::OK, app.services.list().await)
}

#[derive(Deserialize)]
struct LogsQuery {
    lines: Option<String>,
}

async fn service_logs(State(app): S, Path(unit): Path<String>, Query(q): Query<LogsQuery>) -> ApiResult {
    if !app.services.allowed(&unit) {
        return Err(ApiError::not_found("Service not found."));
    }
    let lines = match q.lines.as_deref() {
        None | Some("") => 200,
        Some(v) => v.parse::<u32>().ok().filter(|n| (1..=1000).contains(n)).ok_or_else(|| ApiError::validation("lines must be 1–1000."))?,
    };
    let logs = app.services.logs(&unit, lines).await.map_err(|e| ApiError::new("unavailable", format!("Could not read the journal: {e:#}")))?;
    Ok(data(StatusCode::OK, logs))
}

// ---- jobs ----

async fn job(State(app): S, Path(id): Path<String>) -> ApiResult {
    Ok(data(StatusCode::OK, app.jobs.snapshot(&id).ok_or_else(|| ApiError::not_found("Job not found."))?))
}

async fn job_cancel(State(app): S, Path(id): Path<String>, JsonBody(_): JsonBody<Empty>) -> ApiResult {
    Ok(data(StatusCode::ACCEPTED, app.jobs.cancel(&id).ok_or_else(|| ApiError::not_found("Job not found or no longer running."))?))
}

async fn job_events(State(app): S, Path(id): Path<String>, Query(q): Query<AfterQuery>, headers: HeaderMap) -> ApiResult {
    let cursor = resume_cursor(headers.get("last-event-id").and_then(|v| v.to_str().ok()), q.after.as_deref());
    let (replay, rx) = app.jobs.subscribe(&id, cursor).ok_or_else(|| ApiError::not_found("Job not found."))?;
    Ok(sse_response(move |tx| async move {
        let mut last = cursor;
        for e in replay {
            last = e.id;
            if tx.send(sse_frame(e.id, "job", &serde_json::to_string(&e).unwrap_or_default())).await.is_err() {
                return;
            }
        }
        let Some(mut rx) = rx else { return };
        let app = app.clone();
        loop {
            tokio::select! {
                msg = rx.recv() => match msg {
                    Ok(e) if e.id > last => {
                        last = e.id;
                        if tx.send(sse_frame(e.id, "job", &serde_json::to_string(&e).unwrap_or_default())).await.is_err() { return }
                    }
                    Ok(_) | Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => {}
                    Err(_) => return,
                },
                _ = tokio::time::sleep(std::time::Duration::from_secs(1)) => {
                    // Close once terminal (the SPA then re-GETs the snapshot).
                    if app.jobs.snapshot(&id).is_none_or(|j| j.status.terminal()) { return }
                }
            }
        }
    }))
}

// ---- git ----

async fn repo_summary(app: &App, id: &str) -> RepoSummary {
    let path = app.config.workspace.root.join(id);
    let (branch, files) = app.git.status(&path).await.unwrap_or_default();
    RepoSummary {
        id: id.to_string(),
        path: path.display().to_string(),
        branch,
        head: app.git.head(&path).await,
        dirty: files.len(),
        has_remote: app.git.has_remote(&path).await,
        remote: remote_state(app, id, &path).await,
    }
}

async fn remote_state(app: &App, id: &str, path: &std::path::Path) -> crate::dto::RemoteState {
    let (upstream, counts) = app.git.upstream(path).await;
    crate::dto::RemoteState {
        upstream,
        ahead: counts.map(|c| c.0),
        behind: counts.map(|c| c.1),
        push_branches: app.config.git_remote(id).map(|r| r.branches.clone()).unwrap_or_default(),
    }
}

async fn repos(State(app): S, Path(ws): Path<String>) -> ApiResult {
    check_ws(&app, &ws)?;
    let mut out = Vec::new();
    for id in &app.config.workspace.repos {
        let path = app.config.workspace.root.join(id);
        if path.join(".git").exists() && app.git.check_config(&path).await.is_ok() {
            out.push(repo_summary(&app, id).await);
        }
    }
    Ok(data(StatusCode::OK, out))
}

async fn repo_status(State(app): S, Path((ws, repo)): Path<(String, String)>) -> ApiResult {
    let path = repo_path(&app, &ws, &repo)?;
    app.git.check_config(&path).await?;
    let (branch, files) = app.git.status(&path).await?;
    let remote = remote_state(&app, &repo, &path).await;
    Ok(data(StatusCode::OK, RepoStatus { id: repo.clone(), branch, head: app.git.head(&path).await, has_remote: app.git.has_remote(&path).await, remote, files }))
}

#[derive(Deserialize)]
struct DiffQuery {
    path: String,
}

async fn repo_diff(State(app): S, Path((ws, repo)): Path<(String, String)>, Query(q): Query<DiffQuery>) -> ApiResult {
    let path = repo_path(&app, &ws, &repo)?;
    app.git.check_config(&path).await?;
    let (_, files) = app.git.status(&path).await?;
    let file = files.iter().find(|f| f.path == q.path).ok_or_else(|| ApiError::not_found("No changes for that path."))?;
    Ok(data(StatusCode::OK, app.git.diff(&path, &file.path, file.untracked).await?))
}

async fn repo_commit(State(app): S, Path((ws, repo)): Path<(String, String)>, JsonBody(req): JsonBody<CommitRequest>) -> ApiResult {
    repo_path(&app, &ws, &repo)?;
    let job = crate::ops::commit(app, repo, req.message, req.paths, req.expected_head).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

async fn repo_fetch(State(app): S, Path((ws, repo)): Path<(String, String)>, JsonBody(_): JsonBody<serde_json::Value>) -> ApiResult {
    repo_path(&app, &ws, &repo)?;
    let job = crate::ops::remote_job(app, repo, crate::ops::RemoteOp::Fetch, None, None).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

async fn repo_pull(State(app): S, Path((ws, repo)): Path<(String, String)>, JsonBody(_): JsonBody<serde_json::Value>) -> ApiResult {
    repo_path(&app, &ws, &repo)?;
    let job = crate::ops::remote_job(app, repo, crate::ops::RemoteOp::Pull, None, None).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

async fn repo_push(State(app): S, Path((ws, repo)): Path<(String, String)>, JsonBody(req): JsonBody<crate::dto::PushRequest>) -> ApiResult {
    repo_path(&app, &ws, &repo)?;
    let job = crate::ops::remote_job(app, repo, crate::ops::RemoteOp::Push, Some(req.branch), Some(req.confirm)).await?;
    Ok(data(StatusCode::ACCEPTED, job))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dev_event(url: &str) -> SiteEvent {
        SiteEvent {
            seq: 3,
            kind: "dev.status",
            time: "t".into(),
            dev: Some(DevSessionView {
                site_id: "s".into(),
                unit: "cf-dev@s.service".into(),
                status: DevStatus::Ready,
                url: url.into(),
                started_at: None,
                memory_bytes: None,
                needs_restart: None,
                problem: None,
                ready_timeout_secs: 180,
            }),
            log: None,
            registry: None,
            job: None,
        }
    }

    #[test]
    fn site_events_rewrite_dev_url_per_subscriber() {
        let e = dev_event("https://h.ts.net:9181");
        let tunnel: serde_json::Value = serde_json::from_str(&site_event_json(&e, "http://localhost:9181")).unwrap();
        assert_eq!(tunnel["dev"]["url"], "http://localhost:9181");
        let tailnet: serde_json::Value = serde_json::from_str(&site_event_json(&e, "https://h.ts.net:9181")).unwrap();
        assert_eq!(tailnet["dev"]["url"], "https://h.ts.net:9181");
        let other = SiteEvent { kind: "log", dev: None, ..dev_event("x") };
        assert!(site_event_json(&other, "http://localhost:9181").contains("\"type\":\"log\""));
    }
}
