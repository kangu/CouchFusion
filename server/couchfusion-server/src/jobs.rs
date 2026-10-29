//! Job manager: snapshots, bounded events, per-site lease, cancellation, persistence.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use serde_json::{Map, Value};
use tokio::sync::{broadcast, watch};

use crate::dto::{ActivityEvent, ApiErrorPayload, JobSnapshot, JobStatus};
use crate::error::ApiError;
use crate::events::SiteHub;
use crate::store::Store;
use crate::util::{Redactor, now_rfc3339, random_id};

const MAX_EVENTS: usize = 200;

struct JobEntry {
    snapshot: JobSnapshot,
    next_event: u64,
    tx: broadcast::Sender<ActivityEvent>,
    cancel: watch::Sender<bool>,
}

pub struct Jobs {
    store: Arc<Store>,
    hub: Arc<SiteHub>,
    jobs: Mutex<HashMap<String, JobEntry>>,
    leases: Mutex<HashMap<String, String>>,
    redactor: Redactor,
}

/// Handle given to a running job to report progress.
#[derive(Clone)]
pub struct JobCtx {
    pub id: String,
    pub site: String,
    jobs: Arc<Jobs>,
    cancel: watch::Receiver<bool>,
}

impl Jobs {
    pub fn new(store: Arc<Store>, hub: Arc<SiteHub>) -> Arc<Self> {
        // Jobs that were running when the previous process died cannot be resumed.
        if let Ok(stale) = store.non_terminal_jobs() {
            for mut job in stale {
                job.status = JobStatus::Failed;
                job.completed_at = Some(now_rfc3339());
                job.error = Some(ApiErrorPayload {
                    code: "internal".into(),
                    message: "Interrupted: the control plane restarted while this job was running.".into(),
                    fields: None,
                });
                let _ = store.save_job(&job);
            }
        }
        Arc::new(Self {
            store,
            hub,
            jobs: Mutex::new(HashMap::new()),
            leases: Mutex::new(HashMap::new()),
            redactor: Redactor::new(vec![]),
        })
    }

    /// Takes the per-site lease and registers a queued job. Fails with 409 if the site is busy.
    pub fn create(self: &Arc<Self>, kind: &str, workspace: &str, site: &str) -> Result<JobCtx, ApiError> {
        let id = random_id();
        {
            let mut leases = self.leases.lock().expect("lease lock");
            if let Some(active) = leases.get(site) {
                let mut fields = Map::new();
                fields.insert("activeJobId".into(), Value::String(active.clone()));
                return Err(ApiError::conflict("Another job is already running for this site.").with_fields(fields));
            }
            leases.insert(site.to_string(), id.clone());
        }
        let snapshot = JobSnapshot {
            id: id.clone(),
            kind: kind.to_string(),
            workspace_id: Some(workspace.to_string()),
            site_id: Some(site.to_string()),
            status: JobStatus::Queued,
            created_at: now_rfc3339(),
            started_at: None,
            completed_at: None,
            events: vec![],
            error: None,
            result: None,
        };
        let (cancel_tx, cancel_rx) = watch::channel(false);
        self.jobs.lock().expect("jobs lock").insert(
            id.clone(),
            JobEntry { snapshot: snapshot.clone(), next_event: 1, tx: broadcast::channel(256).0, cancel: cancel_tx },
        );
        let _ = self.store.save_job(&snapshot);
        self.hub.job(site, snapshot);
        Ok(JobCtx { id, site: site.to_string(), jobs: self.clone(), cancel: cancel_rx })
    }

    pub fn snapshot(&self, id: &str) -> Option<JobSnapshot> {
        if let Some(entry) = self.jobs.lock().expect("jobs lock").get(id) {
            return Some(entry.snapshot.clone());
        }
        self.store.load_job(id).ok().flatten()
    }

    /// Replay events after `after`, then a live receiver (None if the job is terminal or unknown).
    pub fn subscribe(&self, id: &str, after: u64) -> Option<(Vec<ActivityEvent>, Option<broadcast::Receiver<ActivityEvent>>)> {
        let jobs = self.jobs.lock().expect("jobs lock");
        if let Some(entry) = jobs.get(id) {
            let replay = entry.snapshot.events.iter().filter(|e| e.id > after).cloned().collect();
            let rx = (!entry.snapshot.status.terminal()).then(|| entry.tx.subscribe());
            return Some((replay, rx));
        }
        drop(jobs);
        self.store
            .load_job(id)
            .ok()
            .flatten()
            .map(|job| (job.events.into_iter().filter(|e| e.id > after).collect(), None))
    }

