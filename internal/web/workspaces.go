package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/nuxt-apps/couchfusion/internal/app"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
)

type Application interface {
	ListWorkspaces(context.Context) ([]registry.Entry, error)
	RegisterWorkspace(context.Context, string, string) (registry.Entry, error)
	InitializeWorkspace(context.Context, app.InitializeWorkspaceRequest, string) (jobs.Snapshot, error)
	Workspace(context.Context, string) (project.Workspace, error)
	RenameWorkspace(context.Context, string, string) (registry.Entry, error)
	ForgetWorkspace(context.Context, string) error
	RelinkWorkspace(context.Context, string, string) (registry.Entry, error)
	EffectiveConfig(context.Context, string) (config.Effective, error)
	WriteWorkspaceConfig(context.Context, string, config.Override) (config.Effective, error)
	Health(context.Context, string) (app.Health, error)
	InstallDependency(context.Context, string, string) (jobs.Snapshot, error)
	CreateSite(context.Context, string, app.CreateSiteRequest, string) (jobs.Snapshot, error)
	CreateLayer(context.Context, string, app.CreateLayerRequest, string) (jobs.Snapshot, error)
	Site(context.Context, string, string) (project.Site, error)
	Tasks(context.Context, string, string) ([]project.Task, error)
	RunTask(context.Context, string, string, string, string) (app.TaskRunResult, error)
	StartProcess(context.Context, string, string, string) (processes.Snapshot, error)
	Process(string) (processes.Snapshot, error)
	StopProcess(context.Context, string) error
	RestartProcess(context.Context, string) (processes.Snapshot, error)
	SubscribeProcess(string, uint64) (<-chan processes.Event, func(), error)
	Job(string) (jobs.Snapshot, error)
	CancelJob(string) error
	SubscribeJob(string, uint64) (<-chan jobs.Event, func(), error)
	ConsumeJobSecret(string, string) (string, error)
}

type apiHandler struct {
	application Application
}

type workspaceSummary struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Root         string `json:"root"`
	Available    bool   `json:"available"`
	Problem      string `json:"problem,omitempty"`
	CreatedAt    string `json:"createdAt"`
	LastOpenedAt string `json:"lastOpenedAt"`
}

func newAPIHandler(application Application) http.Handler {
	handler := apiHandler{application: application}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/workspaces", handler.listWorkspaces)
	mux.HandleFunc("POST /api/v1/workspaces/register", handler.registerWorkspace)
	mux.HandleFunc("POST /api/v1/workspaces/initialize", handler.initializeWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}", handler.getWorkspace)
	mux.HandleFunc("PATCH /api/v1/workspaces/{workspaceId}", handler.renameWorkspace)
	mux.HandleFunc("DELETE /api/v1/workspaces/{workspaceId}", handler.forgetWorkspace)
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceId}/relink", handler.relinkWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/config", handler.getConfig)
	mux.HandleFunc("PUT /api/v1/workspaces/{workspaceId}/config", handler.putConfig)
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/health", handler.getHealth)
	mux.HandleFunc("POST /api/v1/system/dependencies/{component}/install", handler.installDependency)
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceId}/sites", handler.createSite)
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceId}/layers", handler.createLayer)
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/sites/{siteId}", handler.getSite)
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/sites/{siteId}/tasks", handler.getTasks)
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceId}/sites/{siteId}/tasks/{taskId}/run", handler.runTask)
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceId}/sites/{siteId}/processes/{taskId}/start", handler.startProcess)
	mux.HandleFunc("GET /api/v1/jobs/{jobId}", handler.getJob)
	mux.HandleFunc("POST /api/v1/jobs/{jobId}/cancel", handler.cancelJob)
	mux.HandleFunc("POST /api/v1/jobs/{jobId}/secret/consume", handler.consumeSecret)
	mux.HandleFunc("GET /api/v1/jobs/{jobId}/events", handler.jobEvents)
	mux.HandleFunc("GET /api/v1/processes/{processId}", handler.getProcess)
	mux.HandleFunc("POST /api/v1/processes/{processId}/stop", handler.stopProcess)
	mux.HandleFunc("POST /api/v1/processes/{processId}/restart", handler.restartProcess)
	mux.HandleFunc("GET /api/v1/processes/{processId}/events", handler.processEvents)
	mux.HandleFunc("/api/v1/", func(writer http.ResponseWriter, _ *http.Request) {
		writeError(writer, http.StatusNotFound, "not_found", "The requested API route was not found")
	})
	return mux
}

func (h apiHandler) listWorkspaces(writer http.ResponseWriter, request *http.Request) {
	entries, err := h.application.ListWorkspaces(request.Context())
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	result := make([]workspaceSummary, 0, len(entries))
	for _, entry := range entries {
		result = append(result, summarizeWorkspace(entry))
	}
	writeData(writer, http.StatusOK, result)
}

