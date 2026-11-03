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

pub struct SiteHub {
    sites: Mutex<HashMap<String, SiteChannel>>,
    /// First sequence number of this process: boot time in ms × 1000. Sequences therefore keep
    /// growing across control-plane restarts, so a browser resuming with an old cursor never
    /// skips the new process's events (they used to restart at 1 and were all filtered out).
    /// Stays below 2^53 (JS safe integers) until the year 2255.
    base: u64,
}

impl Default for SiteHub {
    fn default() -> Self {
        let ms = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(0);
        Self::with_base(ms.saturating_mul(1000))
    }
}

/// What a new subscriber receives: buffered events after its cursor, then live ones.
pub struct Subscription {
    pub replay: Vec<SiteEvent>,
    pub rx: broadcast::Receiver<SiteEvent>,
    pub cursor: u64,
}

impl SiteHub {
    pub fn with_base(base: u64) -> Self {
        Self { sites: Mutex::new(HashMap::new()), base }
    }

    fn with_channel<T>(&self, site: &str, f: impl FnOnce(&mut SiteChannel) -> T) -> T {
        let mut sites = self.sites.lock().expect("site hub lock");
        let base = self.base;
        let channel = sites.entry(site.to_string()).or_insert_with(|| SiteChannel {
            seq: base,
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

    /// Events after `after`, then live ones. A cursor this process never issued (from before a
    /// restart, or ahead of it) gets the whole buffered history; `cursor` is what to filter
    /// live events against.
    pub fn subscribe(&self, site: &str, after: u64) -> Subscription {
        self.with_channel(site, |ch| {
            let cursor = if after > ch.seq || after < self.base { 0 } else { after };
            Subscription { replay: ch.history.iter().filter(|e| e.seq > cursor).cloned().collect(), rx: ch.tx.subscribe(), cursor }
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
        let hub = SiteHub::with_base(1000);
        for i in 0..3 {
            hub.registry("s", RegistryChange { added: vec![format!("c{i}")], removed: vec![] });
        }
        let sub = hub.subscribe("s", 1001);
        assert_eq!(sub.replay.iter().map(|e| e.seq).collect::<Vec<_>>(), vec![1002, 1003]);
        assert_eq!(sub.cursor, 1001);
    }

    #[tokio::test]
    async fn cursor_from_another_process_gets_everything() {
        // Before a restart the browser saw seq 57 of the old process; the new one starts at its base.
        let hub = SiteHub::with_base(5000);
        hub.registry("s", RegistryChange { added: vec!["a".into()], removed: vec![] });
        let old = hub.subscribe("s", 57);
        assert_eq!(old.cursor, 0);
        assert_eq!(old.replay.len(), 1);
        let ahead = hub.subscribe("s", 9_999_999);
        assert_eq!(ahead.cursor, 0);
        assert_eq!(ahead.replay.len(), 1);
        // A newer process always numbers above an older one.
        assert!(SiteHub::default().base > 5000);
    }
}
