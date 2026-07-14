package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/baseline"
	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/prompt"
	"github.com/nuxt-apps/couchfusion/internal/agent/protocol"
	"github.com/nuxt-apps/couchfusion/internal/agent/store"
	"github.com/nuxt-apps/couchfusion/internal/mutationlease"
)

func TestLifecycleCompletedRebuildReady(t *testing.T) {
	manager, repository, client, events, _ := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	if conversation.Status != model.ConversationActive {
		t.Fatalf("Create() status = %q", conversation.Status)
	}
	document := repository.document(conversation.ID)
	if document.CodexThreadID == "" {
		t.Fatal("Codex thread ID was not persisted")
	}
	assertThreadPolicy(t, client)

	subscription, err := manager.Subscribe(context.Background(), conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "completed")
	waitForStatus(t, manager, conversation.ID, model.ConversationReady)

	statuses := collectStatuses(t, subscription, model.ConversationReady)
	assertStatusOrder(t, statuses,
		model.ConversationActive,
		model.ConversationCompleted,
		model.ConversationRebuildingRegistry,
		model.ConversationReady,
	)
}

func TestLifecycleWaitingReturnsActive(t *testing.T) {
	manager, repository, _, events, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	document := repository.document(conversation.ID)
	params, _ := json.Marshal(map[string]any{
		"threadId":  document.CodexThreadID,
		"turnId":    conversation.ActiveTurn.ID,
		"itemId":    "item-1",
		"questions": []map[string]any{{"id": "question-1", "header": "Style", "question": "Choose a style"}},
	})
	requests <- protocol.ServerRequest{ID: json.RawMessage(`91`), Method: "item/tool/requestUserInput", Params: params}
	waitForStatus(t, manager, conversation.ID, model.ConversationWaiting)

	resolved, _ := json.Marshal(protocol.ServerRequestResolvedNotification{ThreadID: document.CodexThreadID, RequestID: json.RawMessage(`91`)})
	events <- protocol.Notification{Method: "serverRequest/resolved", Params: resolved}
	waitForStatus(t, manager, conversation.ID, model.ConversationActive)
}

func TestLifecycleInterruptAndFailureReturnReady(t *testing.T) {
	t.Run("interrupt", func(t *testing.T) {
		manager, repository, client, events, _ := newTestManager(t, Options{})
		conversation := createTestConversation(t, manager, "workspace-1", "site-1")
		document := repository.document(conversation.ID)
		if err := manager.Interrupt(context.Background(), conversation.ID); err != nil {
			t.Fatalf("Interrupt() error = %v", err)
		}
		waitForStatus(t, manager, conversation.ID, model.ConversationInterrupting)
		if !client.called("turn/interrupt") {
			t.Fatal("turn/interrupt was not called")
		}
		completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "interrupted")
		ready := waitForStatus(t, manager, conversation.ID, model.ConversationReady)
		if ready.LastTurn == nil || ready.LastTurn.Status != model.TurnInterrupted {
			t.Fatalf("LastTurn = %#v", ready.LastTurn)
		}
	})

	t.Run("failure", func(t *testing.T) {
		manager, repository, _, events, _ := newTestManager(t, Options{})
		conversation := createTestConversation(t, manager, "workspace-2", "site-2")
		document := repository.document(conversation.ID)
		completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "failed")
		ready := waitForStatus(t, manager, conversation.ID, model.ConversationReady)
		if ready.LastTurn == nil || ready.LastTurn.Status != model.TurnFailed {
			t.Fatalf("LastTurn = %#v", ready.LastTurn)
		}
	})
}

