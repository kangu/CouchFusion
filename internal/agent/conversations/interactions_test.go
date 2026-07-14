package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/protocol"
)

func TestApprovalPublicIDFirstResolutionAndOwnership(t *testing.T) {
	manager, _, client, _, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	sendCommandApproval(requests, `7001`, client.lastTurnThread(), conversation.ActiveTurn.ID, "bun run test")
	pending := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
	if pending.RequestID == "7001" || pending.RequestID == "" {
		t.Fatalf("public request ID = %q", pending.RequestID)
	}
	snapshot, _ := manager.Get(context.Background(), conversation.ID)
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "7001") {
		t.Fatalf("snapshot leaked raw RPC ID: %s", encoded)
	}
	if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalApproveOnce); err != nil {
		t.Fatal(err)
	}
	response := client.waitResponse(t)
	if string(response.id) != "7001" || responseDecision(response.result) != "accept" {
		t.Fatalf("response = %#v", response)
	}
	if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalDecline); !errors.Is(err, ErrInteractionResolved) {
		t.Fatalf("second resolution error = %v", err)
	}
	other := createTestConversation(t, manager, "workspace-2", "site-2")
	if err := manager.ResolveApproval(context.Background(), other.ID, pending.RequestID, ApprovalDecline); !errors.Is(err, ErrInteractionNotFound) {
		t.Fatalf("foreign resolution error = %v", err)
	}
}

func TestApprovalGenerationGrantIsNarrowAndTurnScoped(t *testing.T) {
	manager, repository, client, events, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	threadID := repository.document(conversation.ID).CodexThreadID

	sendCommandApproval(requests, `1`, threadID, conversation.ActiveTurn.ID, "bun run test:unit")
	pending := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
	if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalApproveGeneration); err != nil {
		t.Fatal(err)
	}
	assertDecision(t, client, "accept")
	sendCommandApproval(requests, `2`, threadID, conversation.ActiveTurn.ID, "bun run test:unit")
	assertDecision(t, client, "accept")
	sendCommandApproval(requests, `3`, threadID, conversation.ActiveTurn.ID, "bun run typecheck")
	second := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
	if err := manager.ResolveApproval(context.Background(), conversation.ID, second.RequestID, ApprovalDecline); err != nil {
		t.Fatal(err)
	}
	assertDecision(t, client, "decline")

	completeTurn(events, threadID, conversation.ActiveTurn.ID, "completed")
	waitForStatus(t, manager, conversation.ID, model.ConversationReady)
	followUp, err := manager.StartTurn(context.Background(), conversation.ID, TurnRequest{Prompt: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	sendCommandApproval(requests, `4`, threadID, followUp.ActiveTurn.ID, "bun run test:unit")
	waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
}

func TestApprovalGrantClearsOnInterruptAndRestart(t *testing.T) {
	for _, test := range []struct {
		name       string
		interrupt  bool
		restartGen bool
	}{
		{name: "interrupt", interrupt: true},
		{name: "runtime restart", restartGen: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var generation atomic.Uint64
			generation.Store(1)
			manager, repository, client, events, requests := newTestManager(t, Options{RuntimeGeneration: generation.Load})
			conversation := createTestConversation(t, manager, "workspace-1", "site-1")
			threadID := repository.document(conversation.ID).CodexThreadID
			sendCommandApproval(requests, `10`, threadID, conversation.ActiveTurn.ID, "bun run test")
			pending := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
			if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalApproveGeneration); err != nil {
				t.Fatal(err)
			}
			client.waitResponse(t)
			turnID := conversation.ActiveTurn.ID
			if test.interrupt {
				if err := manager.Interrupt(context.Background(), conversation.ID); err != nil {
					t.Fatal(err)
				}
				completeTurn(events, threadID, turnID, "interrupted")
				waitForStatus(t, manager, conversation.ID, model.ConversationReady)
				followUp, err := manager.StartTurn(context.Background(), conversation.ID, TurnRequest{Prompt: "retry"})
				if err != nil {
					t.Fatal(err)
				}
				turnID = followUp.ActiveTurn.ID
			} else {
				generation.Add(1)
			}
			sendCommandApproval(requests, `11`, threadID, turnID, "bun run test")
			waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
		})
	}
}

