//! Listeners, guards (contract §3), SPA serving (spec §4.3) and the JSON body extractor.

use std::convert::Infallible;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use axum::Router;
use axum::body::{Body, Bytes};
use axum::extract::{FromRequest, Query, Request, State};
use axum::http::{HeaderMap, HeaderValue, Method, StatusCode, header};
use axum::middleware::{self, Next};
use axum::response::{IntoResponse, Response};
use axum::routing::get;
use futures_util::stream;
use serde::de::DeserializeOwned;
use tokio::sync::mpsc;

use crate::config::{Config, SiteConfig};
use crate::dev::{CallerOrigin, DevManager, HttpClient};
use crate::error::ApiError;
use crate::events::{SSE_KEEPALIVE, SiteHub};
use crate::git::Git;
use crate::jobs::Jobs;
use crate::store::Store;
use crate::units::Units;
use crate::util::random_token;

pub const SESSION_COOKIE: &str = "cf_cp_s";
const MAX_BODY: usize = 1024 * 1024;

pub struct App {
    pub config: Config,
    pub store: Arc<Store>,
    pub hub: Arc<SiteHub>,
    pub jobs: Arc<Jobs>,
    pub units: Arc<Units>,
    pub dev: Arc<DevManager>,
    pub git: Git,
    pub client: HttpClient,
    pub services: crate::services::Services,
}

impl App {
    pub fn new(config: Config) -> anyhow::Result<Arc<Self>> {
        let store = Arc::new(Store::open(&config.db_path())?);
        let hub = Arc::new(SiteHub::default());
        let jobs = Jobs::new(store.clone(), hub.clone());
        let units = Units::new(&config);
        let client: HttpClient = hyper_util::client::legacy::Client::builder(hyper_util::rt::TokioExecutor::new())
            .pool_idle_timeout(std::time::Duration::from_secs(30))
            .build_http();
        let dev = DevManager::new(config.clone(), store.clone(), hub.clone(), units.clone(), client.clone());
        let git = Git { bin: config.toolchain.git.clone() };
        let services = crate::services::Services::new(&config);
        // Generate jobs from a previous process were failed by `Jobs::new`; drop their markers.
        crate::ops::clear_agent_markers(&config);
        Ok(Arc::new(Self { config, store, hub, jobs, units, dev, git, client, services }))
    }

    /// Origins allowed to frame a dev slot (the control plane, tailnet and tunnel).
    /// `frame-ancestors` for slot responses. `'self'` is required because the builder (on the
    /// slot origin) embeds its own live-preview iframe of the same site, and CSP checks every
    /// ancestor in the chain: builder (slot) → control plane.
    pub fn frame_ancestors(&self) -> String {
        let p = self.config.server.control_port;
        format!("'self' https://{}:{p} http://localhost:{p} http://127.0.0.1:{p}", self.config.server.public_host)
    }

    fn slot_origins(&self) -> Vec<String> {
        self.config
            .sites
            .iter()
            .flat_map(|s| {
                [
                    format!("https://{}:{}", self.config.server.public_host, s.slot_port),
                    format!("http://localhost:{}", s.slot_port),
                    format!("http://127.0.0.1:{}", s.slot_port),
                ]
            })
            .collect()
    }
}

/// Which listener a request arrived on.
#[derive(Debug, Clone, Copy)]
pub struct ListenerInfo {
    pub tailnet: bool,
    pub port: u16,
    pub control: bool,
}

/// The authenticated caller, set by the guard.
#[derive(Debug, Clone)]
pub struct Caller {
    pub tailnet: bool,
    /// Host without port.
    pub host: String,
    /// `host:port` as the browser sees it.
    pub authority: String,
    pub port: u16,
    pub login: Option<String>,
}

impl Caller {
    pub fn origin(&self) -> CallerOrigin {
        CallerOrigin { tailnet: self.tailnet, host: self.host.clone() }
    }
    pub fn own_origin(&self) -> String {
        format!("{}://{}", if self.tailnet { "https" } else { "http" }, self.authority)
    }
}

/// Validates the authority (Host on TCP, `X-Forwarded-Host` on the tailscale unix socket).
pub fn allowed_authority(config: &Config, info: ListenerInfo, authority: &str) -> Option<String> {
    let authority = authority.trim().to_ascii_lowercase();
    let (host, port) = authority.rsplit_once(':')?;
    if port.parse::<u16>().ok()? != info.port {
        return None;
    }
    let ok = if info.tailnet { host == config.server.public_host.to_ascii_lowercase() } else { host == "localhost" || host == "127.0.0.1" };
    ok.then(|| host.to_string())
}

