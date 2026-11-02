//! Wire types. Mirrors `cli-init/web/types/api.ts` and the Phase 1 contract §2.
//! camelCase, optional fields omitted (never `null`).

use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Bootstrap {
    pub version: &'static str,
    pub backend: &'static str,
    pub capabilities: Vec<&'static str>,
    pub dev_origins: Vec<String>,
    pub workspace_id: String,
    pub site_id: String,
    /// Model settings for LLM runs; `null` fields = Codex's account default.
    pub agent: AgentInfo,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentInfo {
    pub model: Option<String>,
    pub reasoning_effort: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct OnboardingState {
    pub version: u32,
    pub status: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub workspace_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub site_id: Option<String>,
    pub updated_at: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceSummary {
    pub id: String,
    pub name: String,
    pub root: String,
    pub available: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub problem: Option<String>,
    pub created_at: String,
    pub last_opened_at: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Warning {
    pub code: String,
    pub message: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Site {
    pub id: String,
    pub name: String,
    pub root: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub package_name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub package_manager: Option<String>,
    pub modules: Vec<String>,
    pub scripts: serde_json::Map<String, Value>,
    pub dependencies: serde_json::Map<String, Value>,
    pub dev_dependencies: serde_json::Map<String, Value>,
    pub dependencies_installed: bool,
    pub lockfiles: Vec<String>,
    pub warnings: Vec<Warning>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Layer {
    pub id: String,
    pub name: String,
    pub root: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceDetail {
    pub id: String,
    pub name: String,
    pub root: String,
    pub available: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub problem: Option<String>,
    pub sites: Vec<Site>,
    pub layers: Vec<Layer>,
    pub warnings: Vec<Warning>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ContentComponent {
    pub id: String,
    pub label: String,
    #[serde(default)]
    pub description: String,
    pub source: String,
    pub prop_count: i64,
    #[serde(default)]
    pub allow_children: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ComponentRegistryResponse {
    pub status: &'static str,
    pub components: Vec<ContentComponent>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub problem: Option<String>,
    pub can_rebuild: bool,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct NeedsRestart {
    pub reason: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DevSessionView {
    pub site_id: String,
    pub unit: String,
    pub status: DevStatus,
    pub url: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub started_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub memory_bytes: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub needs_restart: Option<NeedsRestart>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub problem: Option<String>,
    /// How long a start may take before it is marked failed (`server.dev_ready_timeout_secs`).
    pub ready_timeout_secs: u64,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum DevStatus {
    Stopped,
    Starting,
    Ready,
    Stopping,
    Failed,
}

impl DevStatus {
    pub fn as_str(self) -> &'static str {
        match self {
            DevStatus::Stopped => "stopped",
            DevStatus::Starting => "starting",
            DevStatus::Ready => "ready",
            DevStatus::Stopping => "stopping",
            DevStatus::Failed => "failed",
        }
    }
    pub fn parse(s: &str) -> Self {
        match s {
            "starting" => DevStatus::Starting,
            "ready" => DevStatus::Ready,
            "stopping" => DevStatus::Stopping,
            "failed" => DevStatus::Failed,
            _ => DevStatus::Stopped,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct PageRef {
    pub id: String,
    pub path: String,
    #[serde(default)]
    pub title: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct CodeRef {
    pub path: String,
    pub line: u32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ComponentUsage {
    pub component_id: String,
    pub source: String,
    pub sha256: String,
    pub git: &'static str,
    pub pages: Vec<PageRef>,
    pub code_references: Vec<CodeRef>,
    pub page_scan: &'static str,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HeadInfo {
    pub sha: String,
    pub subject: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RepoSummary {
    pub id: String,
    pub path: String,
    pub branch: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub head: Option<HeadInfo>,
    pub dirty: usize,
    pub has_remote: bool,
    #[serde(flatten)]
    pub remote: RemoteState,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct RepoFile {
    pub path: String,
    pub index: String,
    pub worktree: String,
    pub untracked: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RepoStatus {
    pub id: String,
    pub branch: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub head: Option<HeadInfo>,
    pub has_remote: bool,
    #[serde(flatten)]
    pub remote: RemoteState,
    pub files: Vec<RepoFile>,
}

/// Remote tracking state of a repo (from local refs; refreshed by a fetch job).
#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RemoteState {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub upstream: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ahead: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub behind: Option<u32>,
    /// Branches the control plane may push (empty: no remote configured in server.toml).
    pub push_branches: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RepoDiff {
    pub path: String,
    pub diff: String,
    pub truncated: bool,
    pub binary: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ActivityEvent {
    pub id: u64,
    pub time: String,
    pub code: String,
    pub text: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stream: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ApiErrorPayload {
    pub code: String,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub fields: Option<serde_json::Map<String, Value>>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum JobStatus {
    Queued,
    Running,
    Succeeded,
    Failed,
    Cancelled,
}

impl JobStatus {
    pub fn terminal(self) -> bool {
        matches!(self, JobStatus::Succeeded | JobStatus::Failed | JobStatus::Cancelled)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct JobSnapshot {
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub workspace_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub site_id: Option<String>,
    pub status: JobStatus,
    pub created_at: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub started_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub completed_at: Option<String>,
    pub events: Vec<ActivityEvent>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ApiErrorPayload>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<serde_json::Map<String, Value>>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RegistryChange {
    pub added: Vec<String>,
    pub removed: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SiteEvent {
    pub seq: u64,
    #[serde(rename = "type")]
    pub kind: &'static str,
    pub time: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub dev: Option<DevSessionView>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub log: Option<ActivityEvent>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub registry: Option<RegistryChange>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub job: Option<JobSnapshot>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ComponentSource {
    pub component_id: String,
    pub path: String,
    pub sha256: String,
    pub size: u64,
    pub content: String,
    pub truncated: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TrashEntry {
    pub trash_id: String,
    pub component_id: String,
    pub source: String,
    pub sha256: String,
    pub git: String,
    pub deleted_at: String,
    pub restorable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub problem: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ServiceSnapshot {
    pub unit: String,
    pub description: String,
    /// adopted | dev | prod | system
    pub kind: &'static str,
    pub active_state: String,
    pub sub_state: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub main_pid: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub memory_current_bytes: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub since: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user: Option<String>,
}

// ---- request bodies ----

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Empty {}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct DeleteComponentRequest {
    pub expected_sha256: String,
    #[serde(default)]
    pub acknowledge_usage: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct GenerateRequest {
    pub prompt: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct LlmRequest {
    pub prompt: String,
    #[serde(default)]
    pub current_path: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct CommitRequest {
    pub message: String,
    pub paths: Vec<String>,
    pub expected_head: String,
}

/// One production release (`<www_dir>/releases/<id>/release.json`).
#[derive(Debug, Clone, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ReleaseInfo {
    pub id: String,
    /// UTC time the snapshot was taken (from the id).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub created_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub app_head: Option<String>,
    pub app_dirty: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub layers_head: Option<String>,
    pub layers_dirty: bool,
    pub current: bool,
}

/// Whether a build may start now (spec §7.2 step 1).
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Admission {
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub memory_available_mib: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub disk_free_gib: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub problem: Option<String>,
}

/// `GET …/sites/{site}/prod`.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProdView {
    pub site_id: String,
    pub unit: String,
    pub port: u16,
    pub service: ServiceSnapshot,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub current: Option<String>,
    /// Newest first.
    pub releases: Vec<ReleaseInfo>,
    pub admission: Admission,
    /// Running deploy/rollback (or other site job holding the lease), if any.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub active_job_id: Option<String>,
    /// Tailnet preview of the production server (`[sites.prod] preview_port`).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub preview_url: Option<String>,
    /// Public address (`[sites.prod] public_url`).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub public_url: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct DeployRequest {
    /// The site id, typed by the user (step-up confirmation).
    pub confirm: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct RollbackRequest {
    pub release_id: String,
    pub confirm: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct PushRequest {
    pub branch: String,
    pub confirm: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn optional_fields_are_omitted() {
        let view = DevSessionView {
            site_id: "radustanciu".into(),
            unit: "cf-dev@radustanciu.service".into(),
            status: DevStatus::Stopped,
            url: "https://h:9181".into(),
            started_at: None,
            memory_bytes: None,
            needs_restart: None,
            problem: None,
            ready_timeout_secs: 180,
        };
        let json = serde_json::to_string(&view).unwrap();
        assert_eq!(
            json,
            r#"{"siteId":"radustanciu","unit":"cf-dev@radustanciu.service","status":"stopped","url":"https://h:9181","readyTimeoutSecs":180}"#
        );
    }

    #[test]
    fn job_type_field_name() {
        let job = JobSnapshot {
            id: "j".into(),
            kind: "component.delete".into(),
            workspace_id: None,
            site_id: Some("s".into()),
            status: JobStatus::Queued,
            created_at: "t".into(),
            started_at: None,
            completed_at: None,
            events: vec![],
            error: None,
            result: None,
        };
        let v: Value = serde_json::to_value(&job).unwrap();
        assert_eq!(v["type"], "component.delete");
        assert_eq!(v["status"], "queued");
        assert!(v.get("workspaceId").is_none());
    }
}
