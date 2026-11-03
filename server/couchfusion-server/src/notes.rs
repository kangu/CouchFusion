//! Workbench notes for the commit message: finished workbench jobs (LLM request, new / deleted /
//! restored component, registry rebuild) whose files are still uncommitted. The dev page turns
//! them into the default commit message (one task: its summary; several: timestamped notes).

use std::collections::{HashMap, HashSet};
use std::path::Path;

use serde_json::Value;
use time::OffsetDateTime;
use time::format_description::well_known::Rfc3339;

use crate::dto::{JobSnapshot, JobStatus, RepoFile, WorkbenchNote};
use crate::http::App;
use crate::request::JOB_REQUEST;
use crate::workspace::MANIFEST_REL;

/// Jobs that change app files, newest `PER_KIND` of each are considered.
const KINDS: [&str; 5] = [JOB_REQUEST, "component.generate", "component.delete", "component.restore", "component-registry.rebuild"];
const PER_KIND: usize = 50;
/// Commits read to find when each file was last committed.
const LOG_DEPTH: usize = 500;
const SUBJECT_MAX: usize = 72;

fn text<'a>(job: &'a JobSnapshot, key: &str) -> Option<&'a str> {
    job.result.as_ref()?.get(key)?.as_str().map(str::trim).filter(|s| !s.is_empty())
}

/// First line, then first sentence, cut at a word boundary to fit a commit subject.
pub fn subject(source: &str) -> String {
    let line = source.trim().lines().next().unwrap_or("").trim();
    let sentence = match line.find(". ") {
        Some(i) => &line[..i],
        None => line.strip_suffix('.').unwrap_or(line),
    };
    if sentence.chars().count() <= SUBJECT_MAX {
        return sentence.to_string();
    }
    let cut: String = sentence.chars().take(SUBJECT_MAX - 1).collect();
    let cut = match cut.rfind(' ') {
        Some(i) if i > SUBJECT_MAX / 2 => &cut[..i],
        _ => cut.as_str(),
    };
    format!("{}…", cut.trim_end_matches([' ', ',', ';', ':']))
}

/// Files a job changed, relative to the app dir. `trash_source` resolves a trash id to its path.
fn job_files(job: &JobSnapshot, trash_source: &dyn Fn(&str) -> Option<String>) -> Vec<String> {
    let listed = || {
        job.result.as_ref().and_then(|r| r.get("changedFiles")).and_then(Value::as_array).map(|a| a.iter().filter_map(Value::as_str).map(str::to_string).collect()).unwrap_or_default()
    };
    match job.kind.as_str() {
        "component.delete" => trash_source(&job.id).into_iter().collect(),
        "component.restore" => text(job, "trashId").and_then(trash_source).into_iter().collect(),
        "component-registry.rebuild" => vec![MANIFEST_REL.to_string(), MANIFEST_REL.replace(".json", ".ts")],
        _ => listed(),
    }
}

/// Title (commit subject) and optional longer text for one job.
fn describe(job: &JobSnapshot) -> (String, Option<String>) {
    let component = text(job, "componentId").unwrap_or("a component");
    let (title, body) = match job.kind.as_str() {
        "component.generate" => (format!("Add component {component}"), text(job, "prompt").map(str::to_string)),
        "component.delete" => (format!("Delete component {component}"), None),
        "component.restore" => (format!("Restore component {component}"), None),
        "component-registry.rebuild" => ("Rebuild the component registry".to_string(), None),
        _ => {
            let summary = text(job, "summary").or_else(|| text(job, "prompt"));
            (summary.map(subject).unwrap_or_else(|| "LLM request".to_string()), summary.map(str::to_string))
        }
    };
    let body = body.map(|b| b.split_whitespace().collect::<Vec<_>>().join(" ")).filter(|b| *b != title && b.trim_end_matches('.') != title);
    (title, body)
}

fn unix(rfc3339: &str) -> Option<i64> {
    OffsetDateTime::parse(rfc3339, &Rfc3339).ok().map(|t| t.unix_timestamp())
}

/// Keeps jobs with at least one file that is still dirty and was not committed after the job
/// finished; oldest first.
pub fn select(jobs: Vec<JobSnapshot>, dirty: &HashSet<&str>, committed: &HashMap<String, i64>, trash_source: &dyn Fn(&str) -> Option<String>) -> Vec<WorkbenchNote> {
    let mut notes: Vec<(i64, WorkbenchNote)> = jobs
        .into_iter()
        .filter(|job| job.status == JobStatus::Succeeded)
        .filter_map(|job| {
            let done_at = job.completed_at.clone()?;
            let done = unix(&done_at)?;
            let files: Vec<String> = job_files(&job, trash_source)
                .into_iter()
                .filter(|f| dirty.contains(f.as_str()) && committed.get(f).is_none_or(|&t| done > t))
                .collect();
            if files.is_empty() {
                return None;
            }
            let (title, text) = describe(&job);
            Some((done, WorkbenchNote { job_id: job.id, kind: job.kind, completed_at: done_at, title, text, files }))
        })
        .collect();
    notes.sort_by_key(|(t, _)| *t);
    notes.into_iter().map(|(_, n)| n).collect()
}