/// `Sec-Fetch-Site` and `Origin` checks for mutations (contract §3).
pub fn mutation_allowed(headers: &HeaderMap, own_origin: &str) -> bool {
    if let Some(site) = headers.get("sec-fetch-site").and_then(|v| v.to_str().ok())
        && !matches!(site, "same-origin" | "none")
    {
        return false;
    }
    match headers.get(header::ORIGIN).and_then(|v| v.to_str().ok()) {
        Some(origin) => origin.eq_ignore_ascii_case(own_origin),
        None => true,
    }
}

fn cookie_value(headers: &HeaderMap, name: &str) -> Option<String> {
    headers.get_all(header::COOKIE).iter().filter_map(|v| v.to_str().ok()).flat_map(|v| v.split(';')).find_map(|c| {
        let (k, v) = c.trim().split_once('=')?;
        (k == name).then(|| v.to_string())
    })
}

fn unauthorized(api: bool, tailnet: bool) -> Response {
    let hint = if tailnet {
        "This tailnet login is not allowed on the CouchFusion control plane."
    } else {
        "Not signed in. Run `sudo -u cfcp couchfusion-server login-url` on the VPS and open the printed link."
    };
    if api {
        ApiError::unauthorized(hint).into_response()
    } else {
        let mut res = (StatusCode::UNAUTHORIZED, format!("<!doctype html><meta charset=utf-8><title>Sign in</title><p style=\"font:15px system-ui;padding:2rem\">{hint}</p>")).into_response();
        res.headers_mut().insert(header::CONTENT_TYPE, HeaderValue::from_static("text/html; charset=utf-8"));
        res
    }
}

/// Host allowlist, authentication, mutation checks and audit for every listener.
pub async fn guard(State(app): State<Arc<App>>, mut req: Request, next: Next) -> Response {
    let info = *req.extensions().get::<ListenerInfo>().expect("listener info");
    let path = req.uri().path().to_string();
    let api = info.control && path.starts_with("/api/");
    let authority_header = if info.tailnet { req.headers().get("x-forwarded-host") } else { req.headers().get(header::HOST) };
    let Some(host) = authority_header.and_then(|v| v.to_str().ok()).and_then(|a| allowed_authority(&app.config, info, a)) else {
        return if api { ApiError::forbidden("Host not allowed.").into_response() } else { (StatusCode::FORBIDDEN, "Host not allowed.").into_response() };
    };
    let authority = format!("{host}:{}", info.port);

    let login = if info.tailnet {
        let login = req.headers().get("tailscale-user-login").and_then(|v| v.to_str().ok()).map(str::to_string);
        match login {
            Some(l) if app.config.auth.allowed_logins.iter().any(|a| a.eq_ignore_ascii_case(&l)) => Some(l),
            _ => return unauthorized(api, true),
        }
    } else {
        let exchange = info.control && path == "/auth/exchange";
        let valid = cookie_value(req.headers(), SESSION_COOKIE).is_some_and(|t| app.store.session_valid(&t).unwrap_or(false));
        if !valid && !exchange {
            return unauthorized(api, false);
        }
        None
    };
    let caller = Caller { tailnet: info.tailnet, host, authority, port: info.port, login };

    let method = req.method().clone();
    let mutation = matches!(method, Method::POST | Method::PUT | Method::PATCH | Method::DELETE);
    if api && mutation {
        if !mutation_allowed(req.headers(), &caller.own_origin()) {
            return ApiError::forbidden("Cross-origin request refused.").into_response();
        }
        let json = req
            .headers()
            .get(header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .is_some_and(|ct| ct.split(';').next().is_some_and(|t| t.trim().eq_ignore_ascii_case("application/json")));
        if !json {
            return ApiError::new("unsupported_media_type", "Requests must use application/json.").into_response();
        }
    }
    let via = if caller.tailnet { "tailnet" } else { "tunnel" };
    let login_for_audit = caller.login.clone();
    req.extensions_mut().insert(caller);
    let res = next.run(req).await;
    if api && mutation {
        app.store.audit(via, login_for_audit.as_deref(), method.as_str(), &path, res.status().as_u16());
        tracing::info!(target: "audit", via, login = login_for_audit.as_deref().unwrap_or("-"), method = %method, path, status = res.status().as_u16());
    }
    res
}

#[derive(serde::Deserialize)]
struct ExchangeQuery {
    token: Option<String>,
}

async fn auth_exchange(State(app): State<Arc<App>>, axum::Extension(info): axum::Extension<ListenerInfo>, Query(q): Query<ExchangeQuery>) -> Response {
    if info.tailnet {
        return (StatusCode::NOT_FOUND, "Not found").into_response();
    }
    let session = random_token();
    match q.token.map(|t| app.store.exchange_login_token(&t, &session)) {
        Some(Ok(true)) => {
            let mut res = StatusCode::SEE_OTHER.into_response();
            let h = res.headers_mut();
            h.insert(header::LOCATION, HeaderValue::from_static("/"));
            h.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
            let cookie = format!("{SESSION_COOKIE}={session}; HttpOnly; SameSite=Strict; Path=/; Max-Age=2592000");
            h.insert(header::SET_COOKIE, HeaderValue::from_str(&cookie).expect("ascii cookie"));
            res
        }
        _ => unauthorized(false, false),
    }
}

// ---- JSON body extractor: `{}` accepted, errors in the API envelope ----

pub struct JsonBody<T>(pub T);

impl<S: Send + Sync, T: DeserializeOwned> FromRequest<S> for JsonBody<T> {
    type Rejection = ApiError;

    async fn from_request(req: Request, state: &S) -> Result<Self, Self::Rejection> {
        let bytes = Bytes::from_request(req, state).await.map_err(|_| ApiError::new("request_too_large", "Request body is too large."))?;
        let bytes = if bytes.iter().all(u8::is_ascii_whitespace) { Bytes::from_static(b"{}") } else { bytes };
        serde_json::from_slice(&bytes).map(JsonBody).map_err(|e| ApiError::new("invalid_request", format!("Invalid request body: {e}")))
    }
}

// ---- SSE ----

/// Streams frames produced by `producer` until the client disconnects.
pub fn sse_response<F, Fut>(producer: F) -> Response
where
    F: FnOnce(mpsc::Sender<String>) -> Fut + Send + 'static,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    let (tx, rx) = mpsc::channel::<String>(64);
    let keepalive = tx.clone();
    let ka = tokio::spawn(async move {
        loop {
            tokio::time::sleep(std::time::Duration::from_secs(15)).await;
            if keepalive.send(SSE_KEEPALIVE.to_string()).await.is_err() {
                break;
            }
        }
    });
    // When the producer finishes (e.g. a terminal job), stop the keepalive so the stream ends.
    tokio::spawn(async move {
        producer(tx).await;
        ka.abort();
    });
    let body = stream::unfold(rx, |mut rx| async move { rx.recv().await.map(|frame| (Ok::<_, Infallible>(Bytes::from(frame)), rx)) });
    let mut res = Response::new(Body::from_stream(body));
    let h = res.headers_mut();
    h.insert(header::CONTENT_TYPE, HeaderValue::from_static("text/event-stream; charset=utf-8"));
    h.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    h.insert("x-accel-buffering", HeaderValue::from_static("no"));
    res
}

