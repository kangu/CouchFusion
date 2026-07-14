package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/conversations"
	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/prompt"
	"github.com/nuxt-apps/couchfusion/internal/app"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/preview"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
)

func TestWorkspaceRoutesUseApplicationService(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		call   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/v1/workspaces", status: http.StatusOK, call: "list"},
		{name: "register", method: http.MethodPost, path: "/api/v1/workspaces/register", body: `{"root":"/workspace","name":"Demo"}`, status: http.StatusCreated, call: "register"},
		{name: "initialize", method: http.MethodPost, path: "/api/v1/workspaces/initialize", body: `{"root":"/new","name":"New"}`, status: http.StatusAccepted, call: "initialize"},
		{name: "get", method: http.MethodGet, path: "/api/v1/workspaces/w1", status: http.StatusOK, call: "workspace"},
		{name: "rename", method: http.MethodPatch, path: "/api/v1/workspaces/w1", body: `{"name":"Storefront"}`, status: http.StatusOK, call: "rename"},
		{name: "forget", method: http.MethodDelete, path: "/api/v1/workspaces/w1", body: `{}`, status: http.StatusNoContent, call: "forget"},
		{name: "relink", method: http.MethodPost, path: "/api/v1/workspaces/w1/relink", body: `{"root":"/moved"}`, status: http.StatusOK, call: "relink"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := authenticatedAPIRequest(server, cookie, test.method, test.path, test.body)
			if recorder.Code != test.status {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
			if application.calls[test.call] == 0 {
				t.Fatalf("application call %q was not made", test.call)
			}
		})
	}
}

