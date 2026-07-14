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
