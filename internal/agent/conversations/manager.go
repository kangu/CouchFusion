package conversations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nuxt-apps/couchfusion/internal/agent/baseline"
	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/prompt"
	"github.com/nuxt-apps/couchfusion/internal/agent/protocol"
	"github.com/nuxt-apps/couchfusion/internal/agent/store"
	"github.com/nuxt-apps/couchfusion/internal/mutationlease"
)

const (
	defaultEventLimit     = 256
	defaultEventTextLimit = 1 << 20
	idempotencyLimit      = 256
	idempotencyTTL        = 10 * time.Minute
)

var (
	ErrNotFound       = errors.New("agent conversation was not found")
	ErrConflict       = errors.New("agent conversation has an active turn")
	ErrBindingChanged = errors.New("agent conversation site binding changed")
	ErrUnavailable    = errors.New("agent conversation service is unavailable")
)

type RPCClient interface {
	Call(context.Context, string, any, any) error
	Respond(context.Context, json.RawMessage, any, *protocol.RPCError) error
}

type ClientProvider func(context.Context) (RPCClient, error)
type CaptureFunc func(context.Context, string) (baseline.Snapshot, error)
type CompareFunc func(context.Context, string, baseline.Snapshot) ([]model.ChangedFile, error)
type FingerprintFunc func(string, string, string, string) string

type CompletionRequest struct {
	WorkspaceID  string
	SiteID       string
	SiteRoot     string
	ChangedFiles []model.ChangedFile
}

type CompletionHook interface {
	Complete(context.Context, CompletionRequest) model.RegistryResult
}

type Options struct {
	Repository        store.Repository
	Leases            *mutationlease.Manager
	Skill             prompt.Skill
	Client            ClientProvider
	Events            <-chan protocol.Notification
	Requests          <-chan protocol.ServerRequest
	Capture           CaptureFunc
	Compare           CompareFunc
	Fingerprint       FingerprintFunc
	Completion        CompletionHook
	ShutdownClient    func(context.Context) error
	RuntimeGeneration func() uint64
	EventLimit        int
	EventTextLimit    int
	Now               func() time.Time
	ID                func() string
}

type CreateRequest struct {
	WorkspaceID    string
	SiteID         string
	AppsRoot       string
	SiteRoot       string
	Prompt         string
	IdempotencyKey string
}

type TurnRequest struct {
	Prompt         string
	IdempotencyKey string
}

type RecoveryBinding struct {
	WorkspaceID string
	SiteID      string
	AppsRoot    string
	SiteRoot    string
}

type idempotencyEntry struct {
	conversationID string
	expiresAt      time.Time
}