    pub fn cancel(&self, id: &str) -> Option<JobSnapshot> {
        let jobs = self.jobs.lock().expect("jobs lock");
        let entry = jobs.get(id)?;
        if !entry.snapshot.status.terminal() {
            let _ = entry.cancel.send(true);
        }
        Some(entry.snapshot.clone())
    }

    fn update(&self, id: &str, f: impl FnOnce(&mut JobEntry)) {
        let (snapshot, site) = {
            let mut jobs = self.jobs.lock().expect("jobs lock");
            let Some(entry) = jobs.get_mut(id) else { return };
            f(entry);
            (entry.snapshot.clone(), entry.snapshot.site_id.clone())
        };
        let _ = self.store.save_job(&snapshot);
        if let Some(site) = site {
            self.hub.job(&site, snapshot);
        }
    }

    fn release(&self, site: &str, id: &str) {
        let mut leases = self.leases.lock().expect("lease lock");
        if leases.get(site).is_some_and(|j| j == id) {
            leases.remove(site);
        }
    }
}

impl JobCtx {
    pub fn start(&self) {
        self.jobs.update(&self.id, |e| {
            e.snapshot.status = JobStatus::Running;
            e.snapshot.started_at = Some(now_rfc3339());
        });
    }

    pub fn event(&self, level: &str, code: &str, text: impl AsRef<str>) {
        let text = self.jobs.redactor.redact(text.as_ref());
        self.jobs.update(&self.id, |e| {
            let event = ActivityEvent {
                id: e.next_event,
                time: now_rfc3339(),
                code: code.to_string(),
                text,
                level: Some(level.to_string()),
                stream: None,
            };
            e.next_event += 1;
            if e.snapshot.events.len() == MAX_EVENTS {
                e.snapshot.events.remove(0);
            }
            e.snapshot.events.push(event.clone());
            let _ = e.tx.send(event);
        });
    }

    pub fn info(&self, code: &str, text: impl AsRef<str>) {
        self.event("info", code, text);
    }
    pub fn warn(&self, code: &str, text: impl AsRef<str>) {
        self.event("warn", code, text);
    }

    pub fn cancelled(&self) -> bool {
        *self.cancel.borrow()
    }

    pub fn cancel_signal(&self) -> watch::Receiver<bool> {
        self.cancel.clone()
    }

    pub fn succeed(&self, result: Map<String, Value>) {
        self.finish(JobStatus::Succeeded, None, Some(result));
    }

    pub fn fail(&self, err: &ApiError, result: Option<Map<String, Value>>) {
        self.event("error", "job.failed", &err.message);
        let status = if self.cancelled() { JobStatus::Cancelled } else { JobStatus::Failed };
        self.finish(status, Some(err.payload()), result);
    }

    fn finish(&self, status: JobStatus, error: Option<ApiErrorPayload>, result: Option<Map<String, Value>>) {
        self.jobs.update(&self.id, |e| {
            e.snapshot.status = status;
            e.snapshot.completed_at = Some(now_rfc3339());
            e.snapshot.error = error;
            if result.is_some() {
                e.snapshot.result = result;
            }
        });
        self.jobs.release(&self.site, &self.id);
    }

    pub fn snapshot(&self) -> JobSnapshot {
        self.jobs.snapshot(&self.id).expect("job exists")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lease_is_per_site() {
        let jobs = Jobs::new(Arc::new(Store::memory()), Arc::new(SiteHub::default()));
        let a = jobs.create("component.delete", "w", "s1").unwrap();
        let err = jobs.create("component.delete", "w", "s1").err().unwrap();
        assert_eq!(err.code, "conflict");
        assert_eq!(err.fields.unwrap()["activeJobId"], a.id.as_str());
        jobs.create("component.delete", "w", "s2").unwrap();
        a.succeed(Map::new());
        jobs.create("component.delete", "w", "s1").unwrap();
    }

    #[test]
    fn events_are_bounded_and_redacted() {
        let jobs = Jobs::new(Arc::new(Store::memory()), Arc::new(SiteHub::default()));
        let job = jobs.create("x", "w", "s").unwrap();
        for i in 0..250 {
            job.info("n", format!("event {i} token=abc"));
        }
        let snap = job.snapshot();
        assert_eq!(snap.events.len(), MAX_EVENTS);
        assert_eq!(snap.events.last().unwrap().id, 250);
        assert!(snap.events[0].text.contains("token=[redacted]"));
    }
}
