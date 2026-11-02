//! `GET /api/v1/workspaces/{ws}/health`: the System page's checks on the VPS (read-only; tools are
//! installed by the packaging scripts, so there is no repair action). Runs as `cfcp`.

use std::path::{Path, PathBuf};
use std::time::Duration;

use axum::body::Body;
use http_body_util::BodyExt;
use serde_json::{Value, json};

use crate::http::App;

const COUCHDB_URL: &str = "http://127.0.0.1:5984/";
const TIMEOUT: Duration = Duration::from_secs(10);

/// Runs `<bin> --version` with a minimal environment; `path_dirs` go on PATH (codex's launcher
/// needs `node`). Returns the first output line.
async fn version(bin: &Path, path_dirs: &[PathBuf]) -> Result<String, String> {
    let mut path = path_dirs.iter().map(|d| d.display().to_string()).collect::<Vec<_>>();
    path.extend(["/usr/bin".into(), "/bin".into()]);
    let mut cmd = tokio::process::Command::new(bin);
    cmd.arg("--version").env_clear().env("PATH", path.join(":")).env("HOME", "/nonexistent").env("LC_ALL", "C").stdin(std::process::Stdio::null()).kill_on_drop(true);
    let out = match tokio::time::timeout(TIMEOUT, cmd.output()).await {
        Err(_) => return Err("did not answer within 10 s".into()),
        Ok(Err(e)) if e.kind() == std::io::ErrorKind::NotFound => return Err("not installed".into()),
        Ok(Err(e)) => return Err(format!("could not start: {e}")),
        Ok(Ok(out)) => out,
    };
    let stdout = String::from_utf8_lossy(&out.stdout);
    let stderr = String::from_utf8_lossy(&out.stderr);
    if !out.status.success() {
        let line = stderr.lines().chain(stdout.lines()).find(|l| !l.trim().is_empty()).unwrap_or("").trim().to_string();
        return Err(format!("exited with {}: {}", out.status.code().unwrap_or(-1), line.chars().take(200).collect::<String>()));
    }
    Ok(stdout.lines().find(|l| !l.trim().is_empty()).unwrap_or("").trim().chars().take(120).collect())
}

fn tool(bin: &Path, result: Result<String, String>) -> Value {
    match result {
        Ok(v) => json!({ "available": true, "version": v, "path": bin.display().to_string() }),
        Err(problem) => json!({ "available": false, "path": bin.display().to_string(), "problem": problem }),
    }
}

async fn couchdb(app: &App) -> Value {
    let req = match axum::http::Request::get(COUCHDB_URL).header("accept", "application/json").body(Body::empty()) {
        Ok(r) => r,
        Err(e) => return json!({ "available": false, "url": COUCHDB_URL, "problem": e.to_string() }),
    };
    let res = match tokio::time::timeout(TIMEOUT, app.client.request(req)).await {
        Err(_) => return json!({ "available": false, "url": COUCHDB_URL, "problem": "did not answer within 10 s" }),
        Ok(Err(e)) => return json!({ "available": false, "url": COUCHDB_URL, "problem": format!("not reachable: {e}") }),
        Ok(Ok(res)) => res,
    };
    let status = res.status().as_u16();
    let body = http_body_util::Limited::new(res.into_body(), 64 * 1024).collect().await.map(|b| b.to_bytes()).unwrap_or_default();
    let v: Value = serde_json::from_slice(&body).unwrap_or(Value::Null);
    match v.get("version").and_then(Value::as_str) {
        Some(version) if status == 200 => json!({ "available": true, "version": version, "url": COUCHDB_URL }),
        _ => json!({ "available": false, "url": COUCHDB_URL, "problem": format!("unexpected answer (HTTP {status})") }),
    }
}

pub async fn health(app: &App) -> Value {
    let t = &app.config.toolchain;
    // The toolchain keeps node next to bun and codex (/opt/couchfusion/toolchain/bin).
    let bin_dir = t.bun.parent().map(Path::to_path_buf).unwrap_or_else(|| PathBuf::from("/opt/couchfusion/toolchain/bin"));
    let node = bin_dir.join("node");
    let codex_dirs = vec![t.codex.parent().map(Path::to_path_buf).unwrap_or_default(), bin_dir.clone()];
    let (git, bun, node_v, codex, couch) = tokio::join!(
        version(&t.git, &[]),
        version(&t.bun, &[]),
        version(&node, &[]),
        version(&t.codex, &codex_dirs),
        couchdb(app),
    );
    let agent = match codex {
        Ok(v) => {
            let model = app.config.agent.model.clone().map(|m| format!("model {m}")).unwrap_or_else(|| "Codex default model".into());
            json!({ "status": "ready", "available": true, "version": v, "accountType": model })
        }
        Err(problem) if problem == "not installed" => json!({ "status": "missing", "available": false, "problem": problem }),
        Err(problem) => json!({ "status": "launch_failed", "available": false, "problem": problem }),
    };
    json!({
        "git": tool(&t.git, git),
        "bun": tool(&t.bun, bun),
        "node": tool(&node, node_v),
        "couchdb": couch,
        "agent": agent,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn version_reports_missing_and_failing_tools() {
        assert_eq!(version(Path::new("/nonexistent/tool"), &[]).await.unwrap_err(), "not installed");
        let tmp = tempfile::tempdir().unwrap();
        let ok = tmp.path().join("ok");
        let bad = tmp.path().join("bad");
        std::fs::write(&ok, "#!/bin/sh\necho 'tool 1.2.3'\n").unwrap();
        std::fs::write(&bad, "#!/bin/sh\necho 'env: node: No such file' >&2\nexit 127\n").unwrap();
        use std::os::unix::fs::PermissionsExt;
        for f in [&ok, &bad] {
            std::fs::set_permissions(f, std::fs::Permissions::from_mode(0o755)).unwrap();
        }
        assert_eq!(version(&ok, &[]).await.unwrap(), "tool 1.2.3");
        assert!(version(&bad, &[]).await.unwrap_err().contains("exited with 127: env: node"));
    }
}
