//! Dev sessions (contract §2.2, §5): one `cf-dev@<site>` unit per site, a per-start control
//! token written to `/run/couchfusion/dev/<site>.env`, readiness via the dev server's control
//! ping (fallback: HTTP 200 on `/`), status monitoring and coalesced log streaming.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use axum::body::Body;
use http_body_util::BodyExt;
use hyper_util::client::legacy::Client;
use hyper_util::client::legacy::connect::HttpConnector;

use crate::config::{Config, SiteConfig};
use crate::dto::{ActivityEvent, DevSessionView, DevStatus, NeedsRestart};
use crate::events::SiteHub;
use crate::store::{DevSessionRow, Store};
use crate::units::Units;
use crate::util::{Redactor, now_rfc3339, random_token};

pub type HttpClient = Client<HttpConnector, Body>;

#[derive(Debug, Clone)]
struct DevState {
    status: DevStatus,
    token: String,
    started_at: Option<String>,
    needs_restart: Option<NeedsRestart>,
    problem: Option<String>,
    memory: Option<u64>,
    /// Bumped on every start so stale readiness tasks stop.
    generation: u64,
}

pub struct DevManager {
    config: Config,
    store: Arc<Store>,
    hub: Arc<SiteHub>,
    units: Arc<Units>,
    client: HttpClient,
    states: Mutex<HashMap<String, DevState>>,
}

/// How the caller reached us, used to rewrite slot URLs per caller (contract §2.2 `url`).
#[derive(Debug, Clone)]
pub struct CallerOrigin {
    pub tailnet: bool,
    /// Host name without port (tunnel: `localhost` or `127.0.0.1`).
    pub host: String,
}

impl CallerOrigin {
    pub fn origin_for_port(&self, port: u16) -> String {
        if self.tailnet { format!("https://{}:{port}", self.host) } else { format!("http://{}:{port}", self.host) }
    }
}

impl DevManager {
    pub fn new(config: Config, store: Arc<Store>, hub: Arc<SiteHub>, units: Arc<Units>, client: HttpClient) -> Arc<Self> {
        Arc::new(Self { config, store, hub, units, client, states: Mutex::new(HashMap::new()) })
    }

    fn site(&self, id: &str) -> Option<&SiteConfig> {
        self.config.site(id)
    }

    fn state(&self, site: &str) -> DevState {
        let mut states = self.states.lock().expect("dev lock");
        states
            .entry(site.to_string())
            .or_insert_with(|| {
                let row = self.store.dev_session(site).ok().flatten();
                DevState {
                    status: DevStatus::Stopped,
                    token: row.as_ref().map(|r| r.token.clone()).unwrap_or_default(),
                    started_at: row.as_ref().and_then(|r| r.started_at.clone()),
                    needs_restart: row.and_then(|r| r.needs_restart),
                    problem: None,
                    memory: None,
                    generation: 0,
                }
            })
            .clone()
    }

    fn update(&self, site: &str, f: impl FnOnce(&mut DevState)) {
        let changed = {
            let mut states = self.states.lock().expect("dev lock");
            let Some(state) = states.get_mut(site) else { return };
            let before = (state.status, state.problem.clone(), state.needs_restart.clone(), state.memory.map(|m| m / (16 << 20)));
            f(state);
            let after = (state.status, state.problem.clone(), state.needs_restart.clone(), state.memory.map(|m| m / (16 << 20)));
            let _ = self.store.save_dev_session(
                site,
                &DevSessionRow {
                    status: state.status,
                    token: state.token.clone(),
                    started_at: state.started_at.clone(),
                    needs_restart: state.needs_restart.clone(),
                },
            );
            before != after
        };
        if changed {
            // Stored with the tailnet URL; the site SSE handler rewrites `dev.url` per subscriber.
            let caller = CallerOrigin { tailnet: true, host: self.config.server.public_host.clone() };
            if let Some(view) = self.view(site, &caller) {
                self.hub.dev_status(site, view);
            }
        }
    }

    pub fn view(&self, site: &str, caller: &CallerOrigin) -> Option<DevSessionView> {
        let cfg = self.site(site)?;
        let s = self.state(site);
        Some(DevSessionView {
            site_id: site.to_string(),
            unit: cfg.dev_unit.clone(),
            status: s.status,
            url: caller.origin_for_port(cfg.slot_port),
            started_at: s.started_at,
            memory_bytes: s.memory,
            needs_restart: s.needs_restart,
            problem: s.problem,
        })
    }

    pub fn status(&self, site: &str) -> DevStatus {
        self.state(site).status
    }

    /// The current control token, if a session was started by this server.
    pub fn token(&self, site: &str) -> Option<String> {
        let t = self.state(site).token;
        (!t.is_empty()).then_some(t)
    }

    pub fn set_needs_restart(&self, site: &str, reason: String) {
        self.state(site);
        self.update(site, |s| s.needs_restart = Some(NeedsRestart { reason }));
    }

