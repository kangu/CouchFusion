//! Production releases (spec §7.2): snapshot the workspace → build it in `cf-build@<site>` (as
//! cfdev, no secrets) → copy `.output` into `<www>/releases/<id>` → switch `current` → restart
//! `cf-site@<site>` → health check → roll back on failure. Plus manual rollback and the release
//! list. Same layout and steps as `packaging/hydra1/phase3/04-release.sh`.

use std::fs;
use std::io;
use std::os::unix::fs::{DirBuilderExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};

use axum::body::Body;
use serde_json::{Map, Value, json};

use crate::config::{ProdConfig, SiteConfig};
use crate::dto::{Admission, JobSnapshot, ProdView, ReleaseInfo};
use crate::error::ApiError;
use crate::http::App;
use crate::jobs::JobCtx;

/// Names never copied into a snapshot (at any depth), like the rsync excludes in the script.
const SNAPSHOT_EXCLUDES: &[&str] = &["node_modules", ".nuxt", ".output", ".git", "test-results", ".couchfusion"];
const ACTIVE_TIMEOUT: Duration = Duration::from_secs(30);
const HEALTH_TIMEOUT: Duration = Duration::from_secs(60);
const HEALTHY_IN_A_ROW: u32 = 3;
/// Build log lines forwarded into the job (the job keeps the last 200 events).
const MAX_BUILD_LOG_EVENTS: usize = 120;

fn excluded(name: &str) -> bool {
    SNAPSHOT_EXCLUDES.contains(&name) || name.starts_with(".env") || name.starts_with("._")
}

/// Release ids: `<UTC ts>-<sha12>[-dirty]`; accept the safe character set only.
pub fn valid_release_id(id: &str) -> bool {
    !id.is_empty() && id.len() <= 100 && !id.starts_with('-') && id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
}

/// Snapshot directory names: `YYYYMMDDTHHMMSSZ`.
pub fn is_snapshot_name(name: &str) -> bool {
    let b = name.as_bytes();
    b.len() == 16 && b[8] == b'T' && b[15] == b'Z' && b[..8].iter().chain(&b[9..15]).all(u8::is_ascii_digit)
}

fn snapshot_name(now: time::OffsetDateTime) -> String {
    let f = time::macros::format_description!("[year][month][day]T[hour][minute][second]Z");
    now.to_offset(time::UtcOffset::UTC).format(&f).unwrap_or_default()
}

/// `20261031T132216Z-…` → `2026-10-31T13:22:16Z`.
fn created_at(id: &str) -> Option<String> {
    let ts = id.get(..16).filter(|t| is_snapshot_name(t))?;
    Some(format!("{}-{}-{}T{}:{}:{}Z", &ts[0..4], &ts[4..6], &ts[6..8], &ts[9..11], &ts[11..13], &ts[13..15]))
}

fn releases_dir(prod: &ProdConfig) -> PathBuf {
    prod.www_dir.join("releases")
}

/// The release `current` points to.
pub fn current_release(prod: &ProdConfig) -> Option<String> {
    let target = fs::read_link(prod.www_dir.join("current")).ok()?;
    let name = target.strip_prefix("releases").unwrap_or(&target).to_str()?.to_string();
    valid_release_id(&name).then_some(name)
}

/// Releases on disk, newest first.
pub fn list_releases(prod: &ProdConfig, site: &str) -> Vec<ReleaseInfo> {
    let current = current_release(prod);
    let Ok(entries) = fs::read_dir(releases_dir(prod)) else { return vec![] };
    let mut out: Vec<ReleaseInfo> = entries
        .filter_map(Result::ok)
        .filter(|e| e.file_type().is_ok_and(|t| t.is_dir()))
        .filter_map(|e| e.file_name().into_string().ok())
        .filter(|id| valid_release_id(id))
        .map(|id| {
            let meta: Value = fs::read(releases_dir(prod).join(&id).join("release.json"))
                .ok()
                .and_then(|b| serde_json::from_slice(&b).ok())
                .unwrap_or(Value::Null);
            let repo = |key: &str| meta["repos"][key].clone();
            let head = |v: &Value| v["head"].as_str().filter(|h| !h.is_empty() && *h != "unknown").map(str::to_string);
            let app = repo(&format!("apps/{site}"));
            let layers = repo("layers");
            ReleaseInfo {
                created_at: created_at(&id),
                app_head: head(&app),
                app_dirty: app["dirty"].as_bool().unwrap_or(false),
                layers_head: head(&layers),
                layers_dirty: layers["dirty"].as_bool().unwrap_or(false),
                current: current.as_deref() == Some(id.as_str()),
                id,
            }
        })
        .collect();
    out.sort_by(|a, b| b.id.cmp(&a.id));
    out
}