type Manager struct {
	mu          sync.RWMutex
	options     Options
	states      map[string]*conversationState
	threads     map[string]string
	idempotency map[string]idempotencyEntry
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

type conversationState struct {
	opMu sync.Mutex
	mu   sync.RWMutex

	conversation      model.Conversation
	document          store.Document
	appsRoot          string
	siteRoot          string
	baseline          baseline.Snapshot
	codexThreadID     string
	codexTurnID       string
	release           func()
	pending           *pendingRequest
	resolved          map[string]struct{}
	grants            map[string]struct{}
	consents          map[model.ConsentKind]bool
	actions           map[string]actionMetadata
	runtimeGeneration uint64
	history           []model.Event
	historyText       int
	subscribers       map[uint64]chan model.Event
	nextSubscriber    uint64
}

func NewManager(options Options) *Manager {
	if options.Leases == nil {
		options.Leases = mutationlease.NewManager()
	}
	if options.Capture == nil {
		options.Capture = baseline.Capture
	}
	if options.Compare == nil {
		options.Compare = baseline.Compare
	}
	if options.Fingerprint == nil {
		options.Fingerprint = baseline.Fingerprint
	}
	if options.EventLimit <= 0 {
		options.EventLimit = defaultEventLimit
	}
	if options.EventTextLimit <= 0 {
		options.EventTextLimit = defaultEventTextLimit
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.ID == nil {
		options.ID = opaqueID
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		options: options, states: map[string]*conversationState{}, threads: map[string]string{},
		idempotency: map[string]idempotencyEntry{}, ctx: ctx, cancel: cancel,
	}
	manager.wg.Add(2)
	go manager.runEvents()
	go manager.runRequests()
	return manager
}

func (m *Manager) Create(ctx context.Context, request CreateRequest) (model.Conversation, error) {
	input, err := prompt.InitialTurn(request.Prompt, m.options.Skill)
	if err != nil {
		return model.Conversation{}, err
	}
	if strings.TrimSpace(request.WorkspaceID) == "" || strings.TrimSpace(request.SiteID) == "" ||
		strings.TrimSpace(request.AppsRoot) == "" || strings.TrimSpace(request.SiteRoot) == "" {
		return model.Conversation{}, fmt.Errorf("%w: trusted site binding is incomplete", ErrBindingChanged)
	}
	if existing, ok := m.idempotentConversation(request); ok {
		return existing, nil
	}
	conversationID := m.options.ID()
	if strings.TrimSpace(conversationID) == "" {
		return model.Conversation{}, ErrUnavailable
	}
	release, err := m.options.Leases.Acquire(request.WorkspaceID, conversationID, "agent_conversation")
	if err != nil {
		return model.Conversation{}, err
	}
	releaseOnError := true
	defer func() {
		if releaseOnError {
			release()
		}
	}()

	snapshot, err := m.options.Capture(ctx, request.SiteRoot)
	if err != nil {
		return model.Conversation{}, err
	}
	client, err := m.client(ctx)
	if err != nil {
		return model.Conversation{}, err
	}
	var threadResponse protocol.ThreadResponse
	threadParams := protocol.ThreadStartParams{
		CWD: request.SiteRoot, ApprovalPolicy: "on-request", ApprovalsReviewer: "user",
		DeveloperInstructions: prompt.DeveloperInstructions(), Sandbox: "workspace-write",
		RuntimeWorkspaceRoots: []string{request.SiteRoot},
	}
	if err := client.Call(ctx, "thread/start", threadParams, &threadResponse); err != nil {
		return model.Conversation{}, err
	}
	if strings.TrimSpace(threadResponse.Thread.ID) == "" {
		return model.Conversation{}, errors.New("Codex returned an empty thread ID")
	}

	now := m.options.Now()
	document := store.Document{
		Type: store.DocumentType, SchemaVersion: store.CurrentSchemaVersion,
		ConversationID: conversationID, CodexThreadID: threadResponse.Thread.ID,
		WorkspaceID: request.WorkspaceID, SiteID: request.SiteID,
		SiteRootFingerprint: m.options.Fingerprint(request.WorkspaceID, request.SiteID, request.AppsRoot, request.SiteRoot),
		Title:               titleFromPrompt(request.Prompt), Status: store.Status("starting"), Baseline: storeBaseline(snapshot),
		CreatedAt: now, UpdatedAt: now,
	}
	document, err = m.options.Repository.Create(ctx, document)
	if err != nil {
		return model.Conversation{}, err
	}
	state := &conversationState{
		conversation: documentConversation(document, true), document: document,
		appsRoot: request.AppsRoot, siteRoot: request.SiteRoot, baseline: snapshot,
		codexThreadID: document.CodexThreadID, release: release, subscribers: map[uint64]chan model.Event{},
	}
	m.initializeEphemeral(state)
	m.addState(state)
	state.opMu.Lock()

	var turnResponse protocol.TurnStartResponse
	if err := client.Call(ctx, "turn/start", turnStartParams(document.CodexThreadID, request.SiteRoot, input), &turnResponse); err != nil {
		_ = m.transition(ctx, state, model.ConversationFailed, model.EventConversationFailed, nil)
		state.opMu.Unlock()
		m.releaseStateLease(state)
		return model.Conversation{}, err
	}
	if strings.TrimSpace(turnResponse.Turn.ID) == "" {
		_ = m.transition(ctx, state, model.ConversationFailed, model.EventConversationFailed, nil)
		state.opMu.Unlock()
		m.releaseStateLease(state)
		return model.Conversation{}, errors.New("Codex returned an empty turn ID")
	}
	state.mu.Lock()
	state.codexTurnID = turnResponse.Turn.ID
	state.conversation.ActiveTurn = &model.ConversationTurn{ID: turnResponse.Turn.ID, Status: model.TurnActive, StartedAt: now}
	state.mu.Unlock()
	if err := m.transition(ctx, state, model.ConversationActive, model.EventTurnStarted, nil); err != nil {
		state.opMu.Unlock()
		m.releaseStateLease(state)
		return model.Conversation{}, err
	}
	state.opMu.Unlock()
	releaseOnError = false
	m.rememberIdempotency(request, conversationID)
	return state.snapshot(), nil
}

func (m *Manager) StartTurn(ctx context.Context, conversationID string, request TurnRequest) (model.Conversation, error) {
	input, err := prompt.FollowUp(request.Prompt)
	if err != nil {
		return model.Conversation{}, err
	}
	state, ok := m.state(conversationID)
	if !ok {
		return model.Conversation{}, ErrNotFound
	}
	if existing, ok := m.idempotentTurn(conversationID, request.IdempotencyKey); ok {
		return existing, nil
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	current := state.snapshot()
	if current.ReadOnly || !current.BindingValid {
		return model.Conversation{}, ErrBindingChanged
	}
	if current.ActiveTurn != nil || (current.Status != model.ConversationReady && current.Status != model.ConversationInterrupted && current.Status != model.ConversationFailed) {
		return model.Conversation{}, ErrConflict
	}
	release, err := m.options.Leases.Acquire(current.WorkspaceID, conversationID, "agent_conversation")
	if err != nil {
		return model.Conversation{}, err
	}
	client, err := m.client(ctx)
	if err != nil {
		release()
		return model.Conversation{}, err
	}
	state.mu.RLock()
	threadID, siteRoot := state.codexThreadID, state.siteRoot
	state.mu.RUnlock()
	var response protocol.TurnStartResponse
	if err := client.Call(ctx, "turn/start", turnStartParams(threadID, siteRoot, input), &response); err != nil {
		release()
		return model.Conversation{}, err
	}
	if strings.TrimSpace(response.Turn.ID) == "" {
		release()
		return model.Conversation{}, errors.New("Codex returned an empty turn ID")
	}
	now := m.options.Now()
	state.mu.Lock()
	state.release = release
	state.codexTurnID = response.Turn.ID
	state.conversation.ActiveTurn = &model.ConversationTurn{ID: response.Turn.ID, Status: model.TurnActive, StartedAt: now}
	state.conversation.PendingInteraction = nil
	m.clearEphemeralLocked(state)
	state.runtimeGeneration = m.runtimeGeneration()
	state.mu.Unlock()
	if err := m.transition(ctx, state, model.ConversationActive, model.EventTurnStarted, nil); err != nil {
		m.releaseStateLease(state)
		return model.Conversation{}, err
	}
	m.rememberTurnIdempotency(conversationID, request.IdempotencyKey)
	return state.snapshot(), nil
}

func (m *Manager) Interrupt(ctx context.Context, conversationID string) error {
	state, ok := m.state(conversationID)
	if !ok {
		return ErrNotFound
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	state.mu.RLock()
	status, threadID, turnID := state.conversation.Status, state.codexThreadID, state.codexTurnID
	state.mu.RUnlock()
	if status != model.ConversationActive && status != model.ConversationWaiting {
		return ErrConflict
	}
	state.mu.Lock()
	m.clearEphemeralLocked(state)
	state.mu.Unlock()
	if err := m.transition(ctx, state, model.ConversationInterrupting, model.EventConversationInterrupted, nil); err != nil {
		return err
	}
	client, err := m.client(ctx)
	if err != nil {
		return err
	}
	return client.Call(ctx, "turn/interrupt", protocol.TurnInterruptParams{ThreadID: threadID, TurnID: turnID}, nil)
}

func (m *Manager) ListSite(ctx context.Context, workspaceID, siteID string) ([]model.Conversation, error) {
	documents, err := m.options.Repository.ListSite(ctx, workspaceID, siteID)
	if err != nil {
		return nil, err
	}
	result := make([]model.Conversation, 0, len(documents))
	for _, document := range documents {
		if state, ok := m.state(document.ConversationID); ok {
			result = append(result, state.snapshot())
		} else {
			result = append(result, documentConversation(document, !document.ReadOnly))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

func (m *Manager) Get(ctx context.Context, conversationID string) (model.Conversation, error) {
	if state, ok := m.state(conversationID); ok {
		return state.snapshot(), nil
	}
	document, err := m.options.Repository.Get(ctx, conversationID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Conversation{}, ErrNotFound
	}
	if err != nil && !errors.Is(err, store.ErrNewerSchema) {
		return model.Conversation{}, err
	}
	return documentConversation(document, !document.ReadOnly), nil
}

func (m *Manager) Subscribe(ctx context.Context, conversationID string, after uint64) (<-chan model.Event, error) {
	state, ok := m.state(conversationID)
	if !ok {
		return nil, ErrNotFound
	}
	state.mu.Lock()
	state.nextSubscriber++
	id := state.nextSubscriber
	channel := make(chan model.Event, m.options.EventLimit+1)
	state.subscribers[id] = channel
	tooOld := len(state.history) > 0 && after+1 < state.history[0].Sequence
	if after > state.conversation.Sequence || len(state.history) == 0 || tooOld {
		channel <- state.snapshotEventLocked()
	} else {
		for _, event := range state.history {
			if event.Sequence > after {
				channel <- cloneEvent(event)
			}
		}
	}
	state.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-m.ctx.Done():
		}
		state.mu.Lock()
		if existing, exists := state.subscribers[id]; exists {
			delete(state.subscribers, id)
			close(existing)
		}
		state.mu.Unlock()
	}()
	return channel, nil
}

func (m *Manager) Recover(ctx context.Context, bindings []RecoveryBinding) error {
	if err := m.options.Repository.MarkActiveInterrupted(ctx); err != nil {
		return err
	}
	for _, binding := range bindings {
		documents, err := m.options.Repository.ListSite(ctx, binding.WorkspaceID, binding.SiteID)
		if err != nil {
			return err
		}
		for _, document := range documents {
			valid := document.SiteRootFingerprint == m.options.Fingerprint(binding.WorkspaceID, binding.SiteID, binding.AppsRoot, binding.SiteRoot)
			state := &conversationState{
				conversation: documentConversation(document, valid && !document.ReadOnly), document: document,
				appsRoot: binding.AppsRoot, siteRoot: binding.SiteRoot, baseline: snapshotFromStore(document.Baseline),
				codexThreadID: document.CodexThreadID, subscribers: map[uint64]chan model.Event{},
			}
			m.initializeEphemeral(state)
			if !valid {
				state.conversation.ReadOnly = true
				state.conversation.BindingValid = false
				m.addState(state)
				continue
			}
			client, err := m.client(ctx)
			if err != nil {
				return err
			}
			var response protocol.ThreadResponse
			if err := client.Call(ctx, "thread/read", protocol.ThreadReadParams{ThreadID: document.CodexThreadID, IncludeTurns: true}, &response); err != nil {
				return err
			}
			for _, turn := range response.Thread.Turns {
				for _, raw := range turn.Items {
					if item, ok := normalizeItem(raw, turn.ID); ok {
						state.conversation.Items = append(state.conversation.Items, item)
					}
				}
			}
			state.conversation.PendingInteraction = nil
			m.trimItems(state)
			m.addState(state)
		}
	}
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.cancel()
	m.mu.RLock()
	states := make([]*conversationState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	m.mu.RUnlock()
	for _, state := range states {
		m.releaseStateLease(state)
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	if m.options.ShutdownClient != nil {
		return m.options.ShutdownClient(ctx)
	}
	return nil
}

func (m *Manager) runEvents() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case notification, ok := <-m.options.Events:
			if !ok {
				return
			}
			m.handleNotification(notification)
		}
	}
}

func (m *Manager) runRequests() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case request, ok := <-m.options.Requests:
			if !ok {
				return
			}
			m.handleRequest(request)
		}
	}
}

func (m *Manager) handleNotification(notification protocol.Notification) {
	switch notification.Method {
	case "turn/completed":
		var params protocol.TurnNotification
		if json.Unmarshal(notification.Params, &params) == nil {
			m.handleTurnCompleted(params)
		}
	case "item/started", "item/completed":
		var params protocol.ItemNotification
		if json.Unmarshal(notification.Params, &params) == nil {
			m.handleItem(notification.Method, params)
		}
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta":
		m.handleTextDelta(notification.Method, notification.Params)
	case "serverRequest/resolved":
		var params protocol.ServerRequestResolvedNotification
		if json.Unmarshal(notification.Params, &params) == nil {
			m.handleServerRequestResolved(params)
		}
	}
}

func (m *Manager) handleRequest(request protocol.ServerRequest) {
	m.handleInteractionRequest(request)
}

func (m *Manager) handleItem(method string, params protocol.ItemNotification) {
	state := m.stateByThread(params.ThreadID)
	if state == nil {
		return
	}
	item, ok := normalizeItem(params.Item, params.TurnID)
	if !ok {
		return
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	state.mu.Lock()
	if state.codexTurnID != params.TurnID {
		state.mu.Unlock()
		return
	}
	m.recordActionMetadataLocked(state, params.Item)
	found := false
	for index := range state.conversation.Items {
		if state.conversation.Items[index].ID == item.ID {
			state.conversation.Items[index] = item
			found = true
			break
		}
	}
	if !found {
		state.conversation.Items = append(state.conversation.Items, item)
	}
	m.trimItemsLocked(state)
	eventType := model.EventItemStarted
	if method == "item/completed" {
		eventType = model.EventItemCompleted
	}
	m.publishLocked(state, model.Event{Type: eventType, TurnID: params.TurnID, Item: &item})
	state.mu.Unlock()
}

func (m *Manager) handleTextDelta(method string, raw json.RawMessage) {
	var params struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Delta    string `json:"delta"`
	}
	if json.Unmarshal(raw, &params) != nil || params.ItemID == "" || params.Delta == "" {
		return
	}
	state := m.stateByThread(params.ThreadID)
	if state == nil {
		return
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.codexTurnID != params.TurnID {
		return
	}
	itemID := safeItemID(params.TurnID, params.ItemID)
	itemType := "agentMessage"
	if method == "item/reasoning/summaryTextDelta" {
		itemType = "reasoning"
	}
	index := -1
	for candidate := range state.conversation.Items {
		if state.conversation.Items[candidate].ID == itemID {
			index = candidate
			break
		}
	}
	if index < 0 {
		state.conversation.Items = append(state.conversation.Items, model.ConversationItem{ID: itemID, TurnID: params.TurnID, Type: itemType, Status: "inProgress"})
		index = len(state.conversation.Items) - 1
	}
	state.conversation.Items[index].Text = boundedUTF8(state.conversation.Items[index].Text+params.Delta, 64<<10)
	item := state.conversation.Items[index]
	m.trimItemsLocked(state)
	m.publishLocked(state, model.Event{Type: model.EventItemDelta, TurnID: params.TurnID, Item: &item})
}

func (m *Manager) handleTurnCompleted(params protocol.TurnNotification) {
	state := m.stateByThread(params.ThreadID)
	if state == nil {
		return
	}
	state.opMu.Lock()
	defer state.opMu.Unlock()
	state.mu.RLock()
	activeTurn := state.codexTurnID
	state.mu.RUnlock()
	if params.Turn.ID == "" || params.Turn.ID != activeTurn {
		return
	}
	state.mu.Lock()
	m.clearEphemeralLocked(state)
	state.mu.Unlock()
	switch strings.ToLower(params.Turn.Status) {
	case "completed":
		m.completeSuccessfulTurn(state)
	case "interrupted", "cancelled", "canceled":
		m.setLastTurn(state, model.TurnInterrupted)
		_ = m.transition(m.ctx, state, model.ConversationInterrupted, model.EventConversationInterrupted, nil)
		m.clearActiveTurn(state)
		_ = m.transition(m.ctx, state, model.ConversationReady, model.EventTurnCompleted, nil)
	default:
		m.setLastTurn(state, model.TurnFailed)
		_ = m.transition(m.ctx, state, model.ConversationFailed, model.EventConversationFailed, nil)
		m.clearActiveTurn(state)
		_ = m.transition(m.ctx, state, model.ConversationReady, model.EventTurnCompleted, nil)
	}
	m.releaseStateLease(state)
}

func (m *Manager) completeSuccessfulTurn(state *conversationState) {
	m.setLastTurn(state, model.TurnCompleted)
	if err := m.transition(m.ctx, state, model.ConversationCompleted, model.EventTurnCompleted, nil); err != nil {
		m.clearActiveTurn(state)
		return
	}
	state.mu.RLock()
	siteRoot, snapshot := state.siteRoot, state.baseline
	state.mu.RUnlock()
	changes, err := m.options.Compare(m.ctx, siteRoot, snapshot)
	if err != nil {
		m.clearActiveTurn(state)
		result := model.RegistryResult{State: model.RegistryFailed, Problem: "Changed files could not be evaluated"}
		_ = m.persistCompletion(m.ctx, state, nil, result)
		_ = m.transition(m.ctx, state, model.ConversationReady, model.EventRegistryFailed, nil)
		return
	}
	if err := m.persistCompletion(m.ctx, state, changes, model.RegistryResult{}); err != nil {
		m.clearActiveTurn(state)
		_ = m.transition(m.ctx, state, model.ConversationReady, model.EventRegistryFailed, nil)
		return
	}
	m.clearActiveTurn(state)
	if !hasRegistryInputs(changes) {
		result := model.RegistryResult{State: model.RegistryNotNeeded}
		_ = m.transition(m.ctx, state, model.ConversationRebuildingRegistry, model.EventTurnCompleted, nil)
		_ = m.persistCompletion(m.ctx, state, changes, result)
		_ = m.transition(m.ctx, state, model.ConversationReady, model.EventRegistryCompleted, nil)
		return
	}
	running := model.RegistryResult{State: model.RegistryRunning}
	_ = m.persistCompletion(m.ctx, state, changes, running)
	if err := m.transition(m.ctx, state, model.ConversationRebuildingRegistry, model.EventRegistryStarted, nil); err != nil {
		return
	}
	result := model.RegistryResult{State: model.RegistryFailed, Problem: "Registry completion is unavailable"}
	if m.options.Completion != nil {
		state.mu.RLock()
		request := CompletionRequest{
			WorkspaceID: state.conversation.WorkspaceID, SiteID: state.conversation.SiteID,
			SiteRoot: state.siteRoot, ChangedFiles: append([]model.ChangedFile(nil), changes...),
		}
		state.mu.RUnlock()
		result = normalizeRegistryResult(m.options.Completion.Complete(m.ctx, request))
	}
	_ = m.persistCompletion(m.ctx, state, changes, result)
	eventType := model.EventRegistryCompleted
	if result.State == model.RegistryFailed {
		eventType = model.EventRegistryFailed
	}
	_ = m.transition(m.ctx, state, model.ConversationReady, eventType, nil)
}

func (m *Manager) persistCompletion(ctx context.Context, state *conversationState, changes []model.ChangedFile, result model.RegistryResult) error {
	state.mu.RLock()
	document := state.document
	state.mu.RUnlock()
	if changes != nil {
		document.ChangedFiles = storeChangedFiles(changes)
	}
	if result.State != "" {
		document.Registry = &store.RegistryResult{
			State: string(result.State), Problem: result.Problem, GeneratedComponentID: result.GeneratedComponentID,
		}
		if result.GeneratedComponentID != "" {
			document.GeneratedComponentID = result.GeneratedComponentID
		}
	}
	updated, err := m.options.Repository.Update(ctx, document)
	if err != nil {
		return err
	}
	state.mu.Lock()
	state.document = updated
	if changes != nil {
		state.conversation.ChangedFiles = append([]model.ChangedFile(nil), changes...)
	}
	if result.State != "" {
		state.conversation.Registry = result
		if result.GeneratedComponentID != "" {
			state.conversation.GeneratedComponentID = result.GeneratedComponentID
		}
	}
	state.mu.Unlock()
	return nil
}

func (m *Manager) transition(ctx context.Context, state *conversationState, status model.ConversationStatus, eventType model.EventType, item *model.ConversationItem) error {
	state.mu.RLock()
	document := state.document
	document.Status = store.Status(status)
	if state.conversation.LastTurn != nil {
		document.LastTurn = &store.LastTurn{Status: string(state.conversation.LastTurn.Status), Summary: state.conversation.LastTurn.Summary}
	}
	state.mu.RUnlock()
	updated, err := m.options.Repository.Update(ctx, document)
	if err != nil {
		return err
	}
	state.mu.Lock()
	state.document = updated
	state.conversation.Status = status
	state.conversation.UpdatedAt = m.options.Now()
	event := model.Event{Type: eventType, Status: status, Item: item}
	if state.conversation.ActiveTurn != nil {
		event.TurnID = state.conversation.ActiveTurn.ID
	}
	m.publishLocked(state, event)
	state.mu.Unlock()
	return nil
}

func (m *Manager) publishLocked(state *conversationState, event model.Event) {
	state.conversation.Sequence++
	event.Sequence = state.conversation.Sequence
	event.ConversationID = state.conversation.ID
	event.CreatedAt = m.options.Now()
	event = cloneEvent(event)
	state.history = append(state.history, event)
	state.historyText += eventTextSize(event)
	for len(state.history) > m.options.EventLimit || state.historyText > m.options.EventTextLimit {
		state.historyText -= eventTextSize(state.history[0])
		state.history = state.history[1:]
	}
	m.trimItemsLocked(state)
	for _, subscriber := range state.subscribers {
		select {
		case subscriber <- cloneEvent(event):
		default:
			for len(subscriber) > 0 {
				<-subscriber
			}
			subscriber <- state.snapshotEventLocked()
		}
	}
}

func (m *Manager) addState(state *conversationState) {
	m.mu.Lock()
	m.states[state.conversation.ID] = state
	m.threads[state.codexThreadID] = state.conversation.ID
	m.mu.Unlock()
}

func (m *Manager) state(id string) (*conversationState, bool) {
	m.mu.RLock()
	state, ok := m.states[id]
	m.mu.RUnlock()
	return state, ok
}

func (m *Manager) stateByThread(threadID string) *conversationState {
	m.mu.RLock()
	id := m.threads[threadID]
	state := m.states[id]
	m.mu.RUnlock()
	return state
}

func (m *Manager) client(ctx context.Context) (RPCClient, error) {
	if m.options.Repository == nil || m.options.Client == nil {
		return nil, ErrUnavailable
	}
	client, err := m.options.Client(ctx)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrUnavailable
	}
	return client, nil
}

func (m *Manager) setLastTurn(state *conversationState, status model.TurnStatus) {
	state.mu.Lock()
	state.conversation.LastTurn = &model.LastTurn{Status: status}
	if state.conversation.ActiveTurn != nil {
		state.conversation.ActiveTurn.Status = status
	}
	state.mu.Unlock()
}

func (m *Manager) clearActiveTurn(state *conversationState) {
	state.mu.Lock()
	state.conversation.ActiveTurn = nil
	state.conversation.PendingInteraction = nil
	state.codexTurnID = ""
	m.clearEphemeralLocked(state)
	state.mu.Unlock()
}

func (m *Manager) releaseStateLease(state *conversationState) {
	state.mu.Lock()
	release := state.release
	state.release = nil
	state.mu.Unlock()
	if release != nil {
		release()
	}
}

func (m *Manager) trimItems(state *conversationState) {
	state.mu.Lock()
	m.trimItemsLocked(state)
	state.mu.Unlock()
}

func (m *Manager) trimItemsLocked(state *conversationState) {
	total := 0
	for _, item := range state.conversation.Items {
		total += len(item.Text)
	}
	for len(state.conversation.Items) > m.options.EventLimit || (len(state.conversation.Items) > 0 && total+state.historyText > m.options.EventTextLimit) {
		total -= len(state.conversation.Items[0].Text)
		state.conversation.Items = state.conversation.Items[1:]
	}
}

func (m *Manager) idempotentTurn(conversationID, key string) (model.Conversation, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return model.Conversation{}, false
	}
	return m.idempotentByKey("turn\x00" + conversationID + "\x00" + key)
}