// ---- SPA ----

fn mime(path: &Path) -> &'static str {
    match path.extension().and_then(|e| e.to_str()).unwrap_or("") {
        "html" => "text/html; charset=utf-8",
        "js" | "mjs" => "text/javascript; charset=utf-8",
        "css" => "text/css; charset=utf-8",
        "json" | "map" => "application/json",
        "svg" => "image/svg+xml",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "webp" => "image/webp",
        "ico" => "image/x-icon",
        "woff2" => "font/woff2",
        "woff" => "font/woff",
        "txt" => "text/plain; charset=utf-8",
        _ => "application/octet-stream",
    }
}

/// Resolves a request path inside the SPA directory (no `..`, no hidden files).
pub fn spa_file(spa_dir: &Path, request_path: &str) -> Option<PathBuf> {
    let rel = request_path.trim_start_matches('/');
    if rel.split('/').any(|s| s == ".." || s.starts_with('.')) {
        return None;
    }
    let candidate = if rel.is_empty() { spa_dir.join("index.html") } else { spa_dir.join(rel) };
    let meta = std::fs::symlink_metadata(&candidate).ok()?;
    if meta.is_dir() {
        let index = candidate.join("index.html");
        return index.is_file().then_some(index);
    }
    meta.is_file().then_some(candidate)
}

pub fn inject_nonce(html: &str, nonce: &str) -> String {
    html.replace("<script", &format!("<script nonce=\"{nonce}\""))
}