fn mem_available_mib() -> Option<u64> {
    let text = fs::read_to_string("/proc/meminfo").ok()?;
    let line = text.lines().find(|l| l.starts_with("MemAvailable:"))?;
    let kb: u64 = line.split_whitespace().nth(1)?.parse().ok()?;
    Some(kb / 1024)
}

fn disk_free_gib(path: &Path) -> Option<u64> {
    let st = nix::sys::statvfs::statvfs(path).ok()?;
    Some((st.blocks_available() as u64).saturating_mul(st.fragment_size() as u64) / (1024 * 1024 * 1024))
}

pub fn admission(prod: &ProdConfig) -> Admission {
    let memory = mem_available_mib();
    let disk = disk_free_gib(&prod.build_dir);
    let mut problems = vec![];
    if let Some(m) = memory.filter(|m| *m < prod.min_memory_mib) {
        problems.push(format!("{m} MiB of memory free, a build needs {} MiB (stop the dev server first)", prod.min_memory_mib));
    }
    if let Some(d) = disk.filter(|d| *d < prod.min_disk_gib) {
        problems.push(format!("{d} GiB of disk free, a build needs {} GiB", prod.min_disk_gib));
    }
    Admission {
        ok: problems.is_empty(),
        memory_available_mib: memory,
        disk_free_gib: disk,
        problem: (!problems.is_empty()).then(|| problems.join("; ")),
    }
}

pub async fn view(app: &App, site: &SiteConfig) -> Result<ProdView, ApiError> {
    let prod = site.prod.as_ref().ok_or_else(|| ApiError::not_found("This site has no production configured."))?;
    let (releases, adm) = {
        let (prod, id) = (prod.clone(), site.id.clone());
        tokio::task::spawn_blocking(move || (list_releases(&prod, &id), admission(&prod)))
            .await
            .map_err(|e| ApiError::internal(e.to_string()))?
    };
    Ok(ProdView {
        site_id: site.id.clone(),
        unit: prod.unit.clone(),
        port: prod.port,
        service: app.services.show(&prod.unit).await,
        current: releases.iter().find(|r| r.current).map(|r| r.id.clone()),
        releases,
        admission: adm,
        active_job_id: app.jobs.active(&site.id),
    })
}

// ---------------------------------------------------------------- file operations

#[derive(Clone, Copy)]
enum TreeMode {
    /// Workspace → snapshot: cfcp and cfdev (group cfsrc) both write; excludes apply.
    Snapshot,
    /// Build output → release: cfcp writes, the site user (group) reads; nothing excluded.
    Release,
}

impl TreeMode {
    fn dir_mode(self) -> u32 {
        match self {
            TreeMode::Snapshot => 0o770,
            TreeMode::Release => 0o750,
        }
    }
    fn file_mode(self, src: u32) -> u32 {
        let exec = src & 0o111 != 0;
        match (self, exec) {
            (TreeMode::Snapshot, true) => 0o770,
            (TreeMode::Snapshot, false) => 0o660,
            (TreeMode::Release, true) => 0o750,
            (TreeMode::Release, false) => 0o640,
        }
    }
}

fn set_mode(path: &Path, mode: u32) -> io::Result<()> {
    fs::set_permissions(path, fs::Permissions::from_mode(mode))
}