func TestApprovalGrantDoesNotSurviveRecovery(t *testing.T) {
	manager, repository, client, _, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	document := repository.document(conversation.ID)
	sendCommandApproval(requests, `25`, document.CodexThreadID, conversation.ActiveTurn.ID, "bun run test")
	pending := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
	if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalApproveGeneration); err != nil {
		t.Fatal(err)
	}
	client.waitResponse(t)
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	events := make(chan protocol.Notification, 16)
	recoveredRequests := make(chan protocol.ServerRequest, 16)
	recovered := NewManager(Options{
		Repository: repository, Leases: nil, Skill: testSkill(), Client: client.provider,
		Events: events, Requests: recoveredRequests, Capture: fakeCapture,
	})
	t.Cleanup(func() { _ = recovered.Shutdown(context.Background()) })
	binding := RecoveryBinding{WorkspaceID: "workspace-1", SiteID: "site-1", AppsRoot: "/workspace/apps", SiteRoot: "/workspace/apps/site-1"}
	client.threadReads[document.CodexThreadID] = protocol.Thread{ID: document.CodexThreadID}
	if err := recovered.Recover(context.Background(), []RecoveryBinding{binding}); err != nil {
		t.Fatal(err)
	}
	followUp, err := recovered.StartTurn(context.Background(), conversation.ID, TurnRequest{Prompt: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	sendCommandApproval(recoveredRequests, `26`, document.CodexThreadID, followUp.ActiveTurn.ID, "bun run test")
	waitForInteraction(t, recovered, conversation.ID, model.InteractionApproval)
}

func TestConsentGatesDependenciesPackageFilesAndNetwork(t *testing.T) {
	t.Run("dependency command", func(t *testing.T) {
		manager, repository, client, _, requests := newTestManager(t, Options{})
		conversation := createTestConversation(t, manager, "workspace-1", "site-1")
		threadID := repository.document(conversation.ID).CodexThreadID
		sendCommandApproval(requests, `30`, threadID, conversation.ActiveTurn.ID, "bun add lodash")
		pending := waitForInteraction(t, manager, conversation.ID, model.InteractionConsent)
		if pending.ConsentKind != model.ConsentDependencyChange {
			t.Fatalf("consent = %q", pending.ConsentKind)
		}
		if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalApproveGeneration); !errors.Is(err, ErrConsentRequired) {
			t.Fatalf("approval before consent error = %v", err)
		}
		if err := manager.GrantConsent(context.Background(), conversation.ID, pending.RequestID, model.ConsentDependencyChange, true); err != nil {
			t.Fatal(err)
		}
		waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
		if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalApproveGeneration); err != nil {
			t.Fatal(err)
		}
		assertDecision(t, client, "accept")
	})

	t.Run("package file", func(t *testing.T) {
		manager, repository, client, _, requests := newTestManager(t, Options{})
		conversation := createTestConversation(t, manager, "workspace-1", "site-1")
		threadID := repository.document(conversation.ID).CodexThreadID
		item := json.RawMessage(`{"id":"patch-package","type":"fileChange","status":"inProgress","changes":[{"path":"/workspace/apps/site-1/package.json","kind":"update"}]}`)
		params, _ := json.Marshal(protocol.ItemNotification{ThreadID: threadID, TurnID: conversation.ActiveTurn.ID, Item: item})
		manager.handleNotification(protocol.Notification{Method: "item/started", Params: params})
		sendFileApproval(requests, `31`, threadID, conversation.ActiveTurn.ID, "patch-package")
		waitForInteraction(t, manager, conversation.ID, model.InteractionConsent)
		if client.responseCount() != 0 {
			t.Fatal("package file was approved without consent")
		}
	})

	t.Run("network permissions", func(t *testing.T) {
		manager, repository, client, _, requests := newTestManager(t, Options{})
		conversation := createTestConversation(t, manager, "workspace-1", "site-1")
		threadID := repository.document(conversation.ID).CodexThreadID
		sendPermissionsApproval(requests, `32`, threadID, conversation.ActiveTurn.ID, `{"network":{"enabled":true}}`)
		pending := waitForInteraction(t, manager, conversation.ID, model.InteractionConsent)
		if pending.ConsentKind != model.ConsentNetworkAccess {
			t.Fatalf("consent = %q", pending.ConsentKind)
		}
		if err := manager.GrantConsent(context.Background(), conversation.ID, pending.RequestID, model.ConsentNetworkAccess, false); err != nil {
			t.Fatal(err)
		}
		if !permissionsDenied(client.waitResponse(t).result) {
			t.Fatal("network permission was not denied")
		}
	})
}

