package model

import "time"

type AgentReadinessStatus string

const (
	AgentReady           AgentReadinessStatus = "ready"
	AgentMissing         AgentReadinessStatus = "missing"
	AgentLaunchFailed    AgentReadinessStatus = "launch_failed"
	AgentUnsupported     AgentReadinessStatus = "unsupported"
	AgentUnauthenticated AgentReadinessStatus = "unauthenticated"
	AgentRestarting      AgentReadinessStatus = "restarting"
)

type AgentReadiness struct {
	Status      AgentReadinessStatus `json:"status"`
	Available   bool                 `json:"available"`
	Version     string               `json:"version,omitempty"`
	AccountType string               `json:"accountType,omitempty"`
	Problem     string               `json:"problem,omitempty"`
}

type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

type ChangedFile struct {
	Path          string     `json:"path"`
	Kind          ChangeKind `json:"kind"`
	BaselineDirty bool       `json:"baselineDirty,omitempty"`
}

type ConversationStatus string

const (
	ConversationNew                ConversationStatus = "new"
	ConversationActive             ConversationStatus = "active"
	ConversationWaiting            ConversationStatus = "waiting"
	ConversationInterrupting       ConversationStatus = "interrupting"
	ConversationInterrupted        ConversationStatus = "interrupted"
	ConversationCompleted          ConversationStatus = "completed"
	ConversationFailed             ConversationStatus = "failed"
	ConversationRebuildingRegistry ConversationStatus = "rebuilding_registry"
	ConversationReady              ConversationStatus = "ready"
)

type TurnStatus string

const (
	TurnActive      TurnStatus = "active"
	TurnCompleted   TurnStatus = "completed"
	TurnInterrupted TurnStatus = "interrupted"
	TurnFailed      TurnStatus = "failed"
)

type ConversationTurn struct {
	ID        string     `json:"id"`
	Status    TurnStatus `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
}

type ConversationItem struct {
	ID     string `json:"id"`
	TurnID string `json:"turnId,omitempty"`
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
	Text   string `json:"text,omitempty"`
}

type InteractionKind string

const (
	InteractionApproval InteractionKind = "approval"
	InteractionQuestion InteractionKind = "question"
	InteractionConsent  InteractionKind = "consent"
)

type ApprovalAction string

const (
	ApprovalCommand     ApprovalAction = "command"
	ApprovalFileChange  ApprovalAction = "file_change"
	ApprovalPermissions ApprovalAction = "permissions"
)

type ConsentKind string

const (
	ConsentDependencyChange ConsentKind = "dependency_change"
	ConsentNetworkAccess    ConsentKind = "network_access"
)

type InteractionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID       string              `json:"id"`
	Header   string              `json:"header"`
	Question string              `json:"question"`
	Options  []InteractionOption `json:"options,omitempty"`
	IsOther  bool                `json:"isOther,omitempty"`
	IsSecret bool                `json:"isSecret,omitempty"`
}

type Interaction struct {
	RequestID     string          `json:"requestId"`
	Kind          InteractionKind `json:"kind"`
	Action        ApprovalAction  `json:"action,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	AffectedPaths []string        `json:"affectedPaths,omitempty"`
	ConsentKind   ConsentKind     `json:"consentKind,omitempty"`
	Questions     []Question      `json:"questions,omitempty"`
}

type LastTurn struct {
	Status  TurnStatus `json:"status"`
	Summary string     `json:"summary,omitempty"`
}

type Conversation struct {
	ID                   string             `json:"id"`
	WorkspaceID          string             `json:"workspaceId"`
	SiteID               string             `json:"siteId"`
	Title                string             `json:"title"`
	Status               ConversationStatus `json:"status"`
	ActiveTurn           *ConversationTurn  `json:"activeTurn,omitempty"`
	LastTurn             *LastTurn          `json:"lastTurn,omitempty"`
	Items                []ConversationItem `json:"items,omitempty"`
	PendingInteraction   *Interaction       `json:"pendingInteraction,omitempty"`
	ChangedFiles         []ChangedFile      `json:"changedFiles,omitempty"`
	GeneratedComponentID string             `json:"generatedComponentId,omitempty"`
	Sequence             uint64             `json:"sequence"`
	BindingValid         bool               `json:"bindingValid"`
	ReadOnly             bool               `json:"readOnly,omitempty"`
	CreatedAt            time.Time          `json:"createdAt"`
	UpdatedAt            time.Time          `json:"updatedAt"`
}

type EventType string

const (
	EventConversationSnapshot    EventType = "conversation.snapshot"
	EventTurnStarted             EventType = "turn.started"
	EventItemStarted             EventType = "item.started"
	EventItemDelta               EventType = "item.delta"
	EventItemCompleted           EventType = "item.completed"
	EventInteractionRequested    EventType = "interaction.requested"
	EventInteractionResolved     EventType = "interaction.resolved"
	EventTurnCompleted           EventType = "turn.completed"
	EventRegistryStarted         EventType = "registry.started"
	EventRegistryCompleted       EventType = "registry.completed"
	EventRegistryFailed          EventType = "registry.failed"
	EventConversationInterrupted EventType = "conversation.interrupted"
	EventConversationFailed      EventType = "conversation.failed"
)

type Event struct {
	Sequence       uint64             `json:"sequence"`
	Type           EventType          `json:"type"`
	ConversationID string             `json:"conversationId"`
	TurnID         string             `json:"turnId,omitempty"`
	Status         ConversationStatus `json:"status,omitempty"`
	Item           *ConversationItem  `json:"item,omitempty"`
	Snapshot       *Conversation      `json:"snapshot,omitempty"`
	CreatedAt      time.Time          `json:"createdAt"`
}
