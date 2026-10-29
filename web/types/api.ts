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

export type BootstrapCapability = 'dev-sessions' | 'component-delete' | 'component-generate' | 'git'

export interface Bootstrap {
  version: string
  backend?: 'rust' | 'go'
  capabilities?: BootstrapCapability[]
  devOrigins?: string[]
  workspaceId?: string
  siteId?: string
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
  files: RepoStatusFile[]
}

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

export interface ComponentGenerateResult {
  componentId?: string
  changedFiles: string[]
  threadId?: string
  lint: string[]
  needsRestart?: { reason: string }
}

export interface ComponentDeleteResult {
  componentId: string
  trashId: string
}