/// Creates a directory with permission bits `perm` (no special bits). The group comes from the
/// setgid parent (`/var/cache/cf-build/<app>` → cfsrc, `<www>/releases` → the site user's
/// group), and the kernel passes the setgid bit on to the new directory. That bit must survive:
/// a chmod would have to repeat it, which systemd's `RestrictSUIDSGID=` refuses, so only a
/// directory without it (a umask stricter than the service's 0002, e.g. in tests) is chmodded.
fn make_dir(path: &Path, perm: u32) -> io::Result<()> {
    fs::DirBuilder::new().mode(perm).create(path)?;
    let mode = fs::metadata(path)?.permissions().mode();
    if mode & 0o777 != perm && mode & 0o2000 == 0 {
        set_mode(path, perm)?;
    }
    Ok(())
}

/// Copies `src` into the new directory `dst` (which must not exist). Returns the file count.
fn copy_tree(src: &Path, dst: &Path, mode: TreeMode) -> io::Result<u64> {
    make_dir(dst, mode.dir_mode())?;
    let mut files = 0;
    for entry in fs::read_dir(src)? {
        let entry = entry?;
        let name = entry.file_name();
        if matches!(mode, TreeMode::Snapshot) && name.to_str().is_none_or(excluded) {
            continue;
        }
        let (from, to) = (entry.path(), dst.join(&name));
        let kind = entry.file_type()?;
        if kind.is_symlink() {
            std::os::unix::fs::symlink(fs::read_link(&from)?, &to)?;
        } else if kind.is_dir() {
            files += copy_tree(&from, &to, mode)?;
        } else if kind.is_file() {
            let src_mode = entry.metadata()?.permissions().mode();
            fs::copy(&from, &to)?;
            set_mode(&to, mode.file_mode(src_mode))?;
            files += 1;
        }
    }
    Ok(files)
}

/// Atomically points `<dir>/<link>` at `target` (symlink to a temp name, then rename).
fn switch_link(dir: &Path, link: &str, target: &str) -> io::Result<()> {
    let tmp = dir.join(format!("{link}.new"));
    let _ = fs::remove_file(&tmp);
    std::os::unix::fs::symlink(target, &tmp)?;
    fs::rename(&tmp, dir.join(link))
}

fn switch_to(prod: &ProdConfig, release: &str) -> io::Result<()> {
    if !releases_dir(prod).join(release).join("server/index.mjs").is_file() {
        return Err(io::Error::other(format!("release {release} has no server/index.mjs")));
    }
    switch_link(&prod.www_dir, "current", &format!("releases/{release}"))
}

/// Removes all but the newest `keep` entries accepted by `select`, never touching `protect`.
fn prune(dir: &Path, keep: usize, select: impl Fn(&str) -> bool, protect: &[String]) -> Vec<String> {
    let Ok(entries) = fs::read_dir(dir) else { return vec![] };
    let mut names: Vec<String> = entries
        .filter_map(Result::ok)
        .filter(|e| e.file_type().is_ok_and(|t| t.is_dir()))
        .filter_map(|e| e.file_name().into_string().ok())
        .filter(|n| select(n))
        .collect();
    names.sort();
    let excess = names.len().saturating_sub(keep);
    names
        .into_iter()
        .take(excess)
        .filter(|n| !protect.contains(n))
        .filter(|n| fs::remove_dir_all(dir.join(n)).is_ok())
        .collect()
}

// ---------------------------------------------------------------- restart and health

async fn probe(app: &App, prod: &ProdConfig) -> Result<u16, String> {
    let uri = format!("http://127.0.0.1:{}{}", prod.port, prod.probe_path);
    let req = axum::http::Request::get(uri).header("accept", "text/html").body(Body::empty()).map_err(|e| e.to_string())?;
    match tokio::time::timeout(Duration::from_secs(10), app.client.request(req)).await {
        Ok(Ok(res)) => Ok(res.status().as_u16()),
        Ok(Err(e)) => Err(e.to_string()),
        Err(_) => Err("timed out".into()),
    }
}

