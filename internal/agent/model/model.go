package model

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