async fn spa(State(app): State<Arc<App>>, req: Request) -> Response {
    if !matches!(*req.method(), Method::GET | Method::HEAD) {
        return (StatusCode::METHOD_NOT_ALLOWED, "Method not allowed").into_response();
    }
    let path = req.uri().path();
    if path.starts_with("/api/") {
        return ApiError::not_found("Unknown API route.").into_response();
    }
    let dir = &app.config.server.spa_dir;
    let file = match spa_file(dir, path) {
        Some(f) => f,
        None => {
            let last = path.rsplit('/').next().unwrap_or("");
            if last.contains('.') {
                return (StatusCode::NOT_FOUND, "Not found").into_response();
            }
            dir.join("index.html")
        }
    };
    let Ok(bytes) = tokio::fs::read(&file).await else {
        return (StatusCode::NOT_FOUND, "The control-plane UI is not installed (spa_dir).").into_response();
    };
    let mut res;
    if file.extension().is_some_and(|e| e == "html") {
        let nonce = random_token();
        let html = inject_nonce(&String::from_utf8_lossy(&bytes), &nonce);
        res = html.into_response();
        let csp = format!(
            "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; frame-src {}; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'nonce-{nonce}'",
            app.slot_origins().join(" ")
        );
        let h = res.headers_mut();
        h.insert(header::CONTENT_SECURITY_POLICY, HeaderValue::from_str(&csp).expect("ascii csp"));
        h.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-cache"));
        h.insert(header::X_FRAME_OPTIONS, HeaderValue::from_static("DENY"));
    } else {
        res = bytes.into_response();
        let cache = if path.starts_with("/_nuxt/") { "public, max-age=31536000, immutable" } else { "no-cache" };
        res.headers_mut().insert(header::CACHE_CONTROL, HeaderValue::from_static(cache));
    }
    let h = res.headers_mut();
    h.insert(header::CONTENT_TYPE, HeaderValue::from_static(mime(&file)));
    h.insert(header::X_CONTENT_TYPE_OPTIONS, HeaderValue::from_static("nosniff"));
    h.insert(header::REFERRER_POLICY, HeaderValue::from_static("no-referrer"));
    res
}

// ---- routers and listeners ----

fn control_router(app: Arc<App>) -> Router {
    crate::api::routes()
        .route("/auth/exchange", get(auth_exchange))
        .fallback(spa)
        .layer(middleware::from_fn_with_state(app.clone(), guard))
        .layer(axum::extract::DefaultBodyLimit::max(MAX_BODY))
        .with_state(app)
}

fn slot_router(app: Arc<App>, site: SiteConfig) -> Router {
    Router::new()
        .fallback(crate::proxy::slot_handler)
        .with_state(crate::proxy::SlotState { app: app.clone(), site })
        .layer(slot_compression())
        .layer(middleware::from_fn_with_state(app, guard))
}

/// gzip/brotli for slot responses. `nuxt dev` serves hundreds of uncompressed JS/CSS modules;
/// compressing them cuts the packets sent per builder load, which matters most on lossy links.
/// The default predicate already skips images, `text/event-stream` and tiny bodies; WebSocket
/// upgrades (`101`, Vite HMR) are excluded explicitly. Fastest level keeps CPU low on a 2-vCPU box.
fn slot_compression() -> tower_http::compression::CompressionLayer<impl tower_http::compression::Predicate> {
    use tower_http::compression::{CompressionLayer, CompressionLevel, DefaultPredicate, Predicate};
    let not_upgrade = |status: StatusCode, _: axum::http::Version, _: &HeaderMap, _: &axum::http::Extensions| {
        status != StatusCode::SWITCHING_PROTOCOLS
    };
    CompressionLayer::new()
        .quality(CompressionLevel::Fastest)
        .compress_when(DefaultPredicate::new().and(not_upgrade))
}

pub async fn serve(app: Arc<App>) -> anyhow::Result<()> {
    let config = app.config.clone();
    std::fs::create_dir_all(&config.server.run_dir).ok();
    let mut tasks = tokio::task::JoinSet::new();
    for port in config.ports() {
        let control = port == config.server.control_port;
        let base = if control {
            control_router(app.clone())
        } else {
            let site = config.site_by_slot(port).cloned().expect("slot site");
            slot_router(app.clone(), site)
        };
        if config.server.unix_listeners {
            let mut path = config.socket_path(port);
            // sun_path is ~104 bytes on macOS; bind by relative name inside run_dir if needed.
            if path.as_os_str().len() >= 100 {
                std::env::set_current_dir(&config.server.run_dir)?;
                path = PathBuf::from(path.file_name().expect("socket file name"));
            }
            let _ = std::fs::remove_file(&path);
            let listener = tokio::net::UnixListener::bind(&path).map_err(|e| anyhow::anyhow!("binding {}: {e}", path.display()))?;
            {
                use std::os::unix::fs::PermissionsExt;
                std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))?;
            }
            let router = base.clone().layer(axum::Extension(ListenerInfo { tailnet: true, port, control }));
            tracing::info!("listening on unix:{} (tailnet, port {port})", path.display());
            tasks.spawn(async move { axum::serve(listener, router).with_graceful_shutdown(shutdown()).await });
        }
        if config.server.tcp_listeners {
            let listener = tokio::net::TcpListener::bind(("127.0.0.1", port)).await.map_err(|e| anyhow::anyhow!("binding 127.0.0.1:{port}: {e}"))?;
            let router = base.layer(axum::Extension(ListenerInfo { tailnet: false, port, control }));
            tracing::info!("listening on http://127.0.0.1:{port} (tunnel)");
            tasks.spawn(async move { axum::serve(listener, router).with_graceful_shutdown(shutdown()).await });
        }
    }
    app.dev.spawn_background();
    while let Some(result) = tasks.join_next().await {
        result??;
    }
    Ok(())
}