/// Restarts the production unit and waits for 3 consecutive answers below 500.
async fn restart_and_check(app: &App, prod: &ProdConfig, ctx: &JobCtx) -> Result<(), String> {
    ctx.info("release.restart", format!("Restarting {}…", prod.unit));
    app.units.restart(&prod.unit).await.map_err(|e| format!("{e:#}"))?;
    let start = Instant::now();
    loop {
        match app.units.state(&prod.unit).await {
            Ok(state) if state.active == "active" => break,
            Ok(state) if start.elapsed() > ACTIVE_TIMEOUT => return Err(format!("{} is {} after 30 s", prod.unit, state.active)),
            Err(e) if start.elapsed() > ACTIVE_TIMEOUT => return Err(format!("{e:#}")),
            _ => tokio::time::sleep(Duration::from_millis(500)).await,
        }
    }
    let start = Instant::now();
    let (mut healthy, mut last) = (0, String::from("no answer"));
    while start.elapsed() < HEALTH_TIMEOUT {
        match probe(app, prod).await {
            Ok(code) if code < 500 => {
                healthy += 1;
                last = format!("HTTP {code}");
                if healthy >= HEALTHY_IN_A_ROW {
                    ctx.info("release.healthy", format!("Health check: {HEALTHY_IN_A_ROW} × {last} on {}.", prod.probe_path));
                    return Ok(());
                }
            }
            Ok(code) => (healthy, last) = (0, format!("HTTP {code}")),
            Err(e) => (healthy, last) = (0, e),
        }
        tokio::time::sleep(Duration::from_secs(1)).await;
    }
    Err(format!("last answer: {last}"))
}

// ---------------------------------------------------------------- jobs

fn check_confirm(site: &SiteConfig, confirm: &str, what: &str) -> Result<(), ApiError> {
    if confirm != site.id {
        return Err(ApiError::validation(format!("Type {} to confirm the {what}.", site.id)));
    }
    Ok(())
}

pub async fn deploy(app: Arc<App>, site: SiteConfig, confirm: String) -> Result<JobSnapshot, ApiError> {
    let prod = site.prod.clone().ok_or_else(|| ApiError::not_found("This site has no production configured."))?;
    check_confirm(&site, &confirm, "deploy")?;
    let adm = {
        let prod = prod.clone();
        tokio::task::spawn_blocking(move || admission(&prod)).await.map_err(|e| ApiError::internal(e.to_string()))?
    };
    if !adm.ok {
        return Err(ApiError::conflict(format!("Not enough resources to build: {}.", adm.problem.unwrap_or_default())));
    }
    let ctx = app.jobs.create("release.deploy", &app.config.workspace.id, &site.id)?;
    let snapshot = ctx.snapshot();
    tokio::spawn(async move {
        if let Err((err, result)) = run_deploy(&app, &ctx, &site, &prod).await {
            ctx.fail(&err, result);
        }
    });
    Ok(snapshot)
}

type JobFailure = (ApiError, Option<Map<String, Value>>);

fn fs_err(what: &str) -> impl Fn(io::Error) -> JobFailure + '_ {
    move |e| (ApiError::filesystem(format!("{what}: {e}")), None)
}

async fn blocking<T: Send + 'static>(f: impl FnOnce() -> io::Result<T> + Send + 'static) -> io::Result<T> {
    tokio::task::spawn_blocking(f).await.unwrap_or_else(|e| Err(io::Error::other(e.to_string())))
}

async fn repo_state(app: &App, path: &Path) -> Value {
    let head = app.git.head(path).await.map(|h| h.sha).unwrap_or_else(|| "unknown".into());
    let dirty = app.git.status(path).await.map(|(_, files)| !files.is_empty()).unwrap_or(false);
    json!({ "head": head, "dirty": dirty })
}