func TestConfigHealthAndProvisioningRoutes(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		call   string
	}{
		{method: http.MethodGet, path: "/api/v1/workspaces/w1/config", status: http.StatusOK, call: "config.get"},
		{method: http.MethodPut, path: "/api/v1/workspaces/w1/config", body: `{"workspace":{"defaultRoot":"/sites"}}`, status: http.StatusOK, call: "config.put"},
		{method: http.MethodGet, path: "/api/v1/workspaces/w1/health", status: http.StatusOK, call: "health"},
		{method: http.MethodPost, path: "/api/v1/system/dependencies/couchdb/install", body: `{}`, status: http.StatusAccepted, call: "dependency.install"},
		{method: http.MethodPost, path: "/api/v1/workspaces/w1/sites", body: `{"name":"shop","modules":["auth"],"dbLoginPrefix":"ac"}`, status: http.StatusAccepted, call: "site.create"},
		{method: http.MethodPost, path: "/api/v1/workspaces/w1/layers", body: `{"name":"content"}`, status: http.StatusAccepted, call: "layer.create"},
	}
	for _, test := range tests {
		recorder := authenticatedAPIRequest(server, cookie, test.method, test.path, test.body)
		if recorder.Code != test.status {
			t.Fatalf("%s %s status = %d: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
		if application.calls[test.call] == 0 {
			t.Fatalf("application call %q was not made", test.call)
		}
	}
	if application.siteRequest.DBLoginPrefix != "ac" {
		t.Fatalf("database login prefix = %q, want ac", application.siteRequest.DBLoginPrefix)
	}
}

func TestTaskAndProcessRoutes(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		call   string
	}{
		{method: http.MethodGet, path: "/api/v1/workspaces/w1/sites/shop", status: http.StatusOK, call: "site.get"},
		{method: http.MethodGet, path: "/api/v1/workspaces/w1/sites/shop/tasks", status: http.StatusOK, call: "tasks"},
		{method: http.MethodPost, path: "/api/v1/workspaces/w1/sites/shop/tasks/script:build/run", body: `{}`, status: http.StatusAccepted, call: "task.run"},
		{method: http.MethodPost, path: "/api/v1/workspaces/w1/sites/shop/processes/script:dev/start", body: `{}`, status: http.StatusAccepted, call: "process.start"},
		{method: http.MethodPost, path: "/api/v1/processes/process-1/stop", body: `{}`, status: http.StatusAccepted, call: "process.stop"},
		{method: http.MethodPost, path: "/api/v1/processes/process-1/restart", body: `{}`, status: http.StatusAccepted, call: "process.restart"},
	}
	for _, test := range tests {
		recorder := authenticatedAPIRequest(server, cookie, test.method, test.path, test.body)
		if recorder.Code != test.status {
			t.Fatalf("%s %s status = %d: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
		if application.calls[test.call] == 0 {
			t.Fatalf("application call %q was not made", test.call)
		}
	}
}

func TestComponentRoutesUseSafeSiteScopedOperations(t *testing.T) {
	application := newAPIApplicationStub()
	application.components = app.ComponentRegistryView{
		ComponentRegistry: project.ComponentRegistry{
			Status: project.ComponentRegistryReady,
			Components: []project.ComponentDefinition{{
				ID: "hero", Label: "Hero", Description: "Lead block",
				Source: "app/components/content/Hero.vue", PropCount: 2, AllowChildren: true,
			}},
		},
		CanRebuild: true,
	}
	application.job.Type = "component-registry.rebuild"
	server, cookie := newAuthenticatedAPIServer(t, application)

	list := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/workspaces/w1/sites/shop/components", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"status":"ready"`) || !strings.Contains(list.Body.String(), `"source":"app/components/content/Hero.vue"`) {
		t.Fatalf("list response = %d: %s", list.Code, list.Body.String())
	}
	rebuild := authenticatedAPIRequest(server, cookie, http.MethodPost, "/api/v1/workspaces/w1/sites/shop/components/registry/rebuild", `{}`)
	if rebuild.Code != http.StatusAccepted || !strings.Contains(rebuild.Body.String(), `"type":"component-registry.rebuild"`) {
		t.Fatalf("rebuild response = %d: %s", rebuild.Code, rebuild.Body.String())
	}
	open := authenticatedAPIRequest(server, cookie, http.MethodPost, "/api/v1/workspaces/w1/sites/shop/components/hero/open", `{}`)
	if open.Code != http.StatusNoContent || open.Body.Len() != 0 || application.openedComponentID != "hero" {
		t.Fatalf("open response = %d: %s; component = %q", open.Code, open.Body.String(), application.openedComponentID)
	}
	for _, call := range []string{"components", "components.rebuild", "components.open"} {
		if application.calls[call] != 1 {
			t.Fatalf("application call %q count = %d", call, application.calls[call])
		}
	}
}

func TestComponentRoutesValidateIDsAndMapOperationErrors(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)

	invalidID := authenticatedAPIRequest(server, cookie, http.MethodPost, "/api/v1/workspaces/w1/sites/shop/components/bad%20id/open", `{}`)
	if invalidID.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid ID response = %d: %s", invalidID.Code, invalidID.Body.String())
	}

	tests := []struct {
		err    error
		status int
	}{
		{err: &app.Error{Code: app.ErrorNotFound, Message: "missing"}, status: http.StatusNotFound},
		{err: &app.Error{Code: app.ErrorValidation, Message: "invalid"}, status: http.StatusUnprocessableEntity},
		{err: &app.Error{Code: app.ErrorProcess, Message: "unavailable"}, status: http.StatusConflict},
	}
	for _, test := range tests {
		application.errors["components.open"] = test.err
		response := authenticatedAPIRequest(server, cookie, http.MethodPost, "/api/v1/workspaces/w1/sites/shop/components/hero/open", `{}`)
		if response.Code != test.status {
			t.Fatalf("error %v response = %d: %s", test.err, response.Code, response.Body.String())
		}
	}
}

