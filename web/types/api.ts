export type ErrorCode =
  | 'validation'
  | 'conflict'
  | 'not_found'
  | 'unavailable'
  | 'filesystem'
  | 'git'
  | 'process'
  | 'config'
  | 'internal'
  | 'unauthorized'
  | 'forbidden'

export interface ApiErrorPayload {
  code: ErrorCode
  message: string
  fields?: Record<string, string>
}

export interface ApiEnvelope<T> {
  data: T
}

export interface ApiErrorEnvelope {
  error: ApiErrorPayload
}

export interface WorkspaceSummary {
  id: string
  name: string
  root: string
  available: boolean
  problem?: string
  createdAt: string
  lastOpenedAt: string
}

export interface Warning {
  code: string
  message: string
}

export interface Site {
  id: string
  name: string
  root: string
  packageName?: string
  packageManager?: string
  modules: string[]
  scripts: Record<string, string>
  dependencies: Record<string, string>
  devDependencies: Record<string, string>
  dependenciesInstalled: boolean
  lockfiles: string[]
  warnings: Warning[]
}

export interface Layer {
  id: string
  name: string
  root: string
}

export interface WorkspaceDetail {
  id: string
  name: string
  root: string
  available: boolean
  problem?: string
  sites: Site[]
  layers: Layer[]
  warnings: Warning[]
}

export interface Task {
  id: string
  name: string
  kind: string
  longRunning: boolean
}

export type ComponentRegistryStatus = 'ready' | 'stale' | 'missing' | 'invalid'

export interface ContentComponent {
  id: string
  label: string
  description: string
  source: string
  propCount: number
  allowChildren: boolean
}

export interface ComponentRegistryResponse {
  status: ComponentRegistryStatus
  components: ContentComponent[]
  problem?: string
  canRebuild: boolean
}

export type ComponentPreviewHostStatus = 'stopped' | 'starting' | 'ready' | 'failed'

export interface ComponentPreviewHost {
  status: ComponentPreviewHostStatus
  processId?: string
  url?: string
  startedAt?: string
  problem?: string
}

export type ConfigSource = 'embedded' | 'global' | 'workspace'

export interface EffectiveConfig {
  config: {
    repos: Record<string, { url: string; branch: string; protocol: string; authPrompt: boolean }>
    modules: Record<string, { description: string; extends: string }>
    workspace: { defaultRoot: string }
    prompts: { defaultLayerSelection: string[] }
  }
  sources: Record<string, ConfigSource>
}

export interface DependencyHealth {
  available: boolean
  version?: string
  path?: string
  url?: string
  problem?: string
}

export interface SystemHealth {
  git: DependencyHealth
  bun: DependencyHealth
  node: DependencyHealth
  couchdb: DependencyHealth
	agent: AgentReadiness
}

export type OnboardingStatus = 'not_started' | 'active' | 'paused' | 'completed'
export type OnboardingActionType =
  | 'select_workspace'
  | 'select_site'
  | 'select_component'
  | 'track_job'
  | 'clear_job'
  | 'confirm_preview'
  | 'confirm_placement'
  | 'pause'
  | 'resume'
  | 'reset'

export interface OnboardingAction {
  action: OnboardingActionType
  resourceId?: string
}

export interface OnboardingState {
  version: number
  status: OnboardingStatus
  workspaceId?: string
  siteId?: string
  componentId?: string
  activeJobId?: string
  previewConfirmedAt?: string
  placementConfirmedAt?: string
  startedAt?: string
  pausedAt?: string
  completedAt?: string
  updatedAt?: string
}

export type AgentReadinessStatus =
	| 'ready'
	| 'missing'
	| 'launch_failed'
	| 'unsupported'
	| 'unauthenticated'
	| 'restarting'

export interface AgentReadiness {
	status: AgentReadinessStatus
	available: boolean
	version?: string
	accountType?: string
	problem?: string
}

