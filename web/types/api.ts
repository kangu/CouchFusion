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
