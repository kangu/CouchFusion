package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/app"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
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
	calls       map[string]int
	errors      map[string]error
	lastEventID uint64
	entry       registry.Entry
	workspace   project.Workspace
	site        project.Site
	siteRequest app.CreateSiteRequest
	job         jobs.Snapshot
	process     processes.Snapshot
}

func newAPIApplicationStub() *apiApplicationStub {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return &apiApplicationStub{
		calls:     map[string]int{},
		errors:    map[string]error{},
		entry:     registry.Entry{ID: "w1", Name: "Demo", Root: "/workspace", Available: true},
		workspace: project.Workspace{ID: "w1", Name: "Demo", Root: "/workspace", Available: true, Sites: []project.Site{}, Layers: []project.Layer{}, Warnings: []project.Warning{}},
		site:      project.Site{ID: "shop", Name: "Shop", Root: "/workspace/apps/shop", Modules: []string{"auth"}, Scripts: map[string]string{"dev": "nuxt dev"}, Dependencies: map[string]string{}, DevDependencies: map[string]string{}, Lockfiles: []string{}, Warnings: []project.Warning{}},
		job:       jobs.Snapshot{ID: "job-1", Type: "site.create", WorkspaceID: "w1", Status: jobs.Running, CreatedAt: now, Events: []jobs.Event{}},
		process:   processes.Snapshot{ID: "process-1", WorkspaceID: "w1", SiteID: "shop", TaskID: "script:dev", Status: processes.Running, StartedAt: now, URLs: []string{"http://localhost:3000/"}, Events: []processes.Event{}},
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