export type AgentConversationStatus =
	| 'new'
	| 'active'
	| 'waiting'
	| 'interrupting'
	| 'interrupted'
	| 'completed'
	| 'failed'
	| 'rebuilding_registry'
	| 'ready'

export type AgentTurnStatus = 'active' | 'completed' | 'interrupted' | 'failed'
export type AgentItemType = 'userMessage' | 'agentMessage' | 'reasoning' | 'commandExecution' | 'fileChange' | 'unknown'
export type AgentItemStatus = 'inProgress' | 'completed' | 'failed' | 'declined' | 'interrupted' | 'unknown'
export type AgentInteractionKind = 'approval' | 'question' | 'consent'
export type AgentApprovalAction = 'command' | 'file_change' | 'permissions'
export type AgentApprovalDecision = 'approve_once' | 'approve_generation' | 'decline' | 'cancel'
export type AgentConsentKind = 'dependency_change' | 'network_access'
export type AgentChangedFileKind = 'added' | 'modified' | 'deleted'
export type AgentRegistryState = 'not_needed' | 'running' | 'succeeded' | 'failed'
export type AgentEventType =
	| 'conversation.snapshot'
	| 'turn.started'
	| 'item.started'
	| 'item.delta'
	| 'item.completed'
	| 'interaction.requested'
	| 'interaction.resolved'
	| 'turn.completed'
	| 'registry.started'
	| 'registry.completed'
	| 'registry.failed'
	| 'conversation.interrupted'
	| 'conversation.failed'

export interface AgentConversationTurn {
	id: string
	status: AgentTurnStatus
	startedAt: string
}

export interface AgentConversationItem {
	id: string
	turnId?: string
	type: AgentItemType
	status?: AgentItemStatus
	text?: string
}

export interface AgentInteractionOption {
	label: string
	description?: string
}

export interface AgentQuestion {
	id: string
	header: string
	question: string
	options?: AgentInteractionOption[]
	isOther?: boolean
	isSecret?: boolean
}

export interface AgentInteraction {
	requestId: string
	kind: AgentInteractionKind
	action?: AgentApprovalAction
	summary?: string
	affectedPaths?: string[]
	consentKind?: AgentConsentKind
	questions?: AgentQuestion[]
}

export interface AgentChangedFile {
	path: string
	kind: AgentChangedFileKind
	baselineDirty?: boolean
}

export interface AgentRegistryResult {
	state: AgentRegistryState
	problem?: string
	generatedComponentId?: string
}

export interface AgentConversation {
	id: string
	workspaceId: string
	siteId: string
	title: string
	status: AgentConversationStatus
	activeTurn?: AgentConversationTurn
	lastTurn?: { status: AgentTurnStatus, summary?: string }
	items?: AgentConversationItem[]
	pendingInteraction?: AgentInteraction
	changedFiles?: AgentChangedFile[]
	generatedComponentId?: string
	registry: AgentRegistryResult
	sequence: number
	bindingValid: boolean
	readOnly?: boolean
	createdAt: string
	updatedAt: string
}

export interface AgentConversationCollection {
	readiness: AgentReadiness
	conversations: AgentConversation[]
}

export interface AgentConversationEvent {
	sequence: number
	type: AgentEventType
	conversationId: string
	turnId?: string
	status?: AgentConversationStatus
	item?: AgentConversationItem
	snapshot?: AgentConversation
	createdAt: string
}

export interface TaskRunResult {
  job?: JobSnapshot
  process?: ProcessSnapshot
}

export type ActivityStatus = 'queued' | 'running' | 'stopping' | 'succeeded' | 'failed' | 'cancelled' | 'exited' | 'stopped'

export interface ActivityEvent {
  id: number
  time: string
  code: string
  text: string
  level?: string
  stream?: string
}

export interface JobSnapshot {
  id: string
  type: string
  workspaceId?: string
  siteId?: string
  status: ActivityStatus
  createdAt: string
  startedAt?: string
  completedAt?: string
  events: ActivityEvent[]
  error?: ApiErrorPayload
  /** Job-type specific outcome (Rust backend, Phase 1 contract §2.2). */
  result?: Record<string, unknown>
}