func (m *Manager) rememberTurnIdempotency(conversationID, key string) {
	key = strings.TrimSpace(key)
	if key != "" {
		m.rememberByKey("turn\x00"+conversationID+"\x00"+key, conversationID)
	}
}

func (m *Manager) idempotentConversation(request CreateRequest) (model.Conversation, bool) {
	key := idempotencyKey(request.WorkspaceID, request.SiteID, request.IdempotencyKey)
	if key == "" {
		return model.Conversation{}, false
	}
	return m.idempotentByKey("create\x00" + key)
}

func (m *Manager) idempotentByKey(key string) (model.Conversation, bool) {
	now := m.options.Now()
	m.mu.Lock()
	entry, ok := m.idempotency[key]
	if ok && !entry.expiresAt.After(now) {
		delete(m.idempotency, key)
		ok = false
	}
	state := m.states[entry.conversationID]
	m.mu.Unlock()
	if !ok || state == nil {
		return model.Conversation{}, false
	}
	return state.snapshot(), true
}

func (m *Manager) rememberIdempotency(request CreateRequest, conversationID string) {
	key := idempotencyKey(request.WorkspaceID, request.SiteID, request.IdempotencyKey)
	if key == "" {
		return
	}
	m.rememberByKey("create\x00"+key, conversationID)
}