func TestConcurrentLifecycleEnforcesTurnAndWorkspaceOwnership(t *testing.T) {
	manager, repository, client, events, _ := newTestManager(t, Options{})
	first := createTestConversation(t, manager, "workspace-1", "site-1")
	if _, err := manager.StartTurn(context.Background(), first.ID, TurnRequest{Prompt: "another turn"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("StartTurn(active) error = %v, want ErrConflict", err)
	}
	if _, err := manager.Create(context.Background(), testCreateRequest("workspace-1", "site-2")); err == nil {
		t.Fatal("Create() with occupied workspace succeeded")
	}

	firstThread := repository.document(first.ID).CodexThreadID
	completeTurn(events, firstThread, first.ActiveTurn.ID, "completed")
	waitForStatus(t, manager, first.ID, model.ConversationReady)
	followUp, err := manager.StartTurn(context.Background(), first.ID, TurnRequest{Prompt: "make it tighter"})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if got := client.lastTurnThread(); got != firstThread {
		t.Fatalf("follow-up thread = %q, want retained %q", got, firstThread)
	}
	completeTurn(events, firstThread, followUp.ActiveTurn.ID, "completed")
	waitForStatus(t, manager, first.ID, model.ConversationReady)

	second := createTestConversation(t, manager, "workspace-1", "site-2")
	secondThread := repository.document(second.ID).CodexThreadID
	if secondThread == firstThread {
		t.Fatalf("separate conversations reused thread %q", firstThread)
	}
}

func TestEventsReplaySequenceAndOverflowSnapshot(t *testing.T) {
	manager, repository, _, events, _ := newTestManager(t, Options{EventLimit: 3, EventTextLimit: 1024})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	document := repository.document(conversation.ID)
	for index := 0; index < 5; index++ {
		item, _ := json.Marshal(map[string]any{
			"id":     fmt.Sprintf("item-%d", index),
			"type":   "agentMessage",
			"text":   fmt.Sprintf("message-%d", index),
			"status": "completed",
		})
		params, _ := json.Marshal(protocol.ItemNotification{ThreadID: document.CodexThreadID, TurnID: conversation.ActiveTurn.ID, Item: item})
		events <- protocol.Notification{Method: "item/completed", Params: params}
	}
	waitForSequence(t, manager, conversation.ID, 6)

	overflow, err := manager.Subscribe(context.Background(), conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-overflow:
		if event.Type != model.EventConversationSnapshot || event.Snapshot == nil {
			t.Fatalf("overflow replay event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for overflow snapshot")
	}

	snapshot, err := manager.Get(context.Background(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot.Sequence - 2
	replay, err := manager.Subscribe(context.Background(), conversation.ID, after)
	if err != nil {
		t.Fatal(err)
	}
	previous := after
	for count := 0; count < 2; count++ {
		select {
		case event := <-replay:
			if event.Sequence <= previous {
				t.Fatalf("event sequence %d after %d", event.Sequence, previous)
			}
			previous = event.Sequence
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for replay")
		}
	}
}

func TestRecoveryMarksInterruptedRevalidatesAndReadsThread(t *testing.T) {
	repository := newFakeRepository()
	client := newFakeClient()
	events := make(chan protocol.Notification, 32)
	requests := make(chan protocol.ServerRequest, 32)
	binding := RecoveryBinding{WorkspaceID: "workspace-1", SiteID: "site-1", AppsRoot: "/workspace/apps", SiteRoot: "/workspace/apps/site-1"}
	fingerprint := baseline.Fingerprint(binding.WorkspaceID, binding.SiteID, binding.AppsRoot, binding.SiteRoot)
	repository.seed(store.Document{
		ID: "agent-conversation:conversation-1", Type: store.DocumentType, SchemaVersion: store.CurrentSchemaVersion,
		ConversationID: "conversation-1", CodexThreadID: "thread-recovered", WorkspaceID: binding.WorkspaceID, SiteID: binding.SiteID,
		SiteRootFingerprint: fingerprint, Title: "Recovered", Status: store.StatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	repository.seed(store.Document{
		ID: "agent-conversation:conversation-moved", Type: store.DocumentType, SchemaVersion: store.CurrentSchemaVersion,
		ConversationID: "conversation-moved", CodexThreadID: "thread-moved", WorkspaceID: binding.WorkspaceID, SiteID: binding.SiteID,
		SiteRootFingerprint: "old-binding", Title: "Moved", Status: store.StatusReady,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	client.threadReads["thread-recovered"] = protocol.Thread{ID: "thread-recovered", Turns: []protocol.Turn{{
		ID: "turn-old", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"id":"message-1","type":"agentMessage","text":"Done"}`)},
	}}}

	manager := NewManager(Options{
		Repository: repository, Leases: mutationlease.NewManager(), Skill: testSkill(), Client: client.provider,
		Events: events, Requests: requests, Capture: fakeCapture, Fingerprint: baseline.Fingerprint,
	})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	if err := manager.Recover(context.Background(), []RecoveryBinding{binding}); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	recovered, err := manager.Get(context.Background(), "conversation-1")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != model.ConversationInterrupted || recovered.PendingInteraction != nil {
		t.Fatalf("recovered = %#v", recovered)
	}
	if len(recovered.Items) != 1 || recovered.Items[0].Text != "Done" {
		t.Fatalf("recovered items = %#v", recovered.Items)
	}
	if !client.readThread("thread-recovered") {
		t.Fatal("thread/read was not called")
	}
	moved, err := manager.Get(context.Background(), "conversation-moved")
	if err != nil {
		t.Fatal(err)
	}
	if !moved.ReadOnly || moved.BindingValid {
		t.Fatalf("moved binding = %#v", moved)
	}
	if client.readThread("thread-moved") {
		t.Fatal("thread/read called for changed binding")
	}
}

func newTestManager(t *testing.T, override Options) (*Manager, *fakeRepository, *fakeClient, chan protocol.Notification, chan protocol.ServerRequest) {
	t.Helper()
	repository := newFakeRepository()
	client := newFakeClient()
	events := make(chan protocol.Notification, 64)
	requests := make(chan protocol.ServerRequest, 64)
	options := Options{
		Repository:  repository,
		Leases:      mutationlease.NewManager(),
		Skill:       testSkill(),
		Client:      client.provider,
		Events:      events,
		Requests:    requests,
		Capture:     fakeCapture,
		Fingerprint: baseline.Fingerprint,
		ID:          func() string { return fmt.Sprintf("conversation-%d", repository.count()+1) },
	}
	if override.EventLimit != 0 {
		options.EventLimit = override.EventLimit
	}
	if override.EventTextLimit != 0 {
		options.EventTextLimit = override.EventTextLimit
	}
	if override.RuntimeGeneration != nil {
		options.RuntimeGeneration = override.RuntimeGeneration
	}
	manager := NewManager(options)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	return manager, repository, client, events, requests
}

func createTestConversation(t *testing.T, manager *Manager, workspaceID, siteID string) model.Conversation {
	t.Helper()
	conversation, err := manager.Create(context.Background(), testCreateRequest(workspaceID, siteID))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return conversation
}

func testCreateRequest(workspaceID, siteID string) CreateRequest {
	return CreateRequest{
		WorkspaceID: workspaceID, SiteID: siteID, AppsRoot: "/workspace/apps", SiteRoot: "/workspace/apps/" + siteID,
		Prompt: "Create a compact testimonial component", IdempotencyKey: "key-" + siteID,
	}
}

func testSkill() prompt.Skill {
	return prompt.Skill{Name: prompt.SkillName, Root: "/managed/skills/digest", Path: "/managed/skills/digest/couchfusion-component-builder/SKILL.md", Digest: "digest"}
}

func fakeCapture(context.Context, string) (baseline.Snapshot, error) {
	return baseline.Snapshot{CapturedAt: time.Now().UTC(), Files: []baseline.Entry{{Path: "package.json", Status: "tracked", SHA256: "abc"}}}, nil
}

func completeTurn(events chan<- protocol.Notification, threadID, turnID, status string) {
	params, _ := json.Marshal(protocol.TurnNotification{ThreadID: threadID, Turn: protocol.Turn{ID: turnID, Status: status}})
	events <- protocol.Notification{Method: "turn/completed", Params: params}
}

func waitForStatus(t *testing.T, manager *Manager, conversationID string, want model.ConversationStatus) model.Conversation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conversation, err := manager.Get(context.Background(), conversationID)
		if err == nil && conversation.Status == want {
			return conversation
		}
		time.Sleep(time.Millisecond)
	}
	conversation, err := manager.Get(context.Background(), conversationID)
	t.Fatalf("status = %q, error = %v, want %q", conversation.Status, err, want)
	return model.Conversation{}
}

func waitForSequence(t *testing.T, manager *Manager, conversationID string, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conversation, err := manager.Get(context.Background(), conversationID)
		if err == nil && conversation.Sequence >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("sequence did not reach %d", want)
}

func collectStatuses(t *testing.T, events <-chan model.Event, until model.ConversationStatus) []model.ConversationStatus {
	t.Helper()
	var statuses []model.ConversationStatus
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Status != "" {
				statuses = append(statuses, event.Status)
			}
			if event.Status == until {
				return statuses
			}
		case <-deadline:
			t.Fatalf("timed out collecting statuses: %#v", statuses)
		}
	}
}

func assertStatusOrder(t *testing.T, got []model.ConversationStatus, want ...model.ConversationStatus) {
	t.Helper()
	index := 0
	for _, status := range got {
		if index < len(want) && status == want[index] {
			index++
		}
	}
	if index != len(want) {
		t.Fatalf("statuses = %#v, missing ordered %#v", got, want[index:])
	}
}

func assertThreadPolicy(t *testing.T, client *fakeClient) {
	t.Helper()
	thread := client.threadStart()
	if thread.CWD != "/workspace/apps/site-1" || thread.ApprovalPolicy != "on-request" || thread.ApprovalsReviewer != "user" || thread.Sandbox != "workspace-write" {
		t.Fatalf("thread/start policy = %#v", thread)
	}
	if thread.DeveloperInstructions != prompt.DeveloperInstructions() {
		t.Fatal("thread/start omitted fixed developer instructions")
	}
	turn := client.lastTurnStart()
	if turn.CWD != thread.CWD || turn.ApprovalPolicy != "on-request" || turn.ApprovalsReviewer != "user" || turn.SandboxPolicy == nil ||
		turn.SandboxPolicy.Type != "workspace-write" || turn.SandboxPolicy.NetworkAccess || len(turn.SandboxPolicy.WritableRoots) != 1 || turn.SandboxPolicy.WritableRoots[0] != thread.CWD {
		t.Fatalf("turn/start policy = %#v", turn)
	}
	if len(turn.Input) != 3 || turn.Input[1].Type != "skill" || turn.Input[2].Text != "Create a compact testimonial component" {
		t.Fatalf("turn/start input = %#v", turn.Input)
	}
}

type fakeRepository struct {
	mu        sync.Mutex
	documents map[string]store.Document
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{documents: map[string]store.Document{}}
}
func (r *fakeRepository) Ensure(context.Context) error { return nil }
func (r *fakeRepository) Create(_ context.Context, document store.Document) (store.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.documents[document.ConversationID]; exists {
		return store.Document{}, store.ErrConflict
	}
	document.Revision = "1-created"
	r.documents[document.ConversationID] = document
	return document, nil
}
func (r *fakeRepository) Get(_ context.Context, id string) (store.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	document, ok := r.documents[id]
	if !ok {
		return store.Document{}, store.ErrNotFound
	}
	return document, nil
}
func (r *fakeRepository) ListSite(_ context.Context, workspaceID, siteID string) ([]store.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []store.Document
	for _, document := range r.documents {
		if document.WorkspaceID == workspaceID && document.SiteID == siteID {
			result = append(result, document)
		}
	}
	return result, nil
}
func (r *fakeRepository) Update(_ context.Context, document store.Document) (store.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.documents[document.ConversationID]; !ok {
		return store.Document{}, store.ErrNotFound
	}
	document.Revision = document.Revision + "-next"
	r.documents[document.ConversationID] = document
	return document, nil
}
func (r *fakeRepository) MarkActiveInterrupted(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, document := range r.documents {
		if document.Status == store.StatusActive || document.Status == store.StatusWaiting {
			document.Status = store.StatusInterrupted
			r.documents[id] = document
		}
	}
	return nil
}
func (r *fakeRepository) seed(document store.Document) {
	r.mu.Lock()
	defer r.mu.Unlock()
	document.Revision = "1-seeded"
	r.documents[document.ConversationID] = document
}
func (r *fakeRepository) document(id string) store.Document {
	document, _ := r.Get(context.Background(), id)
	return document
}
func (r *fakeRepository) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.documents)
}

type recordedCall struct {
	method string
	params any
}

type fakeClient struct {
	mu          sync.Mutex
	calls       []recordedCall
	responses   chan recordedResponse
	nextThread  int
	nextTurn    int
	threadReads map[string]protocol.Thread
}

type recordedResponse struct {
	id     json.RawMessage
	result any
}

func newFakeClient() *fakeClient {
	return &fakeClient{threadReads: map[string]protocol.Thread{}, responses: make(chan recordedResponse, 32)}
}
func (c *fakeClient) provider(context.Context) (RPCClient, error) { return c, nil }
func (c *fakeClient) Call(_ context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, recordedCall{method: method, params: params})
	switch method {
	case "thread/start":
		c.nextThread++
		response := result.(*protocol.ThreadResponse)
		response.Thread.ID = fmt.Sprintf("thread-%d", c.nextThread)
	case "turn/start":
		c.nextTurn++
		response := result.(*protocol.TurnStartResponse)
		response.Turn = protocol.Turn{ID: fmt.Sprintf("turn-%d", c.nextTurn), Status: "inProgress"}
	case "thread/read":
		request := params.(protocol.ThreadReadParams)
		response := result.(*protocol.ThreadResponse)
		response.Thread = c.threadReads[request.ThreadID]
	}
	return nil
}
func (c *fakeClient) Respond(_ context.Context, id json.RawMessage, result any, _ *protocol.RPCError) error {
	c.responses <- recordedResponse{id: append(json.RawMessage(nil), id...), result: result}
	return nil
}
func (c *fakeClient) waitResponse(t *testing.T) recordedResponse {
	t.Helper()
	select {
	case response := <-c.responses:
		return response
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for protocol response")
		return recordedResponse{}
	}
}
func (c *fakeClient) responseCount() int { return len(c.responses) }
func (c *fakeClient) called(method string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if call.method == method {
			return true
		}
	}
	return false
}
func (c *fakeClient) threadStart() protocol.ThreadStartParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if call.method == "thread/start" {
			return call.params.(protocol.ThreadStartParams)
		}
	}
	return protocol.ThreadStartParams{}
}
func (c *fakeClient) lastTurnStart() protocol.TurnStartParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index := len(c.calls) - 1; index >= 0; index-- {
		if c.calls[index].method == "turn/start" {
			return c.calls[index].params.(protocol.TurnStartParams)
		}
	}
	return protocol.TurnStartParams{}
}
func (c *fakeClient) lastTurnThread() string { return c.lastTurnStart().ThreadID }
func (c *fakeClient) readThread(threadID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if call.method == "thread/read" && call.params.(protocol.ThreadReadParams).ThreadID == threadID {
			return true
		}
	}
	return false
}