func (h apiHandler) registerWorkspace(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Root string `json:"root"`
		Name string `json:"name"`
	}
	if !decodeRequest(writer, request, &input) {
		return
	}
	entry, err := h.application.RegisterWorkspace(request.Context(), input.Root, input.Name)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusCreated, summarizeWorkspace(entry))
}

func (h apiHandler) initializeWorkspace(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Root        string `json:"root"`
		Name        string `json:"name"`
		LayerBranch string `json:"layerBranch"`
		Force       bool   `json:"force"`
	}
	if !decodeRequest(writer, request, &input) {
		return
	}
	sessionID, _ := SessionID(request.Context())
	job, err := h.application.InitializeWorkspace(request.Context(), app.InitializeWorkspaceRequest{Root: input.Root, Name: input.Name, LayerBranch: input.LayerBranch, Force: input.Force}, sessionID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, job)
}

func (h apiHandler) getWorkspace(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	workspace, err := h.application.Workspace(request.Context(), workspaceID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, workspace)
}

func (h apiHandler) renameWorkspace(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeRequest(writer, request, &input) {
		return
	}
	entry, err := h.application.RenameWorkspace(request.Context(), workspaceID, input.Name)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, summarizeWorkspace(entry))
}

func (h apiHandler) forgetWorkspace(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	if err := h.application.ForgetWorkspace(request.Context(), workspaceID); err != nil {
		writeApplicationError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h apiHandler) relinkWorkspace(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	var input struct {
		Root string `json:"root"`
	}
	if !decodeRequest(writer, request, &input) {
		return
	}
	entry, err := h.application.RelinkWorkspace(request.Context(), workspaceID, input.Root)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, summarizeWorkspace(entry))
}

func (h apiHandler) getConfig(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	effective, err := h.application.EffectiveConfig(request.Context(), workspaceID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, effective)
}

func (h apiHandler) putConfig(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	var override config.Override
	if !decodeRequest(writer, request, &override) {
		return
	}
	effective, err := h.application.WriteWorkspaceConfig(request.Context(), workspaceID, override)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, effective)
}

func (h apiHandler) getHealth(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	health, err := h.application.Health(request.Context(), workspaceID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, health)
}

func (h apiHandler) installDependency(writer http.ResponseWriter, request *http.Request) {
	component, ok := pathID(writer, request, "component")
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	sessionID, _ := SessionID(request.Context())
	job, err := h.application.InstallDependency(request.Context(), component, sessionID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, job)
}

func (h apiHandler) createSite(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	var input struct {
		Name     string   `json:"name"`
		Modules  []string `json:"modules"`
		Branch   string   `json:"branch"`
		Force    bool     `json:"force"`
		Username string   `json:"username"`
		Password string   `json:"password"`
	}
	if !decodeRequest(writer, request, &input) {
		return
	}
	sessionID, _ := SessionID(request.Context())
	job, err := h.application.CreateSite(request.Context(), workspaceID, app.CreateSiteRequest{Name: input.Name, Modules: append([]string{}, input.Modules...), Branch: input.Branch, Force: input.Force, Username: input.Username, Password: input.Password}, sessionID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, job)
}

func (h apiHandler) createLayer(writer http.ResponseWriter, request *http.Request) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return
	}
	var input struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
		Force  bool   `json:"force"`
	}
	if !decodeRequest(writer, request, &input) {
		return
	}
	sessionID, _ := SessionID(request.Context())
	job, err := h.application.CreateLayer(request.Context(), workspaceID, app.CreateLayerRequest{Name: input.Name, Branch: input.Branch, Force: input.Force}, sessionID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, job)
}

func (h apiHandler) getSite(writer http.ResponseWriter, request *http.Request) {
	workspaceID, siteID, ok := workspaceSiteIDs(writer, request)
	if !ok {
		return
	}
	site, err := h.application.Site(request.Context(), workspaceID, siteID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, site)
}

func (h apiHandler) getTasks(writer http.ResponseWriter, request *http.Request) {
	workspaceID, siteID, ok := workspaceSiteIDs(writer, request)
	if !ok {
		return
	}
	tasks, err := h.application.Tasks(request.Context(), workspaceID, siteID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, tasks)
}

func (h apiHandler) runTask(writer http.ResponseWriter, request *http.Request) {
	workspaceID, siteID, ok := workspaceSiteIDs(writer, request)
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	taskID, ok := pathID(writer, request, "taskId")
	if !ok {
		return
	}
	sessionID, _ := SessionID(request.Context())
	result, err := h.application.RunTask(request.Context(), workspaceID, siteID, taskID, sessionID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, result)
}

func (h apiHandler) startProcess(writer http.ResponseWriter, request *http.Request) {
	workspaceID, siteID, ok := workspaceSiteIDs(writer, request)
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	taskID, ok := pathID(writer, request, "taskId")
	if !ok {
		return
	}
	process, err := h.application.StartProcess(request.Context(), workspaceID, siteID, taskID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, process)
}