func (m *Manager) rememberByKey(key, conversationID string) {
	m.mu.Lock()
	if len(m.idempotency) >= idempotencyLimit {
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range m.idempotency {
			if oldestKey == "" || entry.expiresAt.Before(oldest) {
				oldestKey, oldest = candidate, entry.expiresAt
			}
		}
		delete(m.idempotency, oldestKey)
	}
	m.idempotency[key] = idempotencyEntry{conversationID: conversationID, expiresAt: m.options.Now().Add(idempotencyTTL)}
	m.mu.Unlock()
}

func (s *conversationState) snapshot() model.Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConversation(s.conversation)
}

func (s *conversationState) snapshotEventLocked() model.Event {
	snapshot := cloneConversation(s.conversation)
	return model.Event{
		Sequence: snapshot.Sequence, Type: model.EventConversationSnapshot,
		ConversationID: snapshot.ID, Snapshot: &snapshot, CreatedAt: time.Now().UTC(),
	}
}

func turnStartParams(threadID, siteRoot string, input protocol.TurnInput) protocol.TurnStartParams {
	return protocol.TurnStartParams{
		ThreadID: threadID, Input: input, CWD: siteRoot,
		ApprovalPolicy: "on-request", ApprovalsReviewer: "user",
		SandboxPolicy:         &protocol.SandboxPolicy{Type: "workspace-write", WritableRoots: []string{siteRoot}, NetworkAccess: false},
		RuntimeWorkspaceRoots: []string{siteRoot},
	}
}

