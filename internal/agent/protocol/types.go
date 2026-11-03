package protocol

import "encoding/json"

type RequestEnvelope struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Notification struct {
	Method string
	Params json.RawMessage
}

type ServerRequest struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ClientInfo struct {
	Name    string  `json:"name"`
	Title   *string `json:"title,omitempty"`
	Version string  `json:"version"`
}

type InitializeCapabilities struct {
	ExperimentalAPI              bool     `json:"experimentalApi,omitempty"`
	MCPServerOpenAIFormEliciting bool     `json:"mcpServerOpenaiFormElicitation,omitempty"`
	OptOutNotificationMethods    []string `json:"optOutNotificationMethods,omitempty"`
	RequestAttestation           bool     `json:"requestAttestation,omitempty"`
}

type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities,omitempty"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type AccountReadParams struct {
	RefreshToken bool `json:"refreshToken,omitempty"`
}

type Account struct {
	Type     string `json:"type"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

type AccountReadResponse struct {
	Account            *Account `json:"account"`
	RequiresOpenAIAuth bool     `json:"requiresOpenaiAuth"`
}

type SkillsExtraRootsSetParams struct {
	ExtraRoots []string `json:"extraRoots"`
}

type SkillsListParams struct {
	CWDs        []string `json:"cwds,omitempty"`
	ForceReload bool     `json:"forceReload,omitempty"`
}

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type SkillsByCWD struct {
	CWD    string  `json:"cwd"`
	Skills []Skill `json:"skills"`
	Errors []any   `json:"errors"`
}

type SkillsListResponse struct {
	Data []SkillsByCWD `json:"data"`
}

type SandboxPolicy struct {
	Type                string   `json:"type"`
	WritableRoots       []string `json:"writableRoots,omitempty"`
	NetworkAccess       bool     `json:"networkAccess"`
	ExcludeSlashTmp     bool     `json:"excludeSlashTmp,omitempty"`
	ExcludeTmpDirEnvVar bool     `json:"excludeTmpdirEnvVar,omitempty"`
}

type ThreadStartParams struct {
	CWD                   string   `json:"cwd"`
	ApprovalPolicy        string   `json:"approvalPolicy"`
	ApprovalsReviewer     string   `json:"approvalsReviewer"`
	DeveloperInstructions string   `json:"developerInstructions"`
	Sandbox               string   `json:"sandbox"`
	RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots,omitempty"`
	Environments          []any    `json:"environments,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID              string   `json:"threadId"`
	CWD                   string   `json:"cwd,omitempty"`
	ApprovalPolicy        string   `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     string   `json:"approvalsReviewer,omitempty"`
	Sandbox               string   `json:"sandbox,omitempty"`
	RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots,omitempty"`
}

type ThreadReadParams struct {
	ThreadID     string `json:"threadId"`
	IncludeTurns bool   `json:"includeTurns"`
}

type Thread struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status,omitempty"`
	Turns  []Turn          `json:"turns,omitempty"`
}

type ThreadResponse struct {
	Thread Thread `json:"thread"`
}

type UserInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type TurnInput []UserInput

type TurnStartParams struct {
	ThreadID              string         `json:"threadId"`
	Input                 TurnInput      `json:"input"`
	CWD                   string         `json:"cwd,omitempty"`
	ApprovalPolicy        string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     string         `json:"approvalsReviewer,omitempty"`
	SandboxPolicy         *SandboxPolicy `json:"sandboxPolicy,omitempty"`
	RuntimeWorkspaceRoots []string       `json:"runtimeWorkspaceRoots,omitempty"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type Turn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items,omitempty"`
	Error  json.RawMessage   `json:"error,omitempty"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type ThreadTurnParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type ItemNotification struct {
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
	Item     json.RawMessage `json:"item"`
}

type TurnNotification struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}

type ServerRequestResolvedNotification struct {
	ThreadID  string          `json:"threadId"`
	RequestID json.RawMessage `json:"requestId"`
}

type CommandApprovalParams struct {
	ThreadID              string            `json:"threadId"`
	TurnID                string            `json:"turnId"`
	ItemID                string            `json:"itemId"`
	ApprovalID            string            `json:"approvalId,omitempty"`
	Command               string            `json:"command,omitempty"`
	CWD                   string            `json:"cwd,omitempty"`
	Reason                string            `json:"reason,omitempty"`
	AvailableDecisions    []json.RawMessage `json:"availableDecisions,omitempty"`
	AdditionalPermissions json.RawMessage   `json:"additionalPermissions,omitempty"`
	NetworkContext        json.RawMessage   `json:"networkApprovalContext,omitempty"`
}

type FileChangeApprovalParams struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	ItemID    string `json:"itemId"`
	Reason    string `json:"reason,omitempty"`
	GrantRoot string `json:"grantRoot,omitempty"`
}

type PermissionsApprovalParams struct {
	ThreadID    string          `json:"threadId"`
	TurnID      string          `json:"turnId"`
	ItemID      string          `json:"itemId"`
	Reason      string          `json:"reason,omitempty"`
	Permissions json.RawMessage `json:"permissions"`
}

type ApprovalResponse struct {
	Decision any `json:"decision"`
}

type UserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type UserInputQuestion struct {
	ID       string            `json:"id"`
	Header   string            `json:"header"`
	Question string            `json:"question"`
	Options  []UserInputOption `json:"options,omitempty"`
	IsOther  bool              `json:"isOther,omitempty"`
	IsSecret bool              `json:"isSecret,omitempty"`
}

type UserInputRequestParams struct {
	ThreadID  string              `json:"threadId"`
	TurnID    string              `json:"turnId"`
	ItemID    string              `json:"itemId"`
	Questions []UserInputQuestion `json:"questions"`
}

type UserInputResponse struct {
	Answers map[string]string `json:"answers"`
}
