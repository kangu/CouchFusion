package app

import (
	"context"
	"errors"
	"os"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
)

type ErrorCode string

const (
	ErrorValidation  ErrorCode = "validation"
	ErrorConflict    ErrorCode = "conflict"
	ErrorNotFound    ErrorCode = "not_found"
	ErrorUnavailable ErrorCode = "unavailable"
	ErrorFilesystem  ErrorCode = "filesystem"
	ErrorGit         ErrorCode = "git"
	ErrorProcess     ErrorCode = "process"
	ErrorConfig      ErrorCode = "config"
	ErrorInternal    ErrorCode = "internal"
)

type Error struct {
	Code    ErrorCode         `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func MapError(err error) error {
	if err == nil {
		return nil
	}
	var appError *Error
	if errors.As(err, &appError) {
		return appError
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, jobs.ErrShuttingDown), errors.Is(err, processes.ErrShuttingDown):
		return &Error{Code: ErrorUnavailable, Message: "The requested operation is unavailable", cause: err}
	case errors.Is(err, registry.ErrDuplicate), isJobConflict(err):
		return &Error{Code: ErrorConflict, Message: "The requested operation conflicts with active state", cause: err}
	case errors.Is(err, registry.ErrNotFound), errors.Is(err, project.ErrNotFound), errors.Is(err, jobs.ErrNotFound), errors.Is(err, processes.ErrNotFound):
		return &Error{Code: ErrorNotFound, Message: "The requested resource was not found", cause: err}
	case errors.Is(err, registry.ErrInvalidWorkspace), errors.Is(err, registry.ErrInvalidName), errors.Is(err, project.ErrPathEscape), errors.Is(err, project.ErrTaskNotFound):
		return &Error{Code: ErrorValidation, Message: "The request is invalid", cause: err}
	case errors.Is(err, config.ErrInvalidConfig):
		return &Error{Code: ErrorConfig, Message: "The CouchFusion configuration is invalid", cause: err}
	case errors.Is(err, processes.ErrAlreadyRunning):
		return &Error{Code: ErrorProcess, Message: "The managed process conflicts with active state", cause: err}
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return &Error{Code: ErrorFilesystem, Message: "A filesystem operation failed", cause: err}
	}
	return &Error{Code: ErrorInternal, Message: "An internal operation failed", cause: err}
}

func ValidationError(field, message string) error {
	fields := map[string]string{}
	if field != "" {
		fields[field] = message
	}
	return &Error{Code: ErrorValidation, Message: "The request is invalid", Fields: fields}
}

func isJobConflict(err error) bool {
	var conflict jobs.ConflictError
	return errors.As(err, &conflict)
}

type InitializeWorkspaceRequest struct {
	Root        string
	Name        string
	LayerBranch string
	Force       bool
	ConfigPath  string
}

type CreateSiteRequest struct {
	Name       string
	Modules    []string
	Branch     string
	Force      bool
	Username   string
	Password   string
	ConfigPath string
}

type CreateLayerRequest struct {
	Name       string
	Branch     string
	Force      bool
	ConfigPath string
}

type DependencyHealth struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	URL       string `json:"url,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

type Health struct {
	Git     DependencyHealth `json:"git"`
	Bun     DependencyHealth `json:"bun"`
	CouchDB DependencyHealth `json:"couchdb"`
}

type TaskRunResult struct {
	Job     *jobs.Snapshot      `json:"job,omitempty"`
	Process *processes.Snapshot `json:"process,omitempty"`
}

type Registry interface {
	List(context.Context) ([]registry.Entry, error)
	Register(context.Context, string, string) (registry.Entry, error)
	Forget(context.Context, string) error
	Relink(context.Context, string, string) (registry.Entry, error)
	Rename(context.Context, string, string) (registry.Entry, error)
	Touch(context.Context, string) error
}

type ConfigService interface {
	Load(globalPath, workspaceRoot string) (config.Effective, error)
	WriteWorkspace(workspaceRoot, globalPath string, override config.Override) error
	WriteGlobal(path string, cfg config.Config) error
}

type ProjectService interface {
	Workspace(context.Context, registry.Entry) (project.Workspace, error)
	Site(context.Context, string, string) (project.Site, error)
}

type TaskCatalog interface {
	Tasks(project.Site) []project.Task
	Resolve(project.Site, string) (project.Command, error)
}

type Provisioner interface {
	Initialize(context.Context, config.Config, InitializeWorkspaceRequest, jobs.Reporter) error
	CreateSite(context.Context, config.Config, string, CreateSiteRequest, jobs.Reporter) error
	CreateLayer(context.Context, config.Config, string, CreateLayerRequest, jobs.Reporter) error
}

type HealthChecker interface {
	Check(context.Context, string) (Health, error)
}

type DependencyInstaller interface {
	Install(context.Context, string, jobs.Reporter) error
}

type CommandRunner interface {
	Run(context.Context, project.Command, jobs.Reporter) error
}

type JobManager interface {
	Start(jobs.Request) (jobs.Snapshot, error)
	Get(string) (jobs.Snapshot, error)
	Cancel(string) error
	Subscribe(string, uint64) (<-chan jobs.Event, func(), error)
	ConsumeSecret(string, string) (string, error)
	Shutdown(context.Context) error
}

type ProcessManager interface {
	Start(context.Context, processes.StartRequest) (processes.Snapshot, error)
	Stop(context.Context, string) error
	Restart(context.Context, string) (processes.Snapshot, error)
	Get(string) (processes.Snapshot, error)
	Subscribe(string, uint64) (<-chan processes.Event, func(), error)
	Shutdown(context.Context) error
}

type Dependencies struct {
	Registry         Registry
	Configs          ConfigService
	Projects         ProjectService
	Tasks            TaskCatalog
	Provisioner      Provisioner
	Health           HealthChecker
	Installer        DependencyInstaller
	Commands         CommandRunner
	Jobs             JobManager
	Processes        ProcessManager
	GlobalConfigPath string
}
