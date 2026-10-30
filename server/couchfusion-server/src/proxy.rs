//! Slot proxy (contract §4): `:918N` → `127.0.0.1:<dev_port>` with header rewriting,
//! WebSocket passthrough (Vite HMR), frame-ancestors for the control plane and a boot page.

use std::sync::Arc;

use axum::body::Body;
use axum::extract::{Request, State};
use axum::http::{HeaderMap, HeaderName, HeaderValue, StatusCode, Uri, header};
use axum::response::{IntoResponse, Response};
use hyper_util::rt::TokioIo;

use crate::config::SiteConfig;
use crate::dto::DevStatus;
use crate::http::{App, Caller};

const HOP_BY_HOP: &[&str] = &["connection", "keep-alive", "proxy-connection", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade"];

#[derive(Clone)]
pub struct SlotState {
    pub app: Arc<App>,
    pub site: SiteConfig,
}

/// Removes the control-plane session cookie from a `Cookie` header, keeping all others.
pub fn strip_cookie(value: &str, name: &str) -> Option<String> {
    let kept: Vec<&str> = value
        .split(';')
        .map(str::trim)
        .filter(|c| !c.is_empty() && c.split('=').next().map(str::trim) != Some(name))
        .collect();
    (!kept.is_empty()).then(|| kept.join("; "))
}

/// Request headers forwarded upstream.
pub fn upstream_headers(incoming: &HeaderMap, dev_port: u16, caller: &Caller, upgrade: bool) -> HeaderMap {
    let mut out = HeaderMap::new();
    for (name, value) in incoming {
        let n = name.as_str();
        if n.starts_with("tailscale-") || n == "host" || n.starts_with("x-forwarded-") || n == "cookie" {
            continue;
        }
        if HOP_BY_HOP.contains(&n) && !(upgrade && (n == "connection" || n == "upgrade")) {
            continue;
        }
        out.append(name.clone(), value.clone());
    }
    for value in incoming.get_all(header::COOKIE) {
        if let Some(kept) = value.to_str().ok().and_then(|v| strip_cookie(v, crate::http::SESSION_COOKIE))
            && let Ok(v) = HeaderValue::from_str(&kept)
        {
            out.append(header::COOKIE, v);
        }
    }
    let host = format!("127.0.0.1:{dev_port}");
    out.insert(header::HOST, HeaderValue::from_str(&host).expect("ascii host"));
    let proto = if caller.tailnet { "https" } else { "http" };
    out.insert(HeaderName::from_static("x-forwarded-proto"), HeaderValue::from_static(proto));
    if let Ok(v) = HeaderValue::from_str(&caller.authority) {
        out.insert(HeaderName::from_static("x-forwarded-host"), v);
    }
    if let Ok(v) = HeaderValue::from_str(&caller.port.to_string()) {
        out.insert(HeaderName::from_static("x-forwarded-port"), v);
    }
    out
}

/// Response headers: hop-by-hop removed, framing allowed only by the control plane.
pub fn adjust_response_headers(headers: &mut HeaderMap, frame_ancestors: &str, upgrade: bool) {
    if !upgrade {
        for h in HOP_BY_HOP {
            headers.remove(*h);
        }
    }
    headers.remove(header::X_FRAME_OPTIONS);
    let directive = format!("frame-ancestors {frame_ancestors}");
    let csp = match headers.get(header::CONTENT_SECURITY_POLICY).and_then(|v| v.to_str().ok()) {
        Some(existing) => {
            let kept: Vec<&str> = existing
                .split(';')
                .map(str::trim)
                .filter(|d| !d.is_empty() && !d.to_ascii_lowercase().starts_with("frame-ancestors"))
                .collect();
            if kept.is_empty() { directive } else { format!("{}; {directive}", kept.join("; ")) }
        }
        None => directive,
    };
    if let Ok(v) = HeaderValue::from_str(&csp) {
        headers.insert(header::CONTENT_SECURITY_POLICY, v);
    }
}

fn wants_html(headers: &HeaderMap) -> bool {
    headers.get(header::ACCEPT).and_then(|v| v.to_str().ok()).is_some_and(|a| a.contains("text/html"))
}

pub fn boot_page(site: &str, status: &str) -> Response {
    let html = format!(
        r#"<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Starting {site}…</title>
<style>body{{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f172a;color:#e2e8f0;font:15px/1.5 system-ui,sans-serif}}main{{text-align:center}}.dot{{display:inline-block;width:10px;height:10px;border-radius:50%;background:#38bdf8;margin-right:8px;animation:p 1s infinite alternate}}@keyframes p{{to{{opacity:.3}}}}code{{color:#94a3b8}}</style></head>
<body><main><p><span class="dot"></span><strong>{site}</strong> dev server is <span id="s">{status}</span>…</p><p><code>This page reloads by itself when it is ready.</code></p></main>
<script>
(function poll(){{fetch('/__cf/slot-status',{{cache:'no-store'}}).then(function(r){{return r.json()}}).then(function(j){{document.getElementById('s').textContent=j.status;if(j.status==='ready'){{location.reload()}}else{{setTimeout(poll,1000)}}}}).catch(function(){{setTimeout(poll,1000)}})}})();
</script></body></html>"#
    );
    let mut res = (StatusCode::SERVICE_UNAVAILABLE, html).into_response();
    let h = res.headers_mut();
    h.insert(header::CONTENT_TYPE, HeaderValue::from_static("text/html; charset=utf-8"));
    h.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    h.insert(header::RETRY_AFTER, HeaderValue::from_static("1"));
    res
}

/// Fallback handler of a slot listener.
pub async fn slot_handler(State(state): State<SlotState>, mut req: Request) -> Response {
    let caller = req.extensions().get::<Caller>().cloned().expect("caller set by guard");
    let site = &state.site;
    let dev_status = state.app.dev.status(&site.id);

    if req.uri().path() == "/__cf/slot-status" {
        let mut res = axum::Json(serde_json::json!({ "status": dev_status.as_str() })).into_response();
        res.headers_mut().insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
        return res;
    }
    let upgrade = req
        .headers()
        .get(header::UPGRADE)
        .and_then(|v| v.to_str().ok())
        .is_some_and(|v| v.eq_ignore_ascii_case("websocket"));
    let html_nav = req.method() == axum::http::Method::GET && wants_html(req.headers());

    let path_and_query = req.uri().path_and_query().map(|p| p.as_str().to_string()).unwrap_or_else(|| "/".into());
    let Ok(uri) = format!("http://127.0.0.1:{}{}", site.dev_port, path_and_query).parse::<Uri>() else {
        return StatusCode::BAD_REQUEST.into_response();
    };
    let headers = upstream_headers(req.headers(), site.dev_port, &caller, upgrade);
    let client_upgrade = upgrade.then(|| hyper::upgrade::on(&mut req));

    let (parts, body) = req.into_parts();
    let mut upstream = axum::http::Request::from_parts(parts, body);
    *upstream.uri_mut() = uri;
    *upstream.headers_mut() = headers;

    let mut response = match state.app.client.request(upstream).await {
        Ok(r) => r,
        Err(_) => {
            return if html_nav || dev_status != DevStatus::Ready {
                boot_page(&site.id, if dev_status == DevStatus::Ready { "starting" } else { dev_status.as_str() })
            } else {
                (StatusCode::BAD_GATEWAY, "dev server unavailable").into_response()
            };
        }
    };

    if html_nav && response.status() == StatusCode::SERVICE_UNAVAILABLE && dev_status != DevStatus::Ready {
        return boot_page(&site.id, dev_status.as_str());
    }

    let frame_ancestors = state.app.frame_ancestors();
    if upgrade && response.status() == StatusCode::SWITCHING_PROTOCOLS {
        let upstream_upgrade = hyper::upgrade::on(&mut response);
        let (mut parts, _body) = response.into_parts();
        adjust_response_headers(&mut parts.headers, &frame_ancestors, true);
        if let Some(client_upgrade) = client_upgrade {
            tokio::spawn(async move {
                let (Ok(client), Ok(server)) = (client_upgrade.await, upstream_upgrade.await) else { return };
                let mut client = TokioIo::new(client);
                let mut server = TokioIo::new(server);
                let _ = tokio::time::timeout(
                    std::time::Duration::from_secs(24 * 3600),
                    tokio::io::copy_bidirectional(&mut client, &mut server),
                )
                .await;
            });
        }
        return Response::from_parts(parts, Body::empty());
    }

    let (mut parts, body) = response.into_parts();
    adjust_response_headers(&mut parts.headers, &frame_ancestors, false);
    Response::from_parts(parts, Body::new(body))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn caller() -> Caller {
        Caller { tailnet: true, host: "hetzner1.tailb7471b.ts.net".into(), authority: "hetzner1.tailb7471b.ts.net:9181".into(), port: 9181, login: Some("me".into()) }
    }

    #[test]
    fn strips_session_cookie_only() {
        assert_eq!(strip_cookie("a=1; cf_cp_s=secret; AuthSession=x", "cf_cp_s").unwrap(), "a=1; AuthSession=x");
        assert_eq!(strip_cookie("cf_cp_s=secret", "cf_cp_s"), None);
    }

    #[test]
    fn rewrites_request_headers() {
        let mut h = HeaderMap::new();
        h.insert("host", "localhost".parse().unwrap());
        h.insert("tailscale-user-login", "me@x".parse().unwrap());
        h.insert("x-forwarded-host", "evil".parse().unwrap());
        h.insert("cookie", "cf_cp_s=s; AuthSession=a".parse().unwrap());
        h.insert("connection", "Upgrade".parse().unwrap());
        h.insert("upgrade", "websocket".parse().unwrap());
        h.insert("origin", "https://hetzner1.tailb7471b.ts.net:9181".parse().unwrap());
        let out = upstream_headers(&h, 4401, &caller(), false);
        assert_eq!(out["host"], "127.0.0.1:4401");
        assert!(out.get("tailscale-user-login").is_none());
        assert_eq!(out["x-forwarded-host"], "hetzner1.tailb7471b.ts.net:9181");
        assert_eq!(out["x-forwarded-proto"], "https");
        assert_eq!(out["cookie"], "AuthSession=a");
        assert!(out.get("upgrade").is_none());
        assert_eq!(out["origin"], "https://hetzner1.tailb7471b.ts.net:9181");
        let ws = upstream_headers(&h, 4401, &caller(), true);
        assert_eq!(ws["upgrade"], "websocket");
        assert_eq!(ws["connection"], "Upgrade");
    }

    #[test]
    fn frame_ancestors_replace_and_append() {
        let mut h = HeaderMap::new();
        h.insert("x-frame-options", "DENY".parse().unwrap());
        h.insert("content-security-policy", "default-src 'self'; frame-ancestors 'none'".parse().unwrap());
        adjust_response_headers(&mut h, "https://a:9180 http://localhost:9180", false);
        assert!(h.get("x-frame-options").is_none());
        assert_eq!(h["content-security-policy"], "default-src 'self'; frame-ancestors https://a:9180 http://localhost:9180");
        let mut h = HeaderMap::new();
        adjust_response_headers(&mut h, "https://a:9180", false);
        assert_eq!(h["content-security-policy"], "frame-ancestors https://a:9180");
    }
}
