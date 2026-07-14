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