async fn run_deploy(app: &App, ctx: &JobCtx, site: &SiteConfig, prod: &ProdConfig) -> Result<(), JobFailure> {
    ctx.start();
    let started = Instant::now();
    let ts = snapshot_name(time::OffsetDateTime::now_utc());
    let snap = prod.build_dir.join(&ts);
    let root = app.config.workspace.root.clone();
    let mut repos = vec![format!("apps/{}", site.id)];
    repos.extend(app.config.workspace.repos.iter().filter(|r| !r.starts_with("apps/")).cloned());
    repos.retain(|r| root.join(r).is_dir());

    // 1. Snapshot, so edits (or Codex) during the build cannot change what ships.
    ctx.info("release.snapshot", format!("Copying {} into {}…", repos.join(", "), snap.display()));
    let files = {
        let (root, snap, repos) = (root.clone(), snap.clone(), repos.clone());
        blocking(move || {
            make_dir(&snap, TreeMode::Snapshot.dir_mode())?;
            let mut n = 0;
            for repo in &repos {
                let to = snap.join(repo);
                if let Some(parent) = to.parent().filter(|p| !p.exists()) {
                    make_dir(parent, TreeMode::Snapshot.dir_mode())?;
                }
                n += copy_tree(&root.join(repo), &to, TreeMode::Snapshot)?;
            }
            Ok(n)
        })
        .await
        .map_err(fs_err("Snapshot"))?
    };
    let mut repo_meta = Map::new();
    for repo in &repos {
        repo_meta.insert(repo.clone(), repo_state(app, &root.join(repo)).await);
    }
    let app_head = repo_meta[&repos[0]]["head"].as_str().unwrap_or("unknown").to_string();
    let app_dirty = repo_meta[&repos[0]]["dirty"].as_bool().unwrap_or(false);
    let meta = json!({ "app": site.id, "snapshot": ts, "source": "worktree", "repos": repo_meta });
    let meta_text = serde_json::to_vec_pretty(&meta).unwrap_or_default();
    {
        let (snap, build_dir, ts, meta_text) = (snap.clone(), prod.build_dir.clone(), ts.clone(), meta_text.clone());
        blocking(move || {
            fs::write(snap.join("release.json"), &meta_text)?;
            switch_link(&build_dir, "next", &ts)
        })
        .await
        .map_err(fs_err("Snapshot metadata"))?;
    }
    ctx.info("release.snapshot", format!("Snapshot {ts}: {files} files; app {} {}.", short(&app_head), if app_dirty { "with uncommitted changes" } else { "clean" }));
    if ctx.cancelled() {
        return Err((ApiError::new("cancelled", "Cancelled before the build."), None));
    }

    // 2. Build as cfdev in cf-build@<site>, streaming its log into the job.
    ctx.info("release.build", format!("Building in {} (bun install, nuxt build)…", prod.build_unit));
    let build_started = std::time::SystemTime::now();
    let mut logs = app.units.follow_logs(&prod.build_unit, 0);
    let forward = {
        let ctx = ctx.clone();
        tokio::spawn(async move {
            let mut sent = 0;
            while let Some(line) = logs.recv().await {
                let text = line.message.trim();
                // Skip per-file size listings (Vite chunks, Nitro's output tree).
                if text.is_empty() || text.contains("│ gzip:") || (text.starts_with('ℹ') && text.contains(" kB")) || text.starts_with("├─") || text.starts_with("└─") {
                    continue;
                }
                if sent < MAX_BUILD_LOG_EVENTS {
                    ctx.event(if line.priority <= 3 { "warn" } else { "info" }, "build.log", text);
                    sent += 1;
                }
            }
        })
    };
    let built = app.units.run_oneshot(&prod.build_unit, Duration::from_secs(prod.build_timeout_secs)).await;
    tokio::time::sleep(Duration::from_millis(500)).await;
    forward.abort();
    let build_secs = started.elapsed().as_secs();
    if let Err(e) = built {
        for line in app.units.journal_since(&prod.build_unit, build_started, 15).await {
            ctx.warn("build.log", line);
        }
        let mut r = Map::new();
        r.insert("snapshot".into(), json!(ts));
        return Err((ApiError::new("process", format!("The build failed; production is unchanged. {e:#}")), Some(r)));
    }
    let output = snap.join(&repos[0]).join(".output");
    if !output.join("server/index.mjs").is_file() {
        return Err((ApiError::new("process", "The build finished without .output/server/index.mjs; production is unchanged."), None));
    }
    ctx.info("release.build", format!("Build ok ({build_secs} s)."));

    // 3. Release: copy .output (cfcp-owned, group-readable for the site user).
    let release = format!("{ts}-{}{}", short(&app_head), if app_dirty { "-dirty" } else { "" });
    {
        let (output, dest, meta_text) = (output.clone(), releases_dir(prod).join(&release), meta_text.clone());
        blocking(move || {
            copy_tree(&output, &dest, TreeMode::Release)?;
            fs::write(dest.join("release.json"), &meta_text)?;
            set_mode(&dest.join("release.json"), 0o640)
        })
        .await
        .map_err(fs_err("Copying the release"))?;
    }
    ctx.info("release.copy", format!("Release {release} ready."));

    // 4. Switch, restart, check; roll back to the previous release when unhealthy.
    let previous = current_release(prod);
    switch_to(prod, &release).map_err(fs_err("Switching current"))?;
    ctx.info("release.switch", format!("current → {release}"));
    let mut result = Map::new();
    result.insert("release".into(), json!(release));
    result.insert("buildSeconds".into(), json!(build_secs));
    if let Some(p) = &previous {
        result.insert("previous".into(), json!(p));
    }
    if let Err(why) = restart_and_check(app, prod, ctx).await {
        ctx.warn("release.unhealthy", format!("{release}: {why}"));
        let Some(prev) = previous else {
            return Err((ApiError::new("process", format!("{release} failed its health check ({why}) and there is no previous release.")), Some(result)));
        };
        ctx.warn("release.rollback", format!("Rolling back to {prev}…"));
        switch_to(prod, &prev).map_err(fs_err("Switching back"))?;
        let restored = restart_and_check(app, prod, ctx).await;
        result.insert("rolledBackTo".into(), json!(prev));
        let message = match restored {
            Ok(()) => format!("{release} failed its health check ({why}); rolled back to {prev}."),
            Err(again) => format!("{release} failed its health check ({why}); rolled back to {prev}, which is unhealthy too ({again})."),
        };
        return Err((ApiError::new("process", message), Some(result)));
    }

    // 5. Prune old releases and snapshots (never the current or previous release).
    let protect: Vec<String> = [Some(release.clone()), previous.clone()].into_iter().flatten().collect();
    let (pruned_releases, pruned_snapshots) = {
        let (prod, protect, ts) = (prod.clone(), protect.clone(), ts.clone());
        tokio::task::spawn_blocking(move || {
            (
                prune(&releases_dir(&prod), prod.keep_releases, valid_release_id, &protect),
                prune(&prod.build_dir, prod.keep_snapshots, is_snapshot_name, &[ts]),
            )
        })
        .await
        .unwrap_or_default()
    };
    if !pruned_releases.is_empty() || !pruned_snapshots.is_empty() {
        ctx.info("release.prune", format!("Removed {} old release(s) and {} snapshot(s).", pruned_releases.len(), pruned_snapshots.len()));
    }
    ctx.info("release.deployed", format!("Deployed {release}."));
    ctx.succeed(result);
    Ok(())
}