    fn write_env(&self, cfg: &SiteConfig, token: &str) -> anyhow::Result<()> {
        let path = self.config.dev_env_file(&cfg.id);
        if let Some(dir) = path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        let host = &self.config.server.public_host;
        let control = self.config.server.control_port;
        let body = format!(
            "# Written by couchfusion-server on each dev start. Do not edit.\nCF_DEV_PORT={dev}\nCOUCHFUSION_CONTROL_TOKEN={token}\nCOUCHFUSION_PUBLIC_ORIGIN=https://{host}:{slot}\nNUXT_PUBLIC_CONTENT_CONTROL_PLANE_ORIGINS=https://{host}:{control},http://localhost:{control}\nCOUCHFUSION_AGENT_MARKER={marker}\n",
            dev = cfg.dev_port,
            slot = cfg.slot_port,
            marker = self.config.agent_marker(&cfg.id).display(),
        );
        let tmp = path.with_extension("env.tmp");
        {
            use std::os::unix::fs::OpenOptionsExt;
            let mut f = std::fs::OpenOptions::new().create(true).truncate(true).write(true).mode(0o600).open(&tmp)?;
            std::io::Write::write_all(&mut f, body.as_bytes())?;
        }
        std::fs::rename(&tmp, &path)?;
        Ok(())
    }

    pub async fn start(self: &Arc<Self>, site: &str) -> anyhow::Result<()> {
        let cfg = self.site(site).cloned().ok_or_else(|| anyhow::anyhow!("unknown site"))?;
        let unit_state = self.units.state(&cfg.dev_unit).await?;
        if unit_state.running() && matches!(self.status(site), DevStatus::Ready | DevStatus::Starting) {
            return Ok(());
        }
        self.launch(&cfg, false).await
    }

    pub async fn restart(self: &Arc<Self>, site: &str) -> anyhow::Result<()> {
        let cfg = self.site(site).cloned().ok_or_else(|| anyhow::anyhow!("unknown site"))?;
        self.launch(&cfg, true).await
    }

    async fn launch(self: &Arc<Self>, cfg: &SiteConfig, restart: bool) -> anyhow::Result<()> {
        let token = random_token();
        self.write_env(cfg, &token)?;
        self.state(&cfg.id);
        let mut generation = 0;
        self.update(&cfg.id, |s| {
            s.status = DevStatus::Starting;
            s.token = token.clone();
            s.started_at = Some(now_rfc3339());
            s.problem = None;
            s.needs_restart = None;
            s.generation += 1;
            generation = s.generation;
        });
        let result = if restart { self.units.restart(&cfg.dev_unit).await } else { self.units.start(&cfg.dev_unit).await };
        if let Err(err) = result {
            self.update(&cfg.id, |s| {
                s.status = DevStatus::Failed;
                s.problem = Some(format!("{err:#}"));
            });
            return Err(err);
        }
        let this = self.clone();
        let cfg = cfg.clone();
        tokio::spawn(async move { this.await_ready(cfg, generation).await });
        Ok(())
    }

    pub async fn stop(self: &Arc<Self>, site: &str) -> anyhow::Result<()> {
        let cfg = self.site(site).cloned().ok_or_else(|| anyhow::anyhow!("unknown site"))?;
        self.state(site);
        self.update(site, |s| {
            s.status = DevStatus::Stopping;
            s.generation += 1;
        });
        let result = self.units.stop(&cfg.dev_unit).await;
        self.update(site, |s| {
            s.status = if result.is_ok() { DevStatus::Stopped } else { DevStatus::Failed };
            s.problem = result.as_ref().err().map(|e| format!("{e:#}"));
            s.memory = None;
        });
        result
    }

    /// Polls every 250 ms until the dev server answers or the timeout elapses.
    async fn await_ready(self: Arc<Self>, cfg: SiteConfig, generation: u64) {
        let deadline = tokio::time::Instant::now() + Duration::from_secs(self.config.server.dev_ready_timeout_secs);
        let mut ticks = 0u32;
        loop {
            if self.state(&cfg.id).generation != generation {
                return;
            }
            if self.probe_ready(&cfg).await {
                self.update(&cfg.id, |s| {
                    if s.generation == generation {
                        s.status = DevStatus::Ready;
                    }
                });
                return;
            }
            ticks += 1;
            if ticks.is_multiple_of(8)
                && let Ok(state) = self.units.state(&cfg.dev_unit).await
                && !state.running()
            {
                self.update(&cfg.id, |s| {
                    if s.generation == generation {
                        s.status = DevStatus::Failed;
                        s.problem = Some(format!("{} exited ({})", cfg.dev_unit, state.result));
                    }
                });
                return;
            }
            if tokio::time::Instant::now() > deadline {
                self.update(&cfg.id, |s| {
                    if s.generation == generation {
                        s.status = DevStatus::Failed;
                        s.problem = Some("The dev server did not become ready in time.".into());
                    }
                });
                return;
            }
            tokio::time::sleep(Duration::from_millis(250)).await;
        }
    }