func (h apiHandler) getJob(writer http.ResponseWriter, request *http.Request) {
	jobID, ok := pathID(writer, request, "jobId")
	if !ok {
		return
	}
	job, err := h.application.Job(jobID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, job)
}

func (h apiHandler) cancelJob(writer http.ResponseWriter, request *http.Request) {
	jobID, ok := pathID(writer, request, "jobId")
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	if err := h.application.CancelJob(jobID); err != nil {
		writeApplicationError(writer, err)
		return
	}
	job, err := h.application.Job(jobID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, job)
}

func (h apiHandler) consumeSecret(writer http.ResponseWriter, request *http.Request) {
	jobID, ok := pathID(writer, request, "jobId")
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	sessionID, _ := SessionID(request.Context())
	secret, err := h.application.ConsumeJobSecret(jobID, sessionID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeData(writer, http.StatusOK, map[string]string{"secret": secret})
}

func (h apiHandler) getProcess(writer http.ResponseWriter, request *http.Request) {
	processID, ok := pathID(writer, request, "processId")
	if !ok {
		return
	}
	process, err := h.application.Process(processID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusOK, process)
}

func (h apiHandler) stopProcess(writer http.ResponseWriter, request *http.Request) {
	processID, ok := pathID(writer, request, "processId")
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	if err := h.application.StopProcess(request.Context(), processID); err != nil {
		writeApplicationError(writer, err)
		return
	}
	process, err := h.application.Process(processID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, process)
}

func (h apiHandler) restartProcess(writer http.ResponseWriter, request *http.Request) {
	processID, ok := pathID(writer, request, "processId")
	if !ok || !decodeEmptyRequest(writer, request) {
		return
	}
	process, err := h.application.RestartProcess(request.Context(), processID)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	writeData(writer, http.StatusAccepted, process)
}

func decodeEmptyRequest(writer http.ResponseWriter, request *http.Request) bool {
	return decodeRequest(writer, request, &struct{}{})
}

func decodeRequest(writer http.ResponseWriter, request *http.Request, destination any) bool {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeApplicationError(writer, app.ValidationError("body", "Request body is invalid"))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeApplicationError(writer, app.ValidationError("body", "Request body must contain one JSON value"))
		return false
	}
	return true
}

func pathID(writer http.ResponseWriter, request *http.Request, name string) (string, bool) {
	value := request.PathValue(name)
	if !validResourceID(value) {
		writeApplicationError(writer, app.ValidationError(name, "Resource ID is invalid"))
		return "", false
	}
	return value, true
}

func workspaceSiteIDs(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	workspaceID, ok := pathID(writer, request, "workspaceId")
	if !ok {
		return "", "", false
	}
	siteID, ok := pathID(writer, request, "siteId")
	return workspaceID, siteID, ok
}

func validResourceID(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 256 || strings.ContainsAny(value, `/\`) {
		return false
	}
	for _, item := range value {
		if unicode.IsControl(item) || unicode.IsSpace(item) {
			return false
		}
	}
	return true
}

func summarizeWorkspace(entry registry.Entry) workspaceSummary {
	return workspaceSummary{
		ID:           entry.ID,
		Name:         entry.Name,
		Root:         entry.Root,
		Available:    entry.Available,
		Problem:      entry.Problem,
		CreatedAt:    entry.CreatedAt.UTC().Format(timeFormat),
		LastOpenedAt: entry.LastOpenedAt.UTC().Format(timeFormat),
	}
}

const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func writeData(writer http.ResponseWriter, status int, data any) {
	writeJSON(writer, status, map[string]any{"data": data})
}

func writeApplicationError(writer http.ResponseWriter, err error) {
	if errors.Is(err, jobs.ErrSecretForbidden) {
		writeError(writer, http.StatusForbidden, "unauthorized", "The secret belongs to another session")
		return
	}
	if errors.Is(err, jobs.ErrSecretUnavailable) {
		writeError(writer, http.StatusConflict, "conflict", "The secret is unavailable")
		return
	}
	err = app.MapError(err)
	var applicationError *app.Error
	if !errors.As(err, &applicationError) {
		applicationError = &app.Error{Code: app.ErrorInternal, Message: "An internal operation failed"}
	}
	status := http.StatusInternalServerError
	switch applicationError.Code {
	case app.ErrorValidation, app.ErrorConfig:
		status = http.StatusUnprocessableEntity
	case app.ErrorConflict, app.ErrorProcess:
		status = http.StatusConflict
	case app.ErrorNotFound:
		status = http.StatusNotFound
	case app.ErrorUnavailable:
		status = http.StatusServiceUnavailable
	case app.ErrorGit:
		status = http.StatusBadGateway
	}
	payload := map[string]any{"code": applicationError.Code, "message": applicationError.Message}
	if len(applicationError.Fields) > 0 {
		payload["fields"] = applicationError.Fields
	}
	writeJSON(writer, status, map[string]any{"error": payload})
}
