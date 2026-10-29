package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

func TestRecoveryMarksInterruptedRevalidatesAndResumesThread(t *testing.T) {
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
	if !client.resumedThread("thread-recovered") {
		t.Fatal("thread/resume was not called")
	}
	moved, err := manager.Get(context.Background(), "conversation-moved")
	if err != nil {
		t.Fatal(err)
	}
	if !moved.ReadOnly || moved.BindingValid {
		t.Fatalf("moved binding = %#v", moved)
	}
	if client.resumedThread("thread-moved") {
		t.Fatal("thread/resume called for changed binding")
	}
}

func TestRecoveryIsolatesConversationWhoseCodexThreadCannotResume(t *testing.T) {
	repository := newFakeRepository()
	client := newFakeClient()
	binding := RecoveryBinding{WorkspaceID: "workspace-1", SiteID: "site-1", AppsRoot: "/workspace/apps", SiteRoot: "/workspace/apps/site-1"}
	fingerprint := baseline.Fingerprint(binding.WorkspaceID, binding.SiteID, binding.AppsRoot, binding.SiteRoot)
	repository.seed(store.Document{
		ID: "agent-conversation:conversation-failed", Type: store.DocumentType, SchemaVersion: store.CurrentSchemaVersion,
		ConversationID: "conversation-failed", CodexThreadID: "thread-without-rollout", WorkspaceID: binding.WorkspaceID, SiteID: binding.SiteID,
		SiteRootFingerprint: fingerprint, Title: "Failed", Status: store.Status("failed"),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	client.threadResumeErrors["thread-without-rollout"] = errors.New("no rollout found")
	manager := NewManager(Options{
		Repository: repository, Leases: mutationlease.NewManager(), Skill: testSkill(), Client: client.provider,
		Events: make(chan protocol.Notification, 8), Requests: make(chan protocol.ServerRequest, 8),
		Capture: fakeCapture, Fingerprint: baseline.Fingerprint,
	})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	if err := manager.Recover(context.Background(), []RecoveryBinding{binding}); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	recovered, err := manager.Get(context.Background(), "conversation-failed")
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.ReadOnly || !recovered.BindingValid || recovered.Status != model.ConversationFailed {
		t.Fatalf("recovered = %#v", recovered)
	}
}

func TestRegistryCompletionSkipsHookWithoutRegistryInputs(t *testing.T) {
	compareCalls := 0
	hook := &fakeCompletionHook{}
	manager, repository, _, events, _ := newTestManager(t, Options{
		Compare: func(context.Context, string, baseline.Snapshot) ([]model.ChangedFile, error) {
			compareCalls++
			return []model.ChangedFile{{Path: "README.md", Kind: model.ChangeModified}}, nil
		},
		Completion: hook,
	})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	document := repository.document(conversation.ID)
	completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "completed")
	ready := waitForStatus(t, manager, conversation.ID, model.ConversationReady)
	if compareCalls != 1 || hook.calls != 0 {
		t.Fatalf("compare calls = %d, hook calls = %d", compareCalls, hook.calls)
	}
	if ready.Registry.State != model.RegistryNotNeeded || len(ready.ChangedFiles) != 1 {
		t.Fatalf("ready = %#v", ready)
	}
}

func TestRegistryCompletionRunsWithClearedTurnAndHeldLease(t *testing.T) {
	leases := mutationlease.NewManager()
	var manager *Manager
	var conversationID string
	hook := &fakeCompletionHook{complete: func(_ context.Context, request CompletionRequest) model.RegistryResult {
		snapshot, err := manager.Get(context.Background(), conversationID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.ActiveTurn != nil || snapshot.PendingInteraction != nil || snapshot.Status != model.ConversationRebuildingRegistry {
			t.Fatalf("snapshot during hook = %#v", snapshot)
		}
		if release, err := leases.Acquire(request.WorkspaceID, "other", "job"); err == nil {
			release()
			t.Fatal("workspace lease was released before completion hook")
		}
		if request.SiteRoot != "/workspace/apps/site-1" || len(request.ChangedFiles) != 1 {
			t.Fatalf("completion request = %#v", request)
		}
		return model.RegistryResult{State: model.RegistrySucceeded, GeneratedComponentID: "testimonial-carousel"}
	}}
	manager, repository, _, events, _ := newTestManager(t, Options{
		Leases: leases,
		Compare: func(context.Context, string, baseline.Snapshot) ([]model.ChangedFile, error) {
			return []model.ChangedFile{{Path: "app/components/content/TestimonialCarousel.vue", Kind: model.ChangeAdded}}, nil
		},
		Completion: hook,
	})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	conversationID = conversation.ID
	document := repository.document(conversation.ID)
	subscription, err := manager.Subscribe(context.Background(), conversation.ID, conversation.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "completed")
	ready := waitForStatus(t, manager, conversation.ID, model.ConversationReady)
	if hook.calls != 1 || ready.Registry.State != model.RegistrySucceeded || ready.GeneratedComponentID != "testimonial-carousel" {
		t.Fatalf("ready = %#v, hook calls = %d", ready, hook.calls)
	}
	assertEventTypes(t, subscription, model.EventRegistryStarted, model.EventRegistryCompleted)
	if persisted := repository.document(conversation.ID); persisted.Status != store.StatusReady || persisted.Registry == nil || persisted.Registry.State != string(model.RegistrySucceeded) {
		t.Fatalf("persisted = %#v", persisted)
	}
	if release, err := leases.Acquire("workspace-1", "other", "job"); err != nil {
		t.Fatalf("workspace lease remains held after completion: %v", err)
	} else {
		release()
	}
}

func TestRegistryCompletionFailurePreservesReadyConversationAndFiles(t *testing.T) {
	hook := &fakeCompletionHook{complete: func(context.Context, CompletionRequest) model.RegistryResult {
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry rebuild failed"}
	}}
	manager, repository, _, events, _ := newTestManager(t, Options{
		Compare: func(context.Context, string, baseline.Snapshot) ([]model.ChangedFile, error) {
			return []model.ChangedFile{{Path: "app/components/content/Hero.vue", Kind: model.ChangeModified, BaselineDirty: true}}, nil
		},
		Completion: hook,
	})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	document := repository.document(conversation.ID)
	subscription, _ := manager.Subscribe(context.Background(), conversation.ID, conversation.Sequence)
	completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "completed")
	ready := waitForStatus(t, manager, conversation.ID, model.ConversationReady)
	if ready.LastTurn == nil || ready.LastTurn.Status != model.TurnCompleted || ready.Registry.State != model.RegistryFailed || len(ready.ChangedFiles) != 1 {
		t.Fatalf("ready = %#v", ready)
	}
	assertEventTypes(t, subscription, model.EventRegistryStarted, model.EventRegistryFailed)
}

func TestChangedFilesCompareFailureBecomesRegistryWarning(t *testing.T) {
	manager, repository, _, events, _ := newTestManager(t, Options{
		Compare: func(context.Context, string, baseline.Snapshot) ([]model.ChangedFile, error) {
			return nil, errors.New("compare failed")
		},
	})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	document := repository.document(conversation.ID)
	completeTurn(events, document.CodexThreadID, conversation.ActiveTurn.ID, "completed")
	ready := waitForStatus(t, manager, conversation.ID, model.ConversationReady)
	if ready.Registry.State != model.RegistryFailed || ready.LastTurn.Status != model.TurnCompleted {
		t.Fatalf("ready = %#v", ready)
	}
}

func TestNormalizeItemUsesSafeCommandActivityLabels(t *testing.T) {
	tests := []struct {
		name    string
		actions string
		want    string
	}{
		{name: "read", actions: `[{"type":"read","command":"cat /private/site/secret"}]`, want: "Reading project files"},
		{name: "list files", actions: `[{"type":"listFiles","path":"/private/site"}]`, want: "Listing project files"},
		{name: "search", actions: `[{"type":"search","query":"password"}]`, want: "Searching project files"},
		{name: "unknown", actions: `[{"type":"unknown","command":"do-not-expose"}]`, want: "Running a project command"},
		{name: "empty", actions: `[]`, want: "Running a project command"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(fmt.Sprintf(`{"id":"command","type":"commandExecution","status":"inProgress","command":"do-not-expose","cwd":"/private/site","aggregatedOutput":"do-not-expose-output","commandActions":%s}`, test.actions))
			item, ok := normalizeItem(raw, "turn-1")
			if !ok || item.Text != test.want {
				t.Fatalf("normalizeItem() = %#v, %v; want text %q", item, ok, test.want)
			}
			for _, forbidden := range []string{"do-not-expose", "/private", "password", "secret"} {
				if strings.Contains(item.Text, forbidden) {
					t.Fatalf("normalizeItem() exposed %q in %q", forbidden, item.Text)
				}
			}
		})
	}
}