/// Notes for the repo of a site's app (`apps/<site>`); none for other repos.
pub async fn for_repo(app: &App, repo_id: &str, repo: &Path, files: &[RepoFile]) -> Vec<WorkbenchNote> {
    let Some(site) = repo_id.strip_prefix("apps/").filter(|s| app.config.site(s).is_some()) else { return vec![] };
    let dirty: HashSet<&str> = files.iter().map(|f| f.path.as_str()).collect();
    if dirty.is_empty() {
        return vec![];
    }
    let jobs: Vec<JobSnapshot> = KINDS.iter().flat_map(|kind| app.store.recent_jobs(site, kind, PER_KIND).unwrap_or_default()).collect();
    if jobs.is_empty() {
        return vec![];
    }
    let committed = app.git.last_commit_times(repo, LOG_DEPTH).await;
    let trash = |id: &str| app.store.trash_entry(id).ok().flatten().map(|row| row.source);
    select(jobs, &dirty, &committed, &trash)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Map, json};

    fn job(id: &str, kind: &str, done: &str, result: Value) -> JobSnapshot {
        JobSnapshot {
            id: id.into(), kind: kind.into(), workspace_id: None, site_id: Some("s".into()), status: JobStatus::Succeeded,
            created_at: done.into(), started_at: None, completed_at: Some(done.into()), events: vec![], error: None,
            result: result.as_object().cloned().or(Some(Map::new())),
        }
    }

    #[test]
    fn subjects_fit_a_commit_line() {
        assert_eq!(subject("Shortened the hero. Added a CTA."), "Shortened the hero");
        assert_eq!(subject("Swapped the services page.\nMore."), "Swapped the services page");
        let long = subject(&"word ".repeat(30));
        assert!(long.chars().count() <= SUBJECT_MAX && long.ends_with('…'), "{long}");
    }

    #[test]
    fn keeps_jobs_with_uncommitted_files_oldest_first() {
        let t = |s: &str| unix(s).unwrap();
        let jobs = vec![
            job("r2", JOB_REQUEST, "2026-11-03T11:00:00Z", json!({"summary": "Added a pricing block. It has three tiers.", "changedFiles": ["app/b.vue"]})),
            job("r1", JOB_REQUEST, "2026-11-03T10:00:00Z", json!({"prompt": "Shorten the hero", "changedFiles": ["app/a.vue", "app/x.vue"]})),
            job("g1", "component.generate", "2026-11-03T09:00:00Z", json!({"componentId": "network-pricing", "prompt": "A pricing\n table", "changedFiles": ["app/c.vue"]})),
            job("d1", "component.delete", "2026-11-03T12:00:00Z", json!({"componentId": "old-hero"})),
            // Committed after it finished: left out.
            job("r0", JOB_REQUEST, "2026-11-03T08:00:00Z", json!({"summary": "Old", "changedFiles": ["app/a.vue"]})),
            // Nothing of it is dirty: left out.
            job("r3", JOB_REQUEST, "2026-11-03T12:30:00Z", json!({"summary": "Clean", "changedFiles": ["app/clean.vue"]})),
        ];
        let dirty: HashSet<&str> = ["app/a.vue", "app/b.vue", "app/c.vue", "app/components/OldHero.vue"].into();
        let committed = HashMap::from([("app/a.vue".to_string(), t("2026-11-03T09:30:00Z")), ("app/c.vue".to_string(), t("2026-11-03T08:30:00Z"))]);
        let trash = |id: &str| (id == "d1").then(|| "app/components/OldHero.vue".to_string());
        let notes = select(jobs, &dirty, &committed, &trash);
        let ids: Vec<&str> = notes.iter().map(|n| n.job_id.as_str()).collect();
        assert_eq!(ids, vec!["g1", "r1", "r2", "d1"]);
        assert_eq!(notes[0].title, "Add component network-pricing");
        assert_eq!(notes[0].text.as_deref(), Some("A pricing table"));
        assert_eq!(notes[1].title, "Shorten the hero");
        assert_eq!(notes[1].text, None);
        assert_eq!(notes[1].files, vec!["app/a.vue"]);
        assert_eq!(notes[2].title, "Added a pricing block");
        assert_eq!(notes[2].text.as_deref(), Some("Added a pricing block. It has three tiers."));
        assert_eq!(notes[3].title, "Delete component old-hero");
    }
}