func TestComponentPreviewHostRoutes(t *testing.T) {
	application := newAPIApplicationStub()
	application.previewHost = preview.View{
		Status: preview.Ready, ProcessID: "preview-1",
		URL: "http://127.0.0.1:44001/__couchfusion/components/preview?token=capability",
	}
	server, cookie := newAuthenticatedAPIServer(t, application)
	base := "/api/v1/workspaces/w1/sites/shop/components/preview-host"

	status := authenticatedAPIRequest(server, cookie, http.MethodGet, base, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"ready"`) {
		t.Fatalf("status response = %d: %s", status.Code, status.Body.String())
	}
	start := authenticatedAPIRequest(server, cookie, http.MethodPost, base+"/start", `{}`)
	stop := authenticatedAPIRequest(server, cookie, http.MethodPost, base+"/stop", `{}`)
	if start.Code != http.StatusOK || stop.Code != http.StatusOK {
		t.Fatalf("start/stop responses = %d/%d: %s / %s", start.Code, stop.Code, start.Body.String(), stop.Body.String())
	}
	if application.calls["components.preview.status"] != 1 || application.calls["components.preview.start"] != 1 || application.calls["components.preview.stop"] != 1 {
		t.Fatalf("calls = %#v", application.calls)
	}
	for _, body := range []string{status.Body.String(), start.Body.String(), stop.Body.String()} {
		if strings.Contains(body, `"token":`) {
			t.Fatalf("response exposes standalone token: %s", body)
		}
	}
}

func TestAgentConversationRoutesUseApplicationService(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)

	tests := []struct {
		method string
		path   string
		body   string
		header map[string]string
		status int
		call   string
	}{
		{method: http.MethodGet, path: "/api/v1/workspaces/w1/sites/shop/agent-conversations", status: http.StatusOK, call: "agent.list"},
		{method: http.MethodPost, path: "/api/v1/workspaces/w1/sites/shop/agent-conversations", body: `{"prompt":"Build a hero"}`, header: map[string]string{"Idempotency-Key": "create-1"}, status: http.StatusCreated, call: "agent.create"},
		{method: http.MethodGet, path: "/api/v1/agent-conversations/c1", status: http.StatusOK, call: "agent.get"},
		{method: http.MethodPost, path: "/api/v1/agent-conversations/c1/turns", body: `{"prompt":"Make it compact"}`, header: map[string]string{"Idempotency-Key": "turn-1"}, status: http.StatusAccepted, call: "agent.turn"},
		{method: http.MethodPost, path: "/api/v1/agent-conversations/c1/approvals/r1", body: `{"decision":"approve_generation"}`, status: http.StatusNoContent, call: "agent.approval"},
		{method: http.MethodPost, path: "/api/v1/agent-conversations/c1/questions/r1/answer", body: `{"answers":{"tone":"quiet"}}`, status: http.StatusNoContent, call: "agent.question"},
		{method: http.MethodPost, path: "/api/v1/agent-conversations/c1/consents", body: `{"kind":"network_access","granted":true}`, status: http.StatusNoContent, call: "agent.consent"},
		{method: http.MethodPost, path: "/api/v1/agent-conversations/c1/interrupt", body: `{}`, status: http.StatusNoContent, call: "agent.interrupt"},
	}
	for _, test := range tests {
		response := authenticatedAPIRequestWithHeaders(server, cookie, test.method, test.path, test.body, test.header)
		if response.Code != test.status {
			t.Fatalf("%s %s status = %d: %s", test.method, test.path, response.Code, response.Body.String())
		}
		if application.calls[test.call] == 0 {
			t.Fatalf("application call %q was not made", test.call)
		}
	}
	if application.agentPrompt.Prompt != "Make it compact" || application.agentPrompt.IdempotencyKey != "turn-1" {
		t.Fatalf("turn request = %#v", application.agentPrompt)
	}
	if application.approvalDecision != conversations.ApprovalApproveGeneration {
		t.Fatalf("approval decision = %q", application.approvalDecision)
	}
	if application.consentRequestID != "r1" || application.consentKind != model.ConsentNetworkAccess || !application.consentGranted {
		t.Fatalf("consent = %q %q %t", application.consentRequestID, application.consentKind, application.consentGranted)
	}
}

func TestAgentConversationRoutesValidateStrictInputs(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	tests := []struct {
		name   string
		path   string
		body   string
		header map[string]string
	}{
		{name: "unknown field", path: "/api/v1/workspaces/w1/sites/shop/agent-conversations", body: `{"prompt":"hello","cwd":"/tmp"}`, header: map[string]string{"Idempotency-Key": "key"}},
		{name: "blank prompt", path: "/api/v1/workspaces/w1/sites/shop/agent-conversations", body: `{"prompt":"  "}`, header: map[string]string{"Idempotency-Key": "key"}},
		{name: "long prompt", path: "/api/v1/workspaces/w1/sites/shop/agent-conversations", body: `{"prompt":"` + strings.Repeat("x", prompt.MaxPromptBytes+1) + `"}`, header: map[string]string{"Idempotency-Key": "key"}},
		{name: "missing idempotency", path: "/api/v1/workspaces/w1/sites/shop/agent-conversations", body: `{"prompt":"hello"}`},
		{name: "long idempotency", path: "/api/v1/agent-conversations/c1/turns", body: `{"prompt":"hello"}`, header: map[string]string{"Idempotency-Key": strings.Repeat("k", 129)}},
		{name: "invalid decision", path: "/api/v1/agent-conversations/c1/approvals/r1", body: `{"decision":"always"}`},
		{name: "invalid consent", path: "/api/v1/agent-conversations/c1/consents", body: `{"kind":"filesystem","granted":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := authenticatedAPIRequestWithHeaders(server, cookie, http.MethodPost, test.path, test.body, test.header)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAgentConversationIDsAreSafelyPathDecoded(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	response := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/agent-conversations/bad%20id", "")
	if response.Code != http.StatusUnprocessableEntity || application.calls["agent.get"] != 0 {
		t.Fatalf("response = %d: %s; calls = %d", response.Code, response.Body.String(), application.calls["agent.get"])
	}
}

func TestAgentConversationSSEHonorsLastEventID(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	response := authenticatedAPIRequestWithHeaders(server, cookie, http.MethodGet, "/api/v1/agent-conversations/c1/events", "", map[string]string{"Last-Event-ID": "6"})
	if response.Code != http.StatusOK || application.lastEventID != 6 {
		t.Fatalf("response = %d: %s; after = %d", response.Code, response.Body.String(), application.lastEventID)
	}
	body := response.Body.String()
	if !strings.Contains(body, "id: 7\nevent: conversation\n") || !strings.Contains(body, `"conversationId":"c1"`) {
		t.Fatalf("SSE body = %s", body)
	}
}

func TestAgentConversationResponsesDoNotExposeControlPlaneSecrets(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	response := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/agent-conversations/c1", "")
	for _, forbidden := range []string{"codexThreadId", "cwd", "skillPath", "rpcId", "authorization", "credentials", "Build a secret component"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("response exposes %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestJobProcessAndSSERoutes(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)

	for _, test := range []struct {
		method string
		path   string
		body   string
		status int
		call   string
	}{
		{method: http.MethodGet, path: "/api/v1/jobs/job-1", status: http.StatusOK, call: "job.get"},
		{method: http.MethodPost, path: "/api/v1/jobs/job-1/cancel", body: `{}`, status: http.StatusAccepted, call: "job.cancel"},
		{method: http.MethodPost, path: "/api/v1/jobs/job-1/secret/consume", body: `{}`, status: http.StatusOK, call: "job.secret"},
		{method: http.MethodGet, path: "/api/v1/processes/process-1", status: http.StatusOK, call: "process.get"},
	} {
		recorder := authenticatedAPIRequest(server, cookie, test.method, test.path, test.body)
		if recorder.Code != test.status {
			t.Fatalf("%s %s status = %d: %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
		if application.calls[test.call] == 0 {
			t.Fatalf("application call %q was not made", test.call)
		}
	}

	jobEvents := authenticatedAPIRequestWithHeaders(server, cookie, http.MethodGet, "/api/v1/jobs/job-1/events", "", map[string]string{"Last-Event-ID": "4"})
	if jobEvents.Code != http.StatusOK || !strings.Contains(jobEvents.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(jobEvents.Body.String(), "id: 5\nevent: job\ndata:") {
		t.Fatalf("job SSE = %d %q: %s", jobEvents.Code, jobEvents.Header().Get("Content-Type"), jobEvents.Body.String())
	}
	if application.lastEventID != 4 {
		t.Fatalf("job Last-Event-ID = %d", application.lastEventID)
	}
	processEvents := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/processes/process-1/events", "")
	if processEvents.Code != http.StatusOK || !strings.Contains(processEvents.Body.String(), "event: process") {
		t.Fatalf("process SSE = %d: %s", processEvents.Code, processEvents.Body.String())
	}
}

func TestAPIRejectsUnknownFieldsAndMapsStableErrors(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	unknown := authenticatedAPIRequest(server, cookie, http.MethodPost, "/api/v1/workspaces/register", `{"root":"/workspace","surprise":true}`)
	if unknown.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status = %d: %s", unknown.Code, unknown.Body.String())
	}

	for _, test := range []struct {
		code   app.ErrorCode
		status int
	}{
		{code: app.ErrorValidation, status: http.StatusUnprocessableEntity},
		{code: app.ErrorConflict, status: http.StatusConflict},
		{code: app.ErrorNotFound, status: http.StatusNotFound},
		{code: app.ErrorUnavailable, status: http.StatusServiceUnavailable},
		{code: app.ErrorInternal, status: http.StatusInternalServerError},
	} {
		application.errors["workspace"] = &app.Error{Code: test.code, Message: "safe error"}
		recorder := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/workspaces/w1", "")
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), string(test.code)) {
			t.Fatalf("code %s => %d: %s", test.code, recorder.Code, recorder.Body.String())
		}
	}
}

func TestWorkspaceDTOContainsNoCredentials(t *testing.T) {
	application := newAPIApplicationStub()
	server, cookie := newAuthenticatedAPIServer(t, application)
	recorder := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/workspaces/w1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"adminauth", "password", "cookie_secret", "authorization", "generated-password"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response contains %q: %s", forbidden, body)
		}
	}
}

func TestSecretConsumptionRejectsForeignSessionAndUnknownRoutesStayJSON(t *testing.T) {
	application := newAPIApplicationStub()
	application.errors["job.secret"] = jobs.ErrSecretForbidden
	server, cookie := newAuthenticatedAPIServer(t, application)

	forbidden := authenticatedAPIRequest(server, cookie, http.MethodPost, "/api/v1/jobs/job-1/secret/consume", `{}`)
	if forbidden.Code != http.StatusForbidden || !strings.Contains(forbidden.Body.String(), "unauthorized") {
		t.Fatalf("foreign secret response = %d: %s", forbidden.Code, forbidden.Body.String())
	}
	unknown := authenticatedAPIRequest(server, cookie, http.MethodGet, "/api/v1/unknown", "")
	if unknown.Code != http.StatusNotFound || !strings.HasPrefix(unknown.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unknown response = %d %q: %s", unknown.Code, unknown.Header().Get("Content-Type"), unknown.Body.String())
	}
}

func newAuthenticatedAPIServer(t *testing.T, application Application) (*Server, *http.Cookie) {
	t.Helper()
	server, err := NewServer(Options{Application: application})
	if err != nil {
		t.Fatal(err)
	}
	return server, authenticatedCookie(t, server)
}

func authenticatedAPIRequest(server *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	return authenticatedAPIRequestWithHeaders(server, cookie, method, path, body, nil)
}

func authenticatedAPIRequestWithHeaders(server *Server, cookie *http.Cookie, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	request.AddCookie(cookie)
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://127.0.0.1")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	return recorder
}

type apiApplicationStub struct {
	calls             map[string]int
	errors            map[string]error
	lastEventID       uint64
	entry             registry.Entry
	workspace         project.Workspace
	site              project.Site
	siteRequest       app.CreateSiteRequest
	job               jobs.Snapshot
	process           processes.Snapshot
	components        app.ComponentRegistryView
	previewHost       preview.View
	openedComponentID string
	agentCollection   app.AgentConversationCollection
	agentConversation model.Conversation
	agentPrompt       app.AgentPromptRequest
	approvalDecision  conversations.ApprovalDecision
	consentRequestID  string
	consentKind       model.ConsentKind
	consentGranted    bool
}

func newAPIApplicationStub() *apiApplicationStub {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return &apiApplicationStub{
		calls:             map[string]int{},
		errors:            map[string]error{},
		entry:             registry.Entry{ID: "w1", Name: "Demo", Root: "/workspace", Available: true},
		workspace:         project.Workspace{ID: "w1", Name: "Demo", Root: "/workspace", Available: true, Sites: []project.Site{}, Layers: []project.Layer{}, Warnings: []project.Warning{}},
		site:              project.Site{ID: "shop", Name: "Shop", Root: "/workspace/apps/shop", Modules: []string{"auth"}, Scripts: map[string]string{"dev": "nuxt dev"}, Dependencies: map[string]string{}, DevDependencies: map[string]string{}, Lockfiles: []string{}, Warnings: []project.Warning{}},
		job:               jobs.Snapshot{ID: "job-1", Type: "site.create", WorkspaceID: "w1", Status: jobs.Running, CreatedAt: now, Events: []jobs.Event{}},
		process:           processes.Snapshot{ID: "process-1", WorkspaceID: "w1", SiteID: "shop", TaskID: "script:dev", Status: processes.Running, StartedAt: now, URLs: []string{"http://localhost:3000/"}, Events: []processes.Event{}},
		agentCollection:   app.AgentConversationCollection{Readiness: model.AgentReadiness{Status: model.AgentReady, Available: true}},
		agentConversation: model.Conversation{ID: "c1", WorkspaceID: "w1", SiteID: "shop", Title: "Hero", Status: model.ConversationWaiting, PendingInteraction: &model.Interaction{RequestID: "r1", Kind: model.InteractionConsent, ConsentKind: model.ConsentNetworkAccess}, Registry: model.RegistryResult{State: model.RegistryNotNeeded}, Sequence: 6, BindingValid: true, CreatedAt: now, UpdatedAt: now},
	}
}

func (s *apiApplicationStub) ListWorkspaces(context.Context) ([]registry.Entry, error) {
	s.calls["list"]++
	return []registry.Entry{s.entry}, s.errors["list"]
}
func (s *apiApplicationStub) RegisterWorkspace(context.Context, string, string) (registry.Entry, error) {
	s.calls["register"]++
	return s.entry, s.errors["register"]
}
func (s *apiApplicationStub) InitializeWorkspace(context.Context, app.InitializeWorkspaceRequest, string) (jobs.Snapshot, error) {
	s.calls["initialize"]++
	return s.job, s.errors["initialize"]
}
func (s *apiApplicationStub) Workspace(context.Context, string) (project.Workspace, error) {
	s.calls["workspace"]++
	return s.workspace, s.errors["workspace"]
}
func (s *apiApplicationStub) RenameWorkspace(context.Context, string, string) (registry.Entry, error) {
	s.calls["rename"]++
	return s.entry, s.errors["rename"]
}
func (s *apiApplicationStub) ForgetWorkspace(context.Context, string) error {
	s.calls["forget"]++
	return s.errors["forget"]
}
func (s *apiApplicationStub) RelinkWorkspace(context.Context, string, string) (registry.Entry, error) {
	s.calls["relink"]++
	return s.entry, s.errors["relink"]
}
func (s *apiApplicationStub) EffectiveConfig(context.Context, string) (config.Effective, error) {
	s.calls["config.get"]++
	return config.Effective{Config: config.Config{Repos: map[string]config.RepoConfig{}, Modules: map[string]config.ModuleConfig{}}, Sources: config.SourceMap{}}, s.errors["config.get"]
}
func (s *apiApplicationStub) WriteWorkspaceConfig(context.Context, string, config.Override) (config.Effective, error) {
	s.calls["config.put"]++
	return config.Effective{Config: config.Config{Repos: map[string]config.RepoConfig{}, Modules: map[string]config.ModuleConfig{}}, Sources: config.SourceMap{}}, s.errors["config.put"]
}
func (s *apiApplicationStub) Health(context.Context, string) (app.Health, error) {
	s.calls["health"]++
	return app.Health{}, s.errors["health"]
}
func (s *apiApplicationStub) InstallDependency(context.Context, string, string) (jobs.Snapshot, error) {
	s.calls["dependency.install"]++
	return s.job, s.errors["dependency.install"]
}
func (s *apiApplicationStub) CreateSite(_ context.Context, _ string, request app.CreateSiteRequest, _ string) (jobs.Snapshot, error) {
	s.calls["site.create"]++
	s.siteRequest = request
	return s.job, s.errors["site.create"]
}
func (s *apiApplicationStub) CreateLayer(context.Context, string, app.CreateLayerRequest, string) (jobs.Snapshot, error) {
	s.calls["layer.create"]++
	return s.job, s.errors["layer.create"]
}
func (s *apiApplicationStub) Site(context.Context, string, string) (project.Site, error) {
	s.calls["site.get"]++
	return s.site, s.errors["site.get"]
}
func (s *apiApplicationStub) Tasks(context.Context, string, string) ([]project.Task, error) {
	s.calls["tasks"]++
	return []project.Task{{ID: "script:dev", LongRunning: true}}, s.errors["tasks"]
}
func (s *apiApplicationStub) Components(context.Context, string, string) (app.ComponentRegistryView, error) {
	s.calls["components"]++
	return s.components, s.errors["components"]
}
func (s *apiApplicationStub) RebuildComponentRegistry(context.Context, string, string, string) (jobs.Snapshot, error) {
	s.calls["components.rebuild"]++
	return s.job, s.errors["components.rebuild"]
}
func (s *apiApplicationStub) OpenComponentSource(_ context.Context, _, _, componentID string) error {
	s.calls["components.open"]++
	s.openedComponentID = componentID
	return s.errors["components.open"]
}
func (s *apiApplicationStub) ComponentPreviewHost(context.Context, string, string) (preview.View, error) {
	s.calls["components.preview.status"]++
	return s.previewHost, s.errors["components.preview.status"]
}
func (s *apiApplicationStub) StartComponentPreviewHost(context.Context, string, string) (preview.View, error) {
	s.calls["components.preview.start"]++
	return s.previewHost, s.errors["components.preview.start"]
}
func (s *apiApplicationStub) StopComponentPreviewHost(context.Context, string, string) (preview.View, error) {
	s.calls["components.preview.stop"]++
	return s.previewHost, s.errors["components.preview.stop"]
}
func (s *apiApplicationStub) RunTask(context.Context, string, string, string, string) (app.TaskRunResult, error) {
	s.calls["task.run"]++
	return app.TaskRunResult{Job: &s.job}, s.errors["task.run"]
}
func (s *apiApplicationStub) StartProcess(context.Context, string, string, string) (processes.Snapshot, error) {
	s.calls["process.start"]++
	return s.process, s.errors["process.start"]
}
func (s *apiApplicationStub) Process(string) (processes.Snapshot, error) {
	s.calls["process.get"]++
	return s.process, s.errors["process.get"]
}
func (s *apiApplicationStub) StopProcess(context.Context, string) error {
	s.calls["process.stop"]++
	return s.errors["process.stop"]
}
func (s *apiApplicationStub) RestartProcess(context.Context, string) (processes.Snapshot, error) {
	s.calls["process.restart"]++
	return s.process, s.errors["process.restart"]
}
func (s *apiApplicationStub) SubscribeProcess(string, uint64) (<-chan processes.Event, func(), error) {
	s.calls["process.events"]++
	stream := make(chan processes.Event, 1)
	stream <- processes.Event{ID: 2, Time: time.Now(), Stream: "stdout", Code: "process.output", Text: "ready"}
	close(stream)
	return stream, func() {}, s.errors["process.events"]
}
func (s *apiApplicationStub) Job(string) (jobs.Snapshot, error) {
	s.calls["job.get"]++
	return s.job, s.errors["job.get"]
}
func (s *apiApplicationStub) CancelJob(string) error {
	s.calls["job.cancel"]++
	return s.errors["job.cancel"]
}
func (s *apiApplicationStub) SubscribeJob(_ string, after uint64) (<-chan jobs.Event, func(), error) {
	s.calls["job.events"]++
	s.lastEventID = after
	stream := make(chan jobs.Event, 1)
	stream <- jobs.Event{ID: 5, Time: time.Now(), Level: "info", Code: "job.output", Text: "working"}
	close(stream)
	return stream, func() {}, s.errors["job.events"]
}
func (s *apiApplicationStub) ConsumeJobSecret(string, string) (string, error) {
	s.calls["job.secret"]++
	return "generated-password", s.errors["job.secret"]
}

func (s *apiApplicationStub) AgentConversationList(context.Context, string, string) (app.AgentConversationCollection, error) {
	s.calls["agent.list"]++
	return s.agentCollection, s.errors["agent.list"]
}
func (s *apiApplicationStub) AgentConversation(context.Context, string) (model.Conversation, error) {
	s.calls["agent.get"]++
	return s.agentConversation, s.errors["agent.get"]
}
func (s *apiApplicationStub) CreateAgentConversation(_ context.Context, _, _ string, request app.AgentPromptRequest) (model.Conversation, error) {
	s.calls["agent.create"]++
	s.agentPrompt = request
	return s.agentConversation, s.errors["agent.create"]
}
func (s *apiApplicationStub) StartAgentTurn(_ context.Context, _ string, request app.AgentPromptRequest) (model.Conversation, error) {
	s.calls["agent.turn"]++
	s.agentPrompt = request
	return s.agentConversation, s.errors["agent.turn"]
}
func (s *apiApplicationStub) AnswerAgentQuestion(context.Context, string, string, map[string]string) error {
	s.calls["agent.question"]++
	return s.errors["agent.question"]
}
func (s *apiApplicationStub) ResolveAgentApproval(_ context.Context, _, _ string, decision conversations.ApprovalDecision) error {
	s.calls["agent.approval"]++
	s.approvalDecision = decision
	return s.errors["agent.approval"]
}
func (s *apiApplicationStub) GrantAgentConsent(_ context.Context, _, requestID string, kind model.ConsentKind, granted bool) error {
	s.calls["agent.consent"]++
	s.consentRequestID, s.consentKind, s.consentGranted = requestID, kind, granted
	return s.errors["agent.consent"]
}
func (s *apiApplicationStub) InterruptAgentConversation(context.Context, string) error {
	s.calls["agent.interrupt"]++
	return s.errors["agent.interrupt"]
}
func (s *apiApplicationStub) SubscribeAgentConversation(_ context.Context, _ string, after uint64) (<-chan model.Event, error) {
	s.calls["agent.events"]++
	s.lastEventID = after
	stream := make(chan model.Event, 1)
	stream <- model.Event{Sequence: 7, Type: model.EventConversationSnapshot, ConversationID: "c1", CreatedAt: time.Now().UTC()}
	close(stream)
	return stream, s.errors["agent.events"]
}