func TestCompletedReasoningPreservesStreamedSummaryUnlessFinalSummaryExists(t *testing.T) {
	tests := []struct {
		name         string
		finalSummary []string
		want         string
	}{
		{name: "empty final summary preserves stream", want: "Inspecting component conventions."},
		{name: "final summary replaces stream", finalSummary: []string{"Component conventions inspected."}, want: "Component conventions inspected."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository, _, events, _ := newTestManager(t, Options{})
			conversation := createTestConversation(t, manager, "workspace-1", "site-1")
			document := repository.document(conversation.ID)

			sendReasoningItem(events, "item/started", document.CodexThreadID, conversation.ActiveTurn.ID, "reasoning-1", "inProgress", nil)
			sendReasoningDelta(events, document.CodexThreadID, conversation.ActiveTurn.ID, "reasoning-1", "Inspecting component conventions.")
			sendReasoningItem(events, "item/completed", document.CodexThreadID, conversation.ActiveTurn.ID, "reasoning-1", "completed", test.finalSummary)

			item := waitForConversationItem(t, manager, conversation.ID, "reasoning", "completed")
			if item.Text != test.want {
				t.Fatalf("reasoning text = %q, want %q", item.Text, test.want)
			}
		})
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
		Compare:     func(context.Context, string, baseline.Snapshot) ([]model.ChangedFile, error) { return nil, nil },
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
	if override.Leases != nil {
		options.Leases = override.Leases
	}
	if override.Compare != nil {
		options.Compare = override.Compare
	}
	if override.Completion != nil {
		options.Completion = override.Completion
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

func sendReasoningItem(events chan<- protocol.Notification, method, threadID, turnID, itemID, status string, summary []string) {
	item, _ := json.Marshal(map[string]any{"id": itemID, "type": "reasoning", "status": status, "summary": summary})
	params, _ := json.Marshal(protocol.ItemNotification{ThreadID: threadID, TurnID: turnID, Item: item})
	events <- protocol.Notification{Method: method, Params: params}
}

func sendReasoningDelta(events chan<- protocol.Notification, threadID, turnID, itemID, delta string) {
	params, _ := json.Marshal(map[string]any{
		"threadId": threadID, "turnId": turnID, "itemId": itemID, "summaryIndex": 0, "delta": delta,
	})
	events <- protocol.Notification{Method: "item/reasoning/summaryTextDelta", Params: params}
}

func waitForConversationItem(t *testing.T, manager *Manager, conversationID, itemType, status string) model.ConversationItem {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conversation, err := manager.Get(context.Background(), conversationID)
		if err == nil {
			for _, item := range conversation.Items {
				if item.Type == itemType && item.Status == status {
					return item
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s item with status %s", itemType, status)
	return model.ConversationItem{}
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

func assertEventTypes(t *testing.T, events <-chan model.Event, want ...model.EventType) {
	t.Helper()
	index := 0
	deadline := time.After(2 * time.Second)
	for index < len(want) {
		select {
		case event := <-events:
			if event.Type == want[index] {
				index++
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event types %#v from index %d", want, index)
		}
	}
}

type fakeCompletionHook struct {
	calls    int
	complete func(context.Context, CompletionRequest) model.RegistryResult
}

func (h *fakeCompletionHook) Complete(ctx context.Context, request CompletionRequest) model.RegistryResult {
	h.calls++
	if h.complete != nil {
		return h.complete(ctx, request)
	}
	return model.RegistryResult{State: model.RegistrySucceeded}
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
		turn.SandboxPolicy.Type != "workspaceWrite" || turn.SandboxPolicy.NetworkAccess || len(turn.SandboxPolicy.WritableRoots) != 1 || turn.SandboxPolicy.WritableRoots[0] != thread.CWD {
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
	mu                 sync.Mutex
	calls              []recordedCall
	responses          chan recordedResponse
	nextThread         int
	nextTurn           int
	threadReads        map[string]protocol.Thread
	threadResumeErrors map[string]error
}

type recordedResponse struct {
	id     json.RawMessage
	result any
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		threadReads: map[string]protocol.Thread{}, threadResumeErrors: map[string]error{}, responses: make(chan recordedResponse, 32),
	}
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
	case "thread/resume":
		request := params.(protocol.ThreadResumeParams)
		if err := c.threadResumeErrors[request.ThreadID]; err != nil {
			return err
		}
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
func (c *fakeClient) resumedThread(threadID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, call := range c.calls {
		if call.method == "thread/resume" && call.params.(protocol.ThreadResumeParams).ThreadID == threadID {
			return true
		}
	}
	return false
}