func TestApprovalClassifiesFileAndPermissions(t *testing.T) {
	tests := []struct {
		name   string
		action model.ApprovalAction
		send   func(*Manager, chan<- protocol.ServerRequest, string, string)
	}{
		{name: "file", action: model.ApprovalFileChange, send: func(manager *Manager, ch chan<- protocol.ServerRequest, thread, turn string) {
			item := json.RawMessage(`{"id":"patch","type":"fileChange","status":"inProgress","changes":[{"path":"/workspace/apps/site-1/app/components/content/Card.vue","kind":"update"}]}`)
			params, _ := json.Marshal(protocol.ItemNotification{ThreadID: thread, TurnID: turn, Item: item})
			manager.handleNotification(protocol.Notification{Method: "item/started", Params: params})
			sendFileApproval(ch, `40`, thread, turn, "patch")
		}},
		{name: "permissions", action: model.ApprovalPermissions, send: func(_ *Manager, ch chan<- protocol.ServerRequest, thread, turn string) {
			sendPermissionsApproval(ch, `41`, thread, turn, `{"fileSystem":{}}`)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, repository, client, _, requests := newTestManager(t, Options{})
			conversation := createTestConversation(t, manager, "workspace-1", "site-1")
			test.send(manager, requests, repository.document(conversation.ID).CodexThreadID, conversation.ActiveTurn.ID)
			pending := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
			if pending.Action != test.action {
				t.Fatalf("action = %q", pending.Action)
			}
			if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalDecline); err != nil {
				t.Fatal(err)
			}
			client.waitResponse(t)
		})
	}
}

func TestQuestionValidatesAndMapsDeclaredAnswers(t *testing.T) {
	manager, repository, client, _, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	params, _ := json.Marshal(protocol.UserInputRequestParams{
		ThreadID: repository.document(conversation.ID).CodexThreadID, TurnID: conversation.ActiveTurn.ID, ItemID: "questions",
		Questions: []protocol.UserInputQuestion{
			{ID: "raw-style", Header: "Style", Question: "Choose", Options: []protocol.UserInputOption{{Label: "Quiet"}, {Label: "Bold"}}},
			{ID: "raw-title", Header: "Title", Question: "Enter title", IsOther: true},
		},
	})
	requests <- protocol.ServerRequest{ID: json.RawMessage(`5001`), Method: "item/tool/requestUserInput", Params: params}
	pending := waitForInteraction(t, manager, conversation.ID, model.InteractionQuestion)
	if len(pending.Questions) != 2 || pending.Questions[0].ID == "raw-style" {
		t.Fatalf("questions = %#v", pending.Questions)
	}
	if err := manager.AnswerQuestion(context.Background(), conversation.ID, pending.RequestID, map[string]string{pending.Questions[0].ID: "Unknown"}); !errors.Is(err, ErrInvalidInteraction) {
		t.Fatalf("invalid option error = %v", err)
	}
	answers := map[string]string{pending.Questions[0].ID: "Bold", pending.Questions[1].ID: "Stories"}
	if err := manager.AnswerQuestion(context.Background(), conversation.ID, pending.RequestID, answers); err != nil {
		t.Fatal(err)
	}
	response := client.waitResponse(t)
	if string(response.id) != "5001" || !questionAnswer(response.result, "raw-style", "Bold") || !questionAnswer(response.result, "raw-title", "Stories") {
		t.Fatalf("response = %#v", response)
	}
}

func TestQuestionLimitsCountAndText(t *testing.T) {
	manager, repository, _, _, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	questions := []protocol.UserInputQuestion{}
	for _, id := range []string{"a", "b", "c", "d"} {
		questions = append(questions, protocol.UserInputQuestion{ID: id, Header: "Value", Question: "Enter", IsOther: true})
	}
	params, _ := json.Marshal(protocol.UserInputRequestParams{ThreadID: repository.document(conversation.ID).CodexThreadID, TurnID: conversation.ActiveTurn.ID, ItemID: "questions", Questions: questions})
	requests <- protocol.ServerRequest{ID: json.RawMessage(`6001`), Method: "item/tool/requestUserInput", Params: params}
	pending := waitForInteraction(t, manager, conversation.ID, model.InteractionQuestion)
	if len(pending.Questions) != 3 {
		t.Fatalf("questions = %d", len(pending.Questions))
	}
	if err := manager.AnswerQuestion(context.Background(), conversation.ID, pending.RequestID, map[string]string{pending.Questions[0].ID: strings.Repeat("x", 4097)}); !errors.Is(err, ErrInvalidInteraction) {
		t.Fatalf("oversized answer error = %v", err)
	}
}