export interface ProcessSnapshot {
  id: string
  workspaceId: string
  siteId: string
  taskId: string
  status: ActivityStatus
  pid?: number
  startedAt: string
  completedAt?: string
  exitCode?: number
  urls: string[]
  events: ActivityEvent[]
  error?: ApiErrorPayload
}

// ---------------------------------------------------------------------------
// Rust control plane, Phase 1 (docs/superpowers/specs/2026-10-29-phase1-mvp-contract.md §2.2)
// ---------------------------------------------------------------------------

export type BootstrapCapability =
  | 'dev-sessions'
  | 'component-delete'
  | 'component-generate'
  | 'git'
  // Phase 2 (docs/superpowers/specs/2026-10-30-phase2-workbench-contract.md)
  | 'component-trash'
  | 'component-source'
  | 'registry-rebuild'
  | 'services'
  | 'git-remote'
  // Phase 3: production releases (deploy, rollback)
  | 'prod-releases'
  // LLM Request (docs/superpowers/specs/2026-11-02-llm-request-contract.md)
  | 'llm-request'

export interface Bootstrap {
  version: string
  backend?: 'rust' | 'go'
  capabilities?: BootstrapCapability[]
  devOrigins?: string[]
  workspaceId?: string
  siteId?: string
  /** Model used for LLM runs (generate and request); null = Codex's account default. */
  agent?: AgentInfo
}

export interface AgentInfo {
  model: string | null
  reasoningEffort: string | null
}

export type DevStatus = 'stopped' | 'starting' | 'ready' | 'stopping' | 'failed'

export interface DevSessionView {
  siteId: string
  unit: string
  status: DevStatus
  url: string
  startedAt?: string
  memoryBytes?: number
  needsRestart?: { reason: string }
  problem?: string
  /** Seconds a start may take before the server marks it failed. */
  readyTimeoutSecs?: number
}

export type ComponentGitState = 'tracked_clean' | 'tracked_modified' | 'untracked' | 'unknown'

export interface ComponentUsagePage {
  id: string
  path: string
  title: string
}

export interface ComponentCodeReference {
  path: string
  line: number
}

export interface ComponentUsage {
  componentId: string
  source: string
  sha256: string
  git: ComponentGitState
  pages: ComponentUsagePage[]
  codeReferences: ComponentCodeReference[]
  pageScan: 'ok' | 'unavailable'
}

export interface RepoHead {
  sha: string
  subject: string
}

export interface RepoSummary {
  id: string
  path: string
  branch: string
  head?: RepoHead
  dirty: number
  hasRemote: boolean
  upstream?: string
  ahead?: number
  behind?: number
  /** Branches the server may push for this repo (empty: no remote configured). */
  pushBranches: string[]
}

export interface RepoStatusFile {
  path: string
  index: string
  worktree: string
  untracked: boolean
}

export interface RepoStatus {
  id: string
  branch: string
  head?: RepoHead
  hasRemote: boolean
  upstream?: string
  ahead?: number
  behind?: number
  pushBranches: string[]
  files: RepoStatusFile[]
}

export type GitRemoteOp = 'fetch' | 'pull' | 'push'

export interface RepoDiff {
  path: string
  diff: string
  truncated: boolean
  binary: boolean
}

export type SiteEventType = 'dev.status' | 'log' | 'registry.changed' | 'job'

export interface SiteEvent {
  seq: number
  type: SiteEventType
  time: string
  dev?: DevSessionView
  log?: ActivityEvent
  registry?: { added: string[]; removed: string[] }
  job?: JobSnapshot
}

export interface CodexUsage {
  inputTokens: number
  cachedInputTokens: number
  outputTokens: number
}

