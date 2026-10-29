//! Per-site event hub (contract §2.2 `SiteEvent`) and SSE framing.

use std::collections::{HashMap, VecDeque};
use std::sync::Mutex;

use tokio::sync::broadcast;

use crate::dto::{ActivityEvent, DevSessionView, JobSnapshot, RegistryChange, SiteEvent};
use crate::util::now_rfc3339;

const HISTORY: usize = 300;

struct SiteChannel {
    seq: u64,
    history: VecDeque<SiteEvent>,
    tx: broadcast::Sender<SiteEvent>,
}

#[derive(Default)]
pub struct SiteHub {
    sites: Mutex<HashMap<String, SiteChannel>>,
}

/// What a new subscriber receives: buffered events after its cursor, then live ones.
pub struct Subscription {
    pub replay: Vec<SiteEvent>,
    pub rx: broadcast::Receiver<SiteEvent>,
}

impl SiteHub {
    fn with_channel<T>(&self, site: &str, f: impl FnOnce(&mut SiteChannel) -> T) -> T {
        let mut sites = self.sites.lock().expect("site hub lock");
        let channel = sites.entry(site.to_string()).or_insert_with(|| SiteChannel {
            seq: 0,
            history: VecDeque::with_capacity(HISTORY),
            tx: broadcast::channel(256).0,
        });
        f(channel)
    }

    fn publish(&self, site: &str, kind: &'static str, fill: impl FnOnce(&mut SiteEvent)) {
        self.with_channel(site, |ch| {
            ch.seq += 1;
            let mut event = SiteEvent { seq: ch.seq, kind, time: now_rfc3339(), dev: None, log: None, registry: None, job: None };
            fill(&mut event);
            if ch.history.len() == HISTORY {
                ch.history.pop_front();
            }
            ch.history.push_back(event.clone());
            let _ = ch.tx.send(event);
        });
    }

    pub fn dev_status(&self, site: &str, view: DevSessionView) {
        self.publish(site, "dev.status", |e| e.dev = Some(view));
    }
    pub fn log(&self, site: &str, log: ActivityEvent) {
        self.publish(site, "log", |e| e.log = Some(log));
    }
    pub fn registry(&self, site: &str, change: RegistryChange) {
        self.publish(site, "registry.changed", |e| e.registry = Some(change));
    }
    pub fn job(&self, site: &str, job: JobSnapshot) {
        self.publish(site, "job", |e| e.job = Some(job));
    }

    pub fn subscribe(&self, site: &str, after: u64) -> Subscription {
        self.with_channel(site, |ch| Subscription {
            replay: ch.history.iter().filter(|e| e.seq > after).cloned().collect(),
            rx: ch.tx.subscribe(),
        })
    }
}

/// One SSE frame: `id: <id>\nevent: <name>\ndata: <json>\n\n`. JSON never contains raw
/// newlines (serde escapes them), so a single `data:` line is always valid.
pub fn sse_frame(id: u64, event: &str, data: &str) -> String {
    debug_assert!(!data.contains('\n'));
    format!("id: {id}\nevent: {event}\ndata: {data}\n\n")
}

pub const SSE_KEEPALIVE: &str = ":keepalive\n\n";

/// Parses the resume cursor from `Last-Event-ID` or `?after=`.
pub fn resume_cursor(last_event_id: Option<&str>, after: Option<&str>) -> u64 {
    last_event_id.or(after).and_then(|v| v.trim().parse().ok()).unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_format() {
        assert_eq!(sse_frame(7, "site", r#"{"a":"x\ny"}"#), "id: 7\nevent: site\ndata: {\"a\":\"x\\ny\"}\n\n");
        let json = serde_json::to_string(&serde_json::json!({"t": "line1\nline2"})).unwrap();
        assert!(!json.contains('\n'));
    }

    #[test]
    fn cursor_prefers_header() {
        assert_eq!(resume_cursor(Some("5"), Some("9")), 5);
        assert_eq!(resume_cursor(None, Some("9")), 9);
        assert_eq!(resume_cursor(None, Some("x")), 0);
    }

    #[tokio::test]
    async fn replay_after_cursor() {
        let hub = SiteHub::default();
        for i in 0..3 {
            hub.registry("s", RegistryChange { added: vec![format!("c{i}")], removed: vec![] });
        }
        let sub = hub.subscribe("s", 1);
        assert_eq!(sub.replay.iter().map(|e| e.seq).collect::<Vec<_>>(), vec![2, 3]);
    }
}