func TestApprovalServerResolvedClearsPending(t *testing.T) {
	manager, repository, _, events, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	threadID := repository.document(conversation.ID).CodexThreadID
	sendCommandApproval(requests, `77`, threadID, conversation.ActiveTurn.ID, "bun run test")
	waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
	params, _ := json.Marshal(protocol.ServerRequestResolvedNotification{ThreadID: threadID, RequestID: json.RawMessage(`77`)})
	events <- protocol.Notification{Method: "serverRequest/resolved", Params: params}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := manager.Get(context.Background(), conversation.ID)
		if snapshot.PendingInteraction == nil && snapshot.Status == model.ConversationActive {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending interaction was not cleared")
}

func TestApprovalCancelRespondsAndInterruptsTurn(t *testing.T) {
	manager, repository, client, _, requests := newTestManager(t, Options{})
	conversation := createTestConversation(t, manager, "workspace-1", "site-1")
	threadID := repository.document(conversation.ID).CodexThreadID
	sendCommandApproval(requests, `88`, threadID, conversation.ActiveTurn.ID, "bun run test")
	pending := waitForInteraction(t, manager, conversation.ID, model.InteractionApproval)
	if err := manager.ResolveApproval(context.Background(), conversation.ID, pending.RequestID, ApprovalCancel); err != nil {
		t.Fatal(err)
	}
	assertDecision(t, client, "cancel")
	if !client.called("turn/interrupt") {
		t.Fatal("cancel did not request turn/interrupt")
	}
	waitForStatus(t, manager, conversation.ID, model.ConversationInterrupting)
}

func waitForInteraction(t *testing.T, manager *Manager, id string, kind model.InteractionKind) *model.Interaction {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Get(context.Background(), id)
		if err == nil && snapshot.PendingInteraction != nil && snapshot.PendingInteraction.Kind == kind {
			return snapshot.PendingInteraction
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q interaction", kind)
	return nil
}

func sendCommandApproval(ch chan<- protocol.ServerRequest, id, thread, turn, command string) {
	params, _ := json.Marshal(protocol.CommandApprovalParams{ThreadID: thread, TurnID: turn, ItemID: "command", Command: command})
	ch <- protocol.ServerRequest{ID: json.RawMessage(id), Method: "item/commandExecution/requestApproval", Params: params}
}

func sendFileApproval(ch chan<- protocol.ServerRequest, id, thread, turn, item string) {
	params, _ := json.Marshal(protocol.FileChangeApprovalParams{ThreadID: thread, TurnID: turn, ItemID: item})
	ch <- protocol.ServerRequest{ID: json.RawMessage(id), Method: "item/fileChange/requestApproval", Params: params}
}

func sendPermissionsApproval(ch chan<- protocol.ServerRequest, id, thread, turn, permissions string) {
	params, _ := json.Marshal(protocol.PermissionsApprovalParams{ThreadID: thread, TurnID: turn, ItemID: "permissions", Permissions: json.RawMessage(permissions)})
	ch <- protocol.ServerRequest{ID: json.RawMessage(id), Method: "item/permissions/requestApproval", Params: params}
}

func assertDecision(t *testing.T, client *fakeClient, want string) {
	t.Helper()
	if got := responseDecision(client.waitResponse(t).result); got != want {
		t.Fatalf("decision = %q, want %q", got, want)
	}
}

func responseDecision(result any) string {
	payload, _ := json.Marshal(result)
	var response struct {
		Decision string `json:"decision"`
	}
	_ = json.Unmarshal(payload, &response)
	return response.Decision
}

func permissionsDenied(result any) bool {
	payload, _ := json.Marshal(result)
	var response struct {
		Permissions map[string]any `json:"permissions"`
	}
	_ = json.Unmarshal(payload, &response)
	return len(response.Permissions) == 0
}

func questionAnswer(result any, rawID, want string) bool {
	payload, _ := json.Marshal(result)
	var response struct {
		Answers map[string]struct {
			Answers []string `json:"answers"`
		} `json:"answers"`
	}
	_ = json.Unmarshal(payload, &response)
	answers := response.Answers[rawID].Answers
	return len(answers) == 1 && answers[0] == want
}