async fn shutdown() {
    let mut term = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()).expect("signal handler");
    tokio::select! {
        _ = tokio::signal::ctrl_c() => {},
        _ = term.recv() => {},
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config() -> Config {
        toml::from_str(include_str!("../../packaging/hetzner1/phase1/server.toml")).unwrap()
    }

    #[test]
    fn authority_allowlist() {
        let c = config();
        let tail = ListenerInfo { tailnet: true, port: 9180, control: true };
        let tcp = ListenerInfo { tailnet: false, port: 9180, control: true };
        assert_eq!(allowed_authority(&c, tail, "hetzner1.tailb7471b.ts.net:9180").as_deref(), Some("hetzner1.tailb7471b.ts.net"));
        assert!(allowed_authority(&c, tail, "localhost").is_none());
        assert!(allowed_authority(&c, tail, "hetzner1.tailb7471b.ts.net:9181").is_none());
        assert!(allowed_authority(&c, tail, "evil.example:9180").is_none());
        assert_eq!(allowed_authority(&c, tcp, "localhost:9180").as_deref(), Some("localhost"));
        assert_eq!(allowed_authority(&c, tcp, "127.0.0.1:9180").as_deref(), Some("127.0.0.1"));
        assert!(allowed_authority(&c, tcp, "hetzner1.tailb7471b.ts.net:9180").is_none());
        assert!(allowed_authority(&c, tcp, "rebind.attacker.com:9180").is_none());
    }

    #[test]
    fn mutation_checks() {
        let own = "https://hetzner1.tailb7471b.ts.net:9180";
        let mut h = HeaderMap::new();
        assert!(mutation_allowed(&h, own));
        h.insert("sec-fetch-site", "same-origin".parse().unwrap());
        h.insert("origin", own.parse().unwrap());
        assert!(mutation_allowed(&h, own));
        h.insert("sec-fetch-site", "same-site".parse().unwrap());
        assert!(!mutation_allowed(&h, own), "a dev slot is same-site with the control plane");
        h.insert("sec-fetch-site", "cross-site".parse().unwrap());
        assert!(!mutation_allowed(&h, own));
        h.remove("sec-fetch-site");
        h.insert("origin", "https://hetzner1.tailb7471b.ts.net:9181".parse().unwrap());
        assert!(!mutation_allowed(&h, own));
    }

    #[test]
    fn cookie_parsing() {
        let mut h = HeaderMap::new();
        h.insert("cookie", "a=1; cf_cp_s=tok; b=2".parse().unwrap());
        assert_eq!(cookie_value(&h, "cf_cp_s").as_deref(), Some("tok"));
        assert_eq!(cookie_value(&h, "c"), None);
    }

    #[test]
    fn spa_paths() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        std::fs::write(d.join("index.html"), "<script>x</script>").unwrap();
        std::fs::create_dir_all(d.join("_nuxt")).unwrap();
        std::fs::write(d.join("_nuxt/a.js"), "1").unwrap();
        std::fs::write(d.join(".secret"), "s").unwrap();
        assert_eq!(spa_file(d, "/").unwrap(), d.join("index.html"));
        assert_eq!(spa_file(d, "/_nuxt/a.js").unwrap(), d.join("_nuxt/a.js"));
        assert!(spa_file(d, "/../etc/passwd").is_none());
        assert!(spa_file(d, "/.secret").is_none());
        assert!(spa_file(d, "/workspaces/x").is_none());
        assert_eq!(inject_nonce("<script src=a></script><script>", "N"), "<script nonce=\"N\" src=a></script><script nonce=\"N\">");
    }
}