fn short(sha: &str) -> String {
    sha.chars().take(12).collect()
}

pub async fn rollback(app: Arc<App>, site: SiteConfig, release: String, confirm: String) -> Result<JobSnapshot, ApiError> {
    let prod = site.prod.clone().ok_or_else(|| ApiError::not_found("This site has no production configured."))?;
    check_confirm(&site, &confirm, "rollback")?;
    if !valid_release_id(&release) || !releases_dir(&prod).join(&release).join("server/index.mjs").is_file() {
        return Err(ApiError::not_found("Release not found."));
    }
    if current_release(&prod).as_deref() == Some(release.as_str()) {
        return Err(ApiError::validation(format!("{release} is already live.")));
    }
    let ctx = app.jobs.create("release.rollback", &app.config.workspace.id, &site.id)?;
    let snapshot = ctx.snapshot();
    tokio::spawn(async move {
        ctx.start();
        let previous = current_release(&prod);
        let mut result = Map::new();
        result.insert("release".into(), json!(release));
        if let Some(p) = &previous {
            result.insert("previous".into(), json!(p));
        }
        if let Err(e) = switch_to(&prod, &release) {
            ctx.fail(&ApiError::filesystem(format!("Switching current: {e}")), Some(result));
            return;
        }
        ctx.info("release.switch", format!("current → {release}"));
        match restart_and_check(&app, &prod, &ctx).await {
            Ok(()) => {
                ctx.info("release.rolledback", format!("{release} is live."));
                ctx.succeed(result);
            }
            Err(why) => {
                let message = match previous.as_ref().map(|p| (p, switch_to(&prod, p))) {
                    Some((p, Ok(()))) => {
                        ctx.warn("release.rollback", format!("Switching back to {p}…"));
                        let back = restart_and_check(&app, &prod, &ctx).await;
                        format!("{release} failed its health check ({why}); switched back to {p}{}.", if back.is_ok() { "" } else { ", which is unhealthy too" })
                    }
                    _ => format!("{release} failed its health check ({why})."),
                };
                ctx.fail(&ApiError::new("process", message), Some(result));
            }
        }
    });
    Ok(snapshot)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn prod(dir: &Path) -> ProdConfig {
        toml::from_str(&format!(
            "unit = \"cf-site@x.service\"\nbuild_unit = \"cf-build@x.service\"\nport = 4501\nwww_dir = \"{}\"\nbuild_dir = \"{}\"",
            dir.join("www").display(),
            dir.join("build").display()
        ))
        .unwrap()
    }

    #[test]
    fn ids_and_names() {
        assert!(valid_release_id("20261031T132216Z-e9d443011edf-dirty"));
        assert!(!valid_release_id("../etc"));
        assert!(!valid_release_id("-x"));
        assert!(!valid_release_id(""));
        assert!(is_snapshot_name("20261031T132216Z"));
        assert!(!is_snapshot_name("next"));
        assert_eq!(created_at("20261031T132216Z-abc").as_deref(), Some("2026-10-31T13:22:16Z"));
        assert_eq!(snapshot_name(time::macros::datetime!(2026-10-31 13:22:16 UTC)), "20261031T132216Z");
    }

    #[test]
    fn snapshot_copy_excludes_and_sets_modes() {
        let tmp = tempfile::tempdir().unwrap();
        let src = tmp.path().join("src");
        fs::create_dir_all(src.join("app/node_modules/pkg")).unwrap();
        fs::create_dir_all(src.join("sub/.nuxt")).unwrap();
        fs::write(src.join("app/a.vue"), "a").unwrap();
        fs::write(src.join("app/node_modules/pkg/x.js"), "x").unwrap();
        fs::write(src.join(".env"), "SECRET=1").unwrap();
        fs::write(src.join("._junk"), "").unwrap();
        fs::write(src.join("run.sh"), "#!/bin/sh").unwrap();
        fs::set_permissions(src.join("run.sh"), fs::Permissions::from_mode(0o755)).unwrap();
        std::os::unix::fs::symlink("app/a.vue", src.join("link")).unwrap();
        let dst = tmp.path().join("dst");
        assert_eq!(copy_tree(&src, &dst, TreeMode::Snapshot).unwrap(), 2);
        assert!(dst.join("app/a.vue").is_file());
        assert!(!dst.join("app/node_modules").exists());
        assert!(!dst.join("sub/.nuxt").exists());
        assert!(!dst.join(".env").exists());
        assert!(!dst.join("._junk").exists());
        assert_eq!(fs::read_link(dst.join("link")).unwrap(), PathBuf::from("app/a.vue"));
        assert_eq!(fs::metadata(dst.join("app/a.vue")).unwrap().permissions().mode() & 0o777, 0o660);
        assert_eq!(fs::metadata(dst.join("run.sh")).unwrap().permissions().mode() & 0o777, 0o770);
        assert_eq!(fs::metadata(dst.join("app")).unwrap().permissions().mode() & 0o777, 0o770);
    }

    #[test]
    fn release_copy_keeps_node_modules_and_is_group_readable() {
        let tmp = tempfile::tempdir().unwrap();
        let out = tmp.path().join(".output");
        fs::create_dir_all(out.join("server/node_modules/dep")).unwrap();
        fs::write(out.join("server/index.mjs"), "").unwrap();
        fs::write(out.join("server/node_modules/dep/i.js"), "").unwrap();
        let dst = tmp.path().join("rel");
        assert_eq!(copy_tree(&out, &dst, TreeMode::Release).unwrap(), 2);
        assert!(dst.join("server/node_modules/dep/i.js").is_file());
        assert_eq!(fs::metadata(dst.join("server/index.mjs")).unwrap().permissions().mode() & 0o777, 0o640);
        assert_eq!(fs::metadata(dst.join("server")).unwrap().permissions().mode() & 0o777, 0o750);
    }

    #[test]
    fn switch_list_and_prune() {
        let tmp = tempfile::tempdir().unwrap();
        let p = prod(tmp.path());
        for (id, dirty) in [("20261031T100000Z-aaaaaaaaaaaa", false), ("20261031T110000Z-bbbbbbbbbbbb-dirty", true), ("20261031T120000Z-cccccccccccc", false)] {
            let dir = releases_dir(&p).join(id);
            fs::create_dir_all(dir.join("server")).unwrap();
            fs::write(dir.join("server/index.mjs"), "").unwrap();
            fs::write(
                dir.join("release.json"),
                json!({"repos": {"apps/x": {"head": format!("{}full", &id[17..29]), "dirty": dirty}, "layers": {"head": "unknown", "dirty": false}}}).to_string(),
            )
            .unwrap();
        }
        assert_eq!(current_release(&p), None);
        assert!(switch_to(&p, "nope").is_err());
        switch_to(&p, "20261031T110000Z-bbbbbbbbbbbb-dirty").unwrap();
        assert_eq!(current_release(&p).as_deref(), Some("20261031T110000Z-bbbbbbbbbbbb-dirty"));
        let list = list_releases(&p, "x");
        assert_eq!(list.iter().map(|r| r.id.as_str()).collect::<Vec<_>>(), ["20261031T120000Z-cccccccccccc", "20261031T110000Z-bbbbbbbbbbbb-dirty", "20261031T100000Z-aaaaaaaaaaaa"]);
        assert!(list[1].current && list[1].app_dirty);
        assert_eq!(list[1].app_head.as_deref(), Some("bbbbbbbbbbbbfull"));
        assert_eq!(list[1].layers_head, None);
        assert_eq!(list[0].created_at.as_deref(), Some("2026-10-31T12:00:00Z"));
        // keep 1, but the oldest is protected: only the middle one may go… and it is current.
        let removed = prune(&releases_dir(&p), 1, valid_release_id, &["20261031T100000Z-aaaaaaaaaaaa".into(), "20261031T110000Z-bbbbbbbbbbbb-dirty".into()]);
        assert!(removed.is_empty());
        let removed = prune(&releases_dir(&p), 2, valid_release_id, &["20261031T110000Z-bbbbbbbbbbbb-dirty".into()]);
        assert_eq!(removed, ["20261031T100000Z-aaaaaaaaaaaa"]);
        assert_eq!(list_releases(&p, "x").len(), 2);
    }

    #[test]
    fn admission_reports_without_proc() {
        let tmp = tempfile::tempdir().unwrap();
        let p = prod(tmp.path());
        fs::create_dir_all(&p.build_dir).unwrap();
        let a = admission(&p);
        assert!(a.disk_free_gib.is_some());
        if cfg!(target_os = "linux") {
            assert!(a.memory_available_mib.is_some());
        }
    }
}
