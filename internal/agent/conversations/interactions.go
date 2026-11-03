package conversations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/protocol"
)

type ApprovalDecision string

const (
	ApprovalApproveOnce       ApprovalDecision = "approve_once"
	ApprovalApproveGeneration ApprovalDecision = "approve_generation"
	ApprovalDecline           ApprovalDecision = "decline"
	ApprovalCancel            ApprovalDecision = "cancel"
)

var (
	ErrInteractionNotFound = errors.New("agent interaction was not found")
	ErrInteractionResolved = errors.New("agent interaction was already resolved")
	ErrInvalidInteraction  = errors.New("agent interaction response is invalid")
	ErrConsentRequired     = errors.New("agent interaction requires explicit consent")
)

type pendingRequest struct {
	publicID        string
	rpcID           json.RawMessage
	method          string
	threadID        string
	turnID          string
	itemID          string
	action          model.ApprovalAction
	grantKey        string
	permissions     json.RawMessage
	requiredConsent []model.ConsentKind
	questions       map[string]pendingQuestion
}

type pendingQuestion struct {
	rawID   string
	options map[string]struct{}
	isOther bool
}

type actionMetadata struct {
	paths            []string
	dependencyChange bool
	known            bool
}

func (m *Manager) ResolveApproval(ctx context.Context, conversationID, requestID string, decision ApprovalDecision) error {
	if !validApprovalDecision(decision) {
		return ErrInvalidInteraction
	}
	state, ok := m.state(conversationID)
	if !ok {
		return ErrNotFound
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	pending, err := pendingForResolution(state, requestID)
	if err != nil {
		return err
	}
	state.mu.RLock()
	kind := model.InteractionKind("")
	if state.conversation.PendingInteraction != nil {
		kind = state.conversation.PendingInteraction.Kind
	}
	state.mu.RUnlock()
	if kind == model.InteractionConsent {
		return ErrConsentRequired
	}
	if kind != model.InteractionApproval {
		return ErrInvalidInteraction
	}
	client, err := m.client(ctx)
	if err != nil {
		return err
	}
	protocolDecision := string(decision)
	switch decision {
	case ApprovalApproveOnce, ApprovalApproveGeneration:
		protocolDecision = "accept"
	case ApprovalDecline:
		protocolDecision = "decline"
	case ApprovalCancel:
		protocolDecision = "cancel"
	}
	if err := client.Respond(ctx, pending.rpcID, approvalResponse(pending, protocolDecision), nil); err != nil {
		return err
	}
	if decision == ApprovalApproveGeneration {
		state.mu.Lock()
		state.grants[pending.grantKey] = struct{}{}
		state.mu.Unlock()
	}
	m.finishPending(state, pending.publicID)
	if decision == ApprovalCancel {
		if err := m.transition(ctx, state, model.ConversationInterrupting, model.EventInteractionResolved, nil); err != nil {
			return err
		}
		return client.Call(ctx, "turn/interrupt", protocol.TurnInterruptParams{ThreadID: pending.threadID, TurnID: pending.turnID}, nil)
	}
	return m.transition(ctx, state, model.ConversationActive, model.EventInteractionResolved, nil)
}

func (m *Manager) GrantConsent(ctx context.Context, conversationID, requestID string, kind model.ConsentKind, granted bool) error {
	if kind != model.ConsentDependencyChange && kind != model.ConsentNetworkAccess {
		return ErrInvalidInteraction
	}
	state, ok := m.state(conversationID)
	if !ok {
		return ErrNotFound
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	pending, err := pendingForResolution(state, requestID)
	if err != nil {
		return err
	}
	state.mu.RLock()
	interaction := state.conversation.PendingInteraction
	state.mu.RUnlock()
	if interaction == nil || interaction.Kind != model.InteractionConsent || interaction.ConsentKind != kind {
		return ErrInvalidInteraction
	}
	if !granted {
		client, err := m.client(ctx)
		if err != nil {
			return err
		}
		if err := client.Respond(ctx, pending.rpcID, approvalResponse(pending, "decline"), nil); err != nil {
			return err
		}
		m.finishPending(state, pending.publicID)
		return m.transition(ctx, state, model.ConversationActive, model.EventInteractionResolved, nil)
	}

	state.mu.Lock()
	state.consents[kind] = true
	next := missingConsentLocked(state, pending)
	if state.conversation.PendingInteraction != nil {
		if next == "" {
			state.conversation.PendingInteraction.Kind = model.InteractionApproval
			state.conversation.PendingInteraction.ConsentKind = ""
		} else {
			state.conversation.PendingInteraction.ConsentKind = next
		}
	}
	state.mu.Unlock()
	return m.transition(ctx, state, model.ConversationWaiting, model.EventInteractionRequested, nil)
}

func (m *Manager) AnswerQuestion(ctx context.Context, conversationID, requestID string, answers map[string]string) error {
	state, ok := m.state(conversationID)
	if !ok {
		return ErrNotFound
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	pending, err := pendingForResolution(state, requestID)
	if err != nil {
		return err
	}
	state.mu.RLock()
	interaction := state.conversation.PendingInteraction
	state.mu.RUnlock()
	if interaction == nil || interaction.Kind != model.InteractionQuestion || len(answers) == 0 || len(answers) > 3 {
		return ErrInvalidInteraction
	}
	total := 0
	protocolAnswers := map[string]struct {
		Answers []string `json:"answers"`
	}{}
	for publicID, answer := range answers {
		question, exists := pending.questions[publicID]
		if !exists {
			return ErrInvalidInteraction
		}
		total += len(answer)
		if total > 4<<10 {
			return ErrInvalidInteraction
		}
		if len(question.options) > 0 {
			if _, listed := question.options[answer]; !listed && !question.isOther {
				return ErrInvalidInteraction
			}
		}
		protocolAnswers[question.rawID] = struct {
			Answers []string `json:"answers"`
		}{Answers: []string{answer}}
	}
	client, err := m.client(ctx)
	if err != nil {
		return err
	}
	if err := client.Respond(ctx, pending.rpcID, struct {
		Answers any `json:"answers"`
	}{Answers: protocolAnswers}, nil); err != nil {
		return err
	}
	m.finishPending(state, pending.publicID)
	return m.transition(ctx, state, model.ConversationActive, model.EventInteractionResolved, nil)
}

func (m *Manager) handleInteractionRequest(request protocol.ServerRequest) {
	classified, interaction, ok := m.classifyInteraction(request)
	if !ok {
		return
	}
	state := m.stateByThread(classified.threadID)
	if state == nil {
		return
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	state.mu.Lock()
	if state.codexTurnID != classified.turnID || (state.conversation.Status != model.ConversationActive && state.conversation.Status != model.ConversationWaiting) {
		state.mu.Unlock()
		return
	}
	generation := m.runtimeGeneration()
	if state.runtimeGeneration != generation {
		m.clearEphemeralLocked(state)
		state.runtimeGeneration = generation
	}
	if state.pending != nil {
		state.mu.Unlock()
		return
	}
	if classified.action == model.ApprovalFileChange {
		metadata := state.actions[classified.itemID]
		classified.requiredConsent = append(classified.requiredConsent, consentForMetadata(metadata)...)
		interaction.AffectedPaths = append([]string(nil), metadata.paths...)
		classified.grantKey = fileGrantKey(metadata, classified.itemID)
	}
	previousStatus := state.conversation.Status
	missing := missingConsentLocked(state, classified)
	_, granted := state.grants[classified.grantKey]
	state.mu.Unlock()

	if interaction.Kind == model.InteractionApproval && granted && missing == "" {
		client, err := m.client(m.ctx)
		if err == nil && client.Respond(m.ctx, classified.rpcID, approvalResponse(classified, "accept"), nil) == nil {
			if previousStatus == model.ConversationWaiting {
				_ = m.transition(m.ctx, state, model.ConversationActive, model.EventInteractionResolved, nil)
			}
			return
		}
	}
	if interaction.Kind == model.InteractionApproval && missing != "" {
		interaction.Kind = model.InteractionConsent
		interaction.ConsentKind = missing
	}
	state.mu.Lock()
	state.pending = classified
	state.conversation.PendingInteraction = &interaction
	state.mu.Unlock()
	_ = m.transition(m.ctx, state, model.ConversationWaiting, model.EventInteractionRequested, nil)
}

func (m *Manager) handleServerRequestResolved(params protocol.ServerRequestResolvedNotification) {
	state := m.stateByThread(params.ThreadID)
	if state == nil {
		return
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	state.mu.RLock()
	pending := state.pending
	state.mu.RUnlock()
	if pending == nil || !bytes.Equal(pending.rpcID, params.RequestID) {
		return
	}
	m.finishPending(state, pending.publicID)
	_ = m.transition(m.ctx, state, model.ConversationActive, model.EventInteractionResolved, nil)
}

func (m *Manager) classifyInteraction(request protocol.ServerRequest) (*pendingRequest, model.Interaction, bool) {
	if len(request.ID) == 0 || !json.Valid(request.ID) {
		return nil, model.Interaction{}, false
	}
	publicID := opaqueID()
	if publicID == "" {
		return nil, model.Interaction{}, false
	}
	pending := &pendingRequest{publicID: publicID, rpcID: append(json.RawMessage(nil), request.ID...), method: request.Method, questions: map[string]pendingQuestion{}}
	interaction := model.Interaction{RequestID: publicID}
	switch request.Method {
	case "item/commandExecution/requestApproval":
		var params protocol.CommandApprovalParams
		if json.Unmarshal(request.Params, &params) != nil {
			return nil, model.Interaction{}, false
		}
		pending.threadID, pending.turnID, pending.itemID = params.ThreadID, params.TurnID, params.ItemID
		pending.action = model.ApprovalCommand
		pending.grantKey = commandGrantKey(params.Command)
		pending.requiredConsent = commandConsents(params)
		interaction.Kind, interaction.Action, interaction.Summary = model.InteractionApproval, model.ApprovalCommand, commandSummary(params.Command)
	case "item/fileChange/requestApproval":
		var params protocol.FileChangeApprovalParams
		if json.Unmarshal(request.Params, &params) != nil {
			return nil, model.Interaction{}, false
		}
		pending.threadID, pending.turnID, pending.itemID = params.ThreadID, params.TurnID, params.ItemID
		pending.action, pending.grantKey = model.ApprovalFileChange, "file:"+digestString(params.ItemID)
		interaction.Kind, interaction.Action, interaction.Summary = model.InteractionApproval, model.ApprovalFileChange, "Apply component file changes"
	case "item/permissions/requestApproval":
		var params protocol.PermissionsApprovalParams
		if json.Unmarshal(request.Params, &params) != nil || !json.Valid(params.Permissions) {
			return nil, model.Interaction{}, false
		}
		pending.threadID, pending.turnID, pending.itemID = params.ThreadID, params.TurnID, params.ItemID
		pending.action, pending.permissions = model.ApprovalPermissions, append(json.RawMessage(nil), params.Permissions...)
		pending.grantKey = "permissions:" + digestString(string(params.Permissions))
		if requestsNetwork(params.Permissions) {
			pending.requiredConsent = []model.ConsentKind{model.ConsentNetworkAccess}
		}
		interaction.Kind, interaction.Action, interaction.Summary = model.InteractionApproval, model.ApprovalPermissions, "Grant additional turn permissions"
	case "item/tool/requestUserInput":
		var params protocol.UserInputRequestParams
		if json.Unmarshal(request.Params, &params) != nil || len(params.Questions) == 0 {
			return nil, model.Interaction{}, false
		}
		pending.threadID, pending.turnID, pending.itemID = params.ThreadID, params.TurnID, params.ItemID
		interaction.Kind = model.InteractionQuestion
		for _, question := range params.Questions[:min(3, len(params.Questions))] {
			questionID := opaqueID()
			if questionID == "" {
				questionID = digestString(question.ID)
			}
			options := make([]model.InteractionOption, 0, min(20, len(question.Options)))
			allowed := map[string]struct{}{}
			for _, option := range question.Options[:min(20, len(question.Options))] {
				label := boundedUTF8(option.Label, 256)
				options = append(options, model.InteractionOption{Label: label, Description: boundedUTF8(option.Description, 512)})
				allowed[label] = struct{}{}
			}
			pending.questions[questionID] = pendingQuestion{rawID: question.ID, options: allowed, isOther: question.IsOther}
			interaction.Questions = append(interaction.Questions, model.Question{
				ID: questionID, Header: boundedUTF8(question.Header, 128), Question: boundedUTF8(question.Question, 2048),
				Options: options, IsOther: question.IsOther, IsSecret: question.IsSecret,
			})
		}
	default:
		return nil, model.Interaction{}, false
	}
	return pending, interaction, pending.threadID != "" && pending.turnID != ""
}

func (m *Manager) recordActionMetadataLocked(state *conversationState, raw json.RawMessage) {
	var item struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	if json.Unmarshal(raw, &item) != nil || item.Type != "fileChange" || item.ID == "" {
		return
	}
	metadata := actionMetadata{known: true}
	for _, change := range item.Changes {
		relative, err := filepath.Rel(state.siteRoot, filepath.Clean(change.Path))
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		relative = filepath.ToSlash(relative)
		metadata.paths = append(metadata.paths, relative)
		if protectedPackageFile(filepath.Base(relative)) {
			metadata.dependencyChange = true
		}
	}
	sort.Strings(metadata.paths)
	state.actions[item.ID] = metadata
}

func (m *Manager) initializeEphemeral(state *conversationState) {
	state.resolved = map[string]struct{}{}
	state.grants = map[string]struct{}{}
	state.consents = map[model.ConsentKind]bool{}
	state.actions = map[string]actionMetadata{}
	state.runtimeGeneration = m.runtimeGeneration()
}

func (m *Manager) clearEphemeralLocked(state *conversationState) {
	state.pending = nil
	state.conversation.PendingInteraction = nil
	state.resolved = map[string]struct{}{}
	state.grants = map[string]struct{}{}
	state.consents = map[model.ConsentKind]bool{}
	state.actions = map[string]actionMetadata{}
}

func (m *Manager) runtimeGeneration() uint64 {
	if m.options.RuntimeGeneration == nil {
		return 0
	}
	return m.options.RuntimeGeneration()
}

func pendingForResolution(state *conversationState, requestID string) (*pendingRequest, error) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.pending != nil && state.pending.publicID == requestID {
		return state.pending, nil
	}
	if _, resolved := state.resolved[requestID]; resolved {
		return nil, ErrInteractionResolved
	}
	return nil, ErrInteractionNotFound
}

func (m *Manager) finishPending(state *conversationState, publicID string) {
	state.mu.Lock()
	state.resolved[publicID] = struct{}{}
	state.pending = nil
	state.conversation.PendingInteraction = nil
	state.mu.Unlock()
}

func missingConsentLocked(state *conversationState, pending *pendingRequest) model.ConsentKind {
	for _, kind := range pending.requiredConsent {
		if !state.consents[kind] {
			return kind
		}
	}
	return ""
}

func approvalResponse(pending *pendingRequest, decision string) any {
	if pending.action != model.ApprovalPermissions {
		return protocol.ApprovalResponse{Decision: decision}
	}
	if decision != "accept" {
		return struct {
			Permissions map[string]any `json:"permissions"`
			Scope       string         `json:"scope"`
		}{Permissions: map[string]any{}, Scope: "turn"}
	}
	return struct {
		Permissions json.RawMessage `json:"permissions"`
		Scope       string          `json:"scope"`
	}{Permissions: pending.permissions, Scope: "turn"}
}

func commandConsents(params protocol.CommandApprovalParams) []model.ConsentKind {
	var result []model.ConsentKind
	if dependencyCommand(params.Command) {
		result = append(result, model.ConsentDependencyChange)
	}
	if rawPresent(params.NetworkContext) || requestsNetwork(params.AdditionalPermissions) || networkCommand(params.Command) {
		result = append(result, model.ConsentNetworkAccess)
	}
	return result
}

func consentForMetadata(metadata actionMetadata) []model.ConsentKind {
	if metadata.dependencyChange || !metadata.known || len(metadata.paths) == 0 {
		return []model.ConsentKind{model.ConsentDependencyChange}
	}
	return nil
}

func commandGrantKey(command string) string {
	fields := strings.Fields(command)
	executable := "command"
	if len(fields) > 0 {
		executable = filepath.Base(fields[0])
	}
	return "command:" + executable + ":" + digestString(strings.Join(fields, " "))
}

func fileGrantKey(metadata actionMetadata, itemID string) string {
	if len(metadata.paths) == 0 {
		return "file:" + digestString(itemID)
	}
	return "file:" + digestString(strings.Join(metadata.paths, "\x00"))
}

func commandSummary(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "Run a project command"
	}
	return fmt.Sprintf("Run %s with %d argument(s)", boundedUTF8(filepath.Base(fields[0]), 64), len(fields)-1)
}

func dependencyCommand(command string) bool {
	fields := strings.Fields(strings.ToLower(command))
	if len(fields) < 2 {
		return false
	}
	executable := filepath.Base(fields[0])
	if executable != "bun" && executable != "npm" && executable != "pnpm" && executable != "yarn" {
		return false
	}
	for _, field := range fields[1:] {
		switch field {
		case "add", "install", "remove", "uninstall", "update", "upgrade":
			return true
		}
	}
	return false
}

func networkCommand(command string) bool {
	fields := strings.Fields(strings.ToLower(command))
	if len(fields) == 0 {
		return false
	}
	switch filepath.Base(fields[0]) {
	case "curl", "wget", "ssh", "scp":
		return true
	default:
		return false
	}
}

func requestsNetwork(raw json.RawMessage) bool {
	if !rawPresent(raw) {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return containsEnabledNetwork(value)
}

func containsEnabledNetwork(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, "network") {
				if network, ok := child.(map[string]any); ok {
					if enabled, exists := network["enabled"]; !exists || enabled == true {
						return true
					}
				}
			}
			if containsEnabledNetwork(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsEnabledNetwork(child) {
				return true
			}
		}
	}
	return false
}

func rawPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("{}"))
}

func protectedPackageFile(name string) bool {
	switch name {
	case "package.json", "bun.lock", "bun.lockb", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return true
	default:
		return false
	}
}

func validApprovalDecision(decision ApprovalDecision) bool {
	return decision == ApprovalApproveOnce || decision == ApprovalApproveGeneration || decision == ApprovalDecline || decision == ApprovalCancel
}

func digestString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}