    /// Control ping first (needs the token), then any 200 on `/`.
    pub async fn probe_ready(&self, cfg: &SiteConfig) -> bool {
        if let Some(token) = self.token(&cfg.id)
            && let Ok((200, body)) = self.control_get(cfg, "/__couchfusion/control/ping", &token, Duration::from_secs(3)).await
            && body.contains("\"ready\":true")
        {
            return true;
        }
        let uri = format!("http://127.0.0.1:{}/", cfg.dev_port);
        let req = axum::http::Request::get(uri).header("accept", "text/html").body(Body::empty());
        let Ok(req) = req else { return false };
        matches!(tokio::time::timeout(Duration::from_secs(5), self.client.request(req)).await, Ok(Ok(res)) if res.status() == 200)
    }

    /// GET a token-guarded control route on the dev server. Returns (status, body).
    pub async fn control_get(&self, cfg: &SiteConfig, path_and_query: &str, token: &str, timeout: Duration) -> anyhow::Result<(u16, String)> {
        let req = axum::http::Request::get(format!("http://127.0.0.1:{}{}", cfg.dev_port, path_and_query))
            .header("x-couchfusion-control-token", token)
            .header("accept", "application/json")
            .body(Body::empty())?;
        let res = tokio::time::timeout(timeout, self.client.request(req)).await??;
        let status = res.status().as_u16();
        let bytes = tokio::time::timeout(timeout, res.into_body().collect()).await??.to_bytes();
        Ok((status, String::from_utf8_lossy(&bytes).into_owned()))
    }

    /// Boot reconciliation plus a 5 s monitor loop and one log follower per site.
    pub fn spawn_background(self: &Arc<Self>) {
        for cfg in self.config.sites.clone() {
            self.spawn_log_follower(cfg.clone());
            let this = self.clone();
            tokio::spawn(async move {
                // Re-adopt a unit that survived a control-plane restart.
                if let Ok(state) = this.units.state(&cfg.dev_unit).await
                    && state.running()
                {
                    this.state(&cfg.id);
                    let mut generation = 0;
                    this.update(&cfg.id, |s| {
                        s.status = DevStatus::Starting;
                        s.generation += 1;
                        generation = s.generation;
                    });
                    this.clone().await_ready(cfg.clone(), generation).await;
                } else {
                    this.state(&cfg.id);
                    this.update(&cfg.id, |s| s.status = DevStatus::Stopped);
                }
                loop {
                    tokio::time::sleep(Duration::from_secs(5)).await;
                    let Ok(state) = this.units.state(&cfg.dev_unit).await else { continue };
                    this.update(&cfg.id, |s| {
                        s.memory = state.memory;
                        match (s.status, state.running()) {
                            (DevStatus::Ready | DevStatus::Starting, false) => {
                                s.status = if state.result == "success" { DevStatus::Stopped } else { DevStatus::Failed };
                                if s.status == DevStatus::Failed {
                                    s.problem = Some(format!("{} stopped ({})", cfg.dev_unit, state.result));
                                }
                            }
                            (DevStatus::Stopped | DevStatus::Failed, true) => {
                                // Started outside the control plane (e.g. `systemctl start` by root).
                                s.status = DevStatus::Starting;
                            }
                            _ => {}
                        }
                    });
                    if this.status(&cfg.id) == DevStatus::Starting && this.probe_ready(&cfg).await {
                        this.update(&cfg.id, |s| {
                            if s.status == DevStatus::Starting {
                                s.status = DevStatus::Ready;
                            }
                        });
                    }
                }
            });
        }
    }

    /// Coalesces dev logs into at most 4 site events per second.
    fn spawn_log_follower(self: &Arc<Self>, cfg: SiteConfig) {
        let this = self.clone();
        tokio::spawn(async move {
            let mut rx = this.units.follow_logs(&cfg.dev_unit);
            let mut next_id: u64 = 1;
            loop {
                let Some(first) = rx.recv().await else { return };
                let mut lines = vec![first];
                let window = tokio::time::sleep(Duration::from_millis(250));
                tokio::pin!(window);
                loop {
                    tokio::select! {
                        _ = &mut window => break,
                        next = rx.recv() => match next {
                            Some(l) => { lines.push(l); if lines.len() >= 200 { break } }
                            None => break,
                        }
                    }
                }
                let token = this.token(&cfg.id).unwrap_or_default();
                let redactor = Redactor::new(vec![token]);
                let stderr = lines.iter().any(|l| l.priority <= 3);
                let text = lines.iter().map(|l| redactor.redact(&l.message)).collect::<Vec<_>>().join("\n");
                this.hub.log(
                    &cfg.id,
                    ActivityEvent {
                        id: next_id,
                        time: now_rfc3339(),
                        code: "dev.log".into(),
                        text,
                        level: None,
                        stream: Some(if stderr { "stderr".into() } else { "stdout".into() }),
                    },
                );
                next_id += 1;
            }
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn caller_origins() {
        let t = CallerOrigin { tailnet: true, host: "hetzner1.tailb7471b.ts.net".into() };
        assert_eq!(t.origin_for_port(9181), "https://hetzner1.tailb7471b.ts.net:9181");
        let l = CallerOrigin { tailnet: false, host: "localhost".into() };
        assert_eq!(l.origin_for_port(9181), "http://localhost:9181");
    }
}