export interface ComponentGenerateResult {
  componentId?: string
  changedFiles: string[]
  threadId?: string
  lint: string[]
  needsRestart?: { reason: string }
  /** Phase 2: Codex `turn.completed.usage`. */
  usage?: CodexUsage
}

/** `GET …/pages`: the site's pages (from the dev server). */
export interface SitePageSummary {
  path: string
  title?: string | null
  publicationState?: string
  locales?: string[]
  redirect?: { to: string; status?: number } | null
  updatedAt?: string | null
}

export interface SitePageList {
  defaultLocale?: string
  locales?: string[]
  pages: SitePageSummary[]
}

export type PageOperationKind = 'page.create' | 'page.update' | 'page.move' | 'page.copyContent' | 'page.publication'

export interface PageOperationSummary {
  op: PageOperationKind
  path: string
  text: string
}

export interface PageCheckProblem {
  index: number | null
  message: string
}

export interface PageChangePreview {
  index: number
  op: PageOperationKind
  path: string
  locales?: string[]
  changes: Array<{ field: string; before?: string | null; after?: string | null }>
}

export interface PageCheck {
  ok: boolean
  problems: PageCheckProblem[]
  preview: PageChangePreview[]
}

/** Result of an `llm.request` job. */
export interface LlmRequestResult {
  summary?: string | null
  changedFiles: string[]
  addedComponents?: string[]
  lint?: string[]
  needsRestart?: { reason: string } | null
  operations: PageOperationSummary[]
  check?: PageCheck
  /** `pending` when checked page changes wait for Apply. */
  applyState?: 'pending'
  currentPath?: string | null
  model?: string | null
  usage?: CodexUsage
  threadId?: string
}

/** Result of an `llm.request.apply` job. */
export interface LlmApplyResult {
  requestJobId: string
  applied: Array<{ index: number; path: string; documents?: Array<{ id: string; rev: string }> }>
  problems?: PageCheckProblem[]
  failed?: { index: number; message: string } | null
}

export interface ComponentDeleteResult {
  componentId: string
  trashId: string
}

// ---------------------------------------------------------------------------
// Rust control plane, Phase 2 (docs/superpowers/specs/2026-10-30-phase2-workbench-contract.md §1)
// ---------------------------------------------------------------------------

export interface ComponentSource {
  componentId: string
  path: string
  sha256: string
  size: number
  content: string
  truncated: boolean
}

export interface TrashEntry {
  /** The delete job id. */
  trashId: string
  componentId: string
  source: string
  sha256: string
  git: ComponentGitState
  deletedAt: string
  /** False when a file now exists at `source`, or the trashed file is missing. */
  restorable: boolean
  problem?: string
}

export interface ComponentRestoreResult {
  componentId: string
  trashId: string
}

export interface RegistryRebuildResult {
  components: number
}

export type ServiceKind = 'adopted' | 'dev' | 'prod' | 'system'

export interface ServiceSnapshot {
  unit: string
  description: string
  kind: ServiceKind
  activeState: string
  subState: string
  mainPid?: number
  memoryCurrentBytes?: number
  since?: string
  user?: string
}

/** One production release (`GET …/sites/{site}/prod`, Phase 3). */
export interface ReleaseInfo {
  id: string
  createdAt?: string
  appHead?: string
  appDirty: boolean
  layersHead?: string
  layersDirty: boolean
  current: boolean
}

/** Whether a build may start now (free memory and disk). */
export interface ReleaseAdmission {
  ok: boolean
  memoryAvailableMib?: number
  diskFreeGib?: number
  problem?: string
}

export interface ProdView {
  siteId: string
  unit: string
  port: number
  service: ServiceSnapshot
  current?: string
  /** Newest first. */
  releases: ReleaseInfo[]
  admission: ReleaseAdmission
  /** Job holding the site lease (a deploy, rollback or any other site job). */
  activeJobId?: string
  /** Tailnet preview of the production server. */
  previewUrl?: string
  /** Public address of the site. */
  publicUrl?: string
}