func normalizeItem(raw json.RawMessage, turnID string) (model.ConversationItem, bool) {
	var item struct {
		ID      string   `json:"id"`
		Type    string   `json:"type"`
		Status  string   `json:"status"`
		Text    string   `json:"text"`
		Summary []string `json:"summary"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &item) != nil || item.Type == "" {
		return model.ConversationItem{}, false
	}
	text := ""
	switch item.Type {
	case "userMessage":
		var parts []string
		for _, content := range item.Content {
			if content.Type == "text" && !strings.HasPrefix(strings.TrimSpace(content.Text), "$couchfusion-component-builder") {
				parts = append(parts, content.Text)
			}
		}
		text = strings.Join(parts, "\n")
	case "agentMessage":
		text = item.Text
	case "reasoning":
		text = strings.Join(item.Summary, "\n")
	case "commandExecution":
		text = "Command execution"
	case "fileChange":
		text = "File changes"
	default:
		text = ""
	}
	text = boundedUTF8(text, 64<<10)
	return model.ConversationItem{
		ID: safeItemID(turnID, item.ID), TurnID: boundedUTF8(turnID, 256),
		Type: boundedUTF8(item.Type, 64), Status: boundedUTF8(item.Status, 64), Text: text,
	}, true
}

func safeItemID(turnID, itemID string) string {
	digest := sha256.Sum256([]byte(turnID + "\x00" + itemID))
	return hex.EncodeToString(digest[:16])
}

func documentConversation(document store.Document, bindingValid bool) model.Conversation {
	conversation := model.Conversation{
		ID: document.ConversationID, WorkspaceID: document.WorkspaceID, SiteID: document.SiteID,
		Title: document.Title, Status: model.ConversationStatus(document.Status),
		GeneratedComponentID: document.GeneratedComponentID, BindingValid: bindingValid,
		Registry: model.RegistryResult{State: model.RegistryNotNeeded},
		ReadOnly: document.ReadOnly || !bindingValid, CreatedAt: document.CreatedAt, UpdatedAt: document.UpdatedAt,
	}
	if document.LastTurn != nil {
		conversation.LastTurn = &model.LastTurn{Status: model.TurnStatus(document.LastTurn.Status), Summary: document.LastTurn.Summary}
	}
	if document.Registry != nil {
		conversation.Registry = model.RegistryResult{
			State: model.RegistryState(document.Registry.State), Problem: document.Registry.Problem,
			GeneratedComponentID: document.Registry.GeneratedComponentID,
		}
	}
	for _, changed := range document.ChangedFiles {
		conversation.ChangedFiles = append(conversation.ChangedFiles, model.ChangedFile{
			Path: changed.Path, Kind: model.ChangeKind(changed.Kind), BaselineDirty: changed.BaselineDirty,
		})
	}
	return conversation
}

func storeChangedFiles(changes []model.ChangedFile) []store.ChangedFile {
	result := make([]store.ChangedFile, 0, len(changes))
	for _, change := range changes {
		result = append(result, store.ChangedFile{Path: change.Path, Kind: string(change.Kind), BaselineDirty: change.BaselineDirty})
	}
	return result
}

func hasRegistryInputs(changes []model.ChangedFile) bool {
	for _, change := range changes {
		path := filepath.ToSlash(change.Path)
		lower := strings.ToLower(path)
		contentComponent := strings.HasSuffix(lower, ".vue") &&
			(strings.Contains(path, "/components/content/") || strings.HasPrefix(path, "components/content/") || strings.HasPrefix(path, "app/components/content/"))
		registryArtifact := strings.Contains(lower, "content-builder/") &&
			(strings.Contains(lower, "component-definitions.") || strings.Contains(lower, "component-registry."))
		if contentComponent || registryArtifact {
			return true
		}
	}
	return false
}

func normalizeRegistryResult(result model.RegistryResult) model.RegistryResult {
	result.GeneratedComponentID = boundedUTF8(strings.TrimSpace(result.GeneratedComponentID), 256)
	if !safeComponentID(result.GeneratedComponentID) {
		result.GeneratedComponentID = ""
	}
	switch result.State {
	case model.RegistrySucceeded:
		result.Problem = ""
		return result
	case model.RegistryFailed:
		result.Problem = "Registry rebuild failed"
		result.GeneratedComponentID = ""
		return result
	default:
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry completion returned an invalid result"}
	}
}

func safeComponentID(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func storeBaseline(snapshot baseline.Snapshot) []store.BaselineEntry {
	result := make([]store.BaselineEntry, 0, len(snapshot.Files))
	for _, entry := range snapshot.Files {
		result = append(result, store.BaselineEntry{Path: entry.Path, Status: entry.Status, SHA256: entry.SHA256})
	}
	return result
}

func snapshotFromStore(entries []store.BaselineEntry) baseline.Snapshot {
	result := baseline.Snapshot{Files: make([]baseline.Entry, 0, len(entries))}
	for _, entry := range entries {
		result.Files = append(result.Files, baseline.Entry{Path: entry.Path, Status: entry.Status, SHA256: entry.SHA256})
	}
	return result
}

func cloneConversation(source model.Conversation) model.Conversation {
	clone := source
	if source.ActiveTurn != nil {
		turn := *source.ActiveTurn
		clone.ActiveTurn = &turn
	}
	if source.LastTurn != nil {
		last := *source.LastTurn
		clone.LastTurn = &last
	}
	if source.PendingInteraction != nil {
		interaction := cloneInteraction(*source.PendingInteraction)
		clone.PendingInteraction = &interaction
	}
	clone.Items = append([]model.ConversationItem(nil), source.Items...)
	clone.ChangedFiles = append([]model.ChangedFile(nil), source.ChangedFiles...)
	return clone
}

func cloneInteraction(source model.Interaction) model.Interaction {
	clone := source
	clone.AffectedPaths = append([]string(nil), source.AffectedPaths...)
	clone.Questions = append([]model.Question(nil), source.Questions...)
	for index := range clone.Questions {
		clone.Questions[index].Options = append([]model.InteractionOption(nil), source.Questions[index].Options...)
	}
	return clone
}

func cloneEvent(source model.Event) model.Event {
	clone := source
	if source.Item != nil {
		item := *source.Item
		clone.Item = &item
	}
	if source.Snapshot != nil {
		snapshot := cloneConversation(*source.Snapshot)
		clone.Snapshot = &snapshot
	}
	return clone
}

func eventTextSize(event model.Event) int {
	if event.Item == nil {
		return 0
	}
	return len(event.Item.Text)
}

func titleFromPrompt(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r", ""))
	if newline := strings.IndexByte(value, '\n'); newline >= 0 {
		value = value[:newline]
	}
	return boundedUTF8(value, 96)
}

func boundedUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func idempotencyKey(workspaceID, siteID, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return workspaceID + "\x00" + siteID + "\x00" + key
}

func opaqueID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	return hex.EncodeToString(value)
}
