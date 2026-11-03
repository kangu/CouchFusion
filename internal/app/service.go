package app

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/nuxt-apps/couchfusion/internal/agent/conversations"
	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/bun"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/couchdb"
	"github.com/nuxt-apps/couchfusion/internal/editor"
	"github.com/nuxt-apps/couchfusion/internal/gitsetup"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/onboarding"
	"github.com/nuxt-apps/couchfusion/internal/preview"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
	"github.com/nuxt-apps/couchfusion/internal/workspace"
)

type Service struct {
	dependencies Dependencies
	onboardingMu sync.Mutex
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Registry == nil {
		store, err := registry.DefaultStore()
		if err != nil {
			return nil, MapError(err)
		}
		dependencies.Registry = &registry.Service{Store: store}
	}
	if dependencies.Configs == nil {
		dependencies.Configs = configAdapter{}
	}
	if dependencies.Projects == nil {
		dependencies.Projects = project.Inspector{}
	}
	if dependencies.Tasks == nil {
		dependencies.Tasks = project.Catalog{}
	}
	if dependencies.Components == nil {
		dependencies.Components = project.ComponentInspector{}
	}
	if dependencies.Files == nil {
		dependencies.Files = editor.Opener{}
	}
	if dependencies.Provisioner == nil {
		dependencies.Provisioner = workspaceProvisioner{}
	}
	if dependencies.Health == nil {
		dependencies.Health = systemHealth{}
	}
	if dependencies.Installer == nil {
		dependencies.Installer = systemInstaller{}
	}
	if dependencies.Commands == nil {
		dependencies.Commands = execCommandRunner{}
	}
	if dependencies.Jobs == nil {
		dependencies.Jobs = jobs.NewManager(jobs.Options{})
	}
	if dependencies.Processes == nil {
		dependencies.Processes = processes.NewManager(processes.Options{})
	}
	if dependencies.PreviewHosts == nil {
		dependencies.PreviewHosts = preview.NewManager(preview.Options{Processes: dependencies.Processes})
	}
	if dependencies.AgentConversations == nil {
		dependencies.AgentConversations = unavailableAgentConversations{}
	}
	if dependencies.Onboarding == nil {
		dependencies.Onboarding = &memoryOnboardingStore{state: onboarding.DefaultState()}
	}
	return &Service{dependencies: dependencies}, nil
}

func NewDefaultService(globalConfigPath string) (*Service, error) {
	store, err := onboarding.DefaultStore()
	if err != nil {
		return nil, MapError(err)
	}
	return NewService(Dependencies{GlobalConfigPath: globalConfigPath, Onboarding: store})
}

func (s *Service) InitializeWorkspace(ctx context.Context, request InitializeWorkspaceRequest, sessionID string) (jobs.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return jobs.Snapshot{}, MapError(err)
	}
	if strings.TrimSpace(request.Root) == "" {
		return jobs.Snapshot{}, ValidationError("root", "Workspace root is required")
	}
	snapshot, err := s.dependencies.Jobs.Start(jobs.Request{
		Type:        "workspace.initialize",
		WorkspaceID: request.Root,
		SessionID:   sessionID,
		Run: func(jobContext context.Context, reporter jobs.Reporter) error {
			effective, err := s.dependencies.Configs.Load(s.configPath(request.ConfigPath), "")
			if err != nil {
				return MapError(err)
			}
			if err := s.dependencies.Provisioner.Initialize(jobContext, effective.Config, request, reporter); err != nil {
				return &Error{Code: ErrorGit, Message: "Workspace provisioning failed", cause: err}
			}
			if _, err := s.dependencies.Registry.Register(jobContext, request.Root, request.Name); err != nil {
				return MapError(err)
			}
			return nil
		},
	})
	return snapshot, MapError(err)
}

func (s *Service) RegisterWorkspace(ctx context.Context, root, name string) (registry.Entry, error) {
	entry, err := s.dependencies.Registry.Register(ctx, root, name)
	return entry, MapError(err)
}

func (s *Service) ListWorkspaces(ctx context.Context) ([]registry.Entry, error) {
	entries, err := s.dependencies.Registry.List(ctx)
	return entries, MapError(err)
}

func (s *Service) Workspace(ctx context.Context, workspaceID string) (project.Workspace, error) {
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return project.Workspace{}, err
	}
	result, err := s.dependencies.Projects.Workspace(ctx, entry)
	if err == nil {
		_ = s.dependencies.Registry.Touch(ctx, workspaceID)
	}
	return result, MapError(err)
}

func (s *Service) ForgetWorkspace(ctx context.Context, workspaceID string) error {
	return MapError(s.dependencies.Registry.Forget(ctx, workspaceID))
}

func (s *Service) RelinkWorkspace(ctx context.Context, workspaceID, root string) (registry.Entry, error) {
	entry, err := s.dependencies.Registry.Relink(ctx, workspaceID, root)
	return entry, MapError(err)
}

func (s *Service) RenameWorkspace(ctx context.Context, workspaceID, name string) (registry.Entry, error) {
	entry, err := s.dependencies.Registry.Rename(ctx, workspaceID, name)
	return entry, MapError(err)
}

func (s *Service) EffectiveConfig(ctx context.Context, workspaceID string) (config.Effective, error) {
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return config.Effective{}, err
	}
	effective, err := s.dependencies.Configs.Load(s.dependencies.GlobalConfigPath, entry.Root)
	return effective, MapError(err)
}

func (s *Service) WriteWorkspaceConfig(ctx context.Context, workspaceID string, override config.Override) (config.Effective, error) {
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return config.Effective{}, err
	}
	if err := s.dependencies.Configs.WriteWorkspace(entry.Root, s.dependencies.GlobalConfigPath, override); err != nil {
		return config.Effective{}, MapError(err)
	}
	effective, err := s.dependencies.Configs.Load(s.dependencies.GlobalConfigPath, entry.Root)
	return effective, MapError(err)
}

func (s *Service) Health(ctx context.Context, workspaceID string) (Health, error) {
	root := ""
	if strings.TrimSpace(workspaceID) != "" {
		entry, err := s.workspaceEntry(ctx, workspaceID)
		if err != nil {
			return Health{}, err
		}
		root = entry.Root
	}
	health, err := s.dependencies.Health.Check(ctx, root)
	health.Agent = s.dependencies.AgentConversations.Readiness(ctx)
	return health, MapError(err)
}

func (s *Service) AgentConversationList(ctx context.Context, workspaceID, siteID string) (AgentConversationCollection, error) {
	binding, err := s.agentSiteBinding(ctx, workspaceID, siteID)
	if err != nil {
		return AgentConversationCollection{}, err
	}
	if err := s.recoverAgentBinding(ctx, workspaceID, siteID, binding); err != nil {
		return unavailableAgentCollection(), nil
	}
	items, err := s.dependencies.AgentConversations.ListSite(ctx, workspaceID, siteID)
	if err != nil {
		return unavailableAgentCollection(), nil
	}
	return AgentConversationCollection{
		Readiness: s.dependencies.AgentConversations.Readiness(ctx), Conversations: items,
	}, nil
}

func unavailableAgentCollection() AgentConversationCollection {
	return AgentConversationCollection{
		Readiness:     model.AgentReadiness{Status: model.AgentLaunchFailed, Problem: "CouchFusion agent control data is unavailable"},
		Conversations: []model.Conversation{},
	}
}

func (s *Service) AgentConversation(ctx context.Context, conversationID string) (model.Conversation, error) {
	conversation, err := s.dependencies.AgentConversations.Get(ctx, conversationID)
	return conversation, MapError(err)
}

func (s *Service) CreateAgentConversation(ctx context.Context, workspaceID, siteID string, request AgentPromptRequest) (model.Conversation, error) {
	binding, err := s.agentSiteBinding(ctx, workspaceID, siteID)
	if err != nil {
		return model.Conversation{}, err
	}
	if err := s.recoverAgentBinding(ctx, workspaceID, siteID, binding); err != nil {
		return model.Conversation{}, err
	}
	conversation, err := s.dependencies.AgentConversations.Create(ctx, conversations.CreateRequest{
		WorkspaceID: workspaceID, SiteID: siteID, AppsRoot: binding.appsRoot, SiteRoot: binding.siteRoot,
		Prompt: request.Prompt, IdempotencyKey: request.IdempotencyKey,
	})
	return conversation, MapError(err)
}

func (s *Service) StartAgentTurn(ctx context.Context, conversationID string, request AgentPromptRequest) (model.Conversation, error) {
	conversation, err := s.dependencies.AgentConversations.StartTurn(ctx, conversationID, conversations.TurnRequest{
		Prompt: request.Prompt, IdempotencyKey: request.IdempotencyKey,
	})
	return conversation, MapError(err)
}

func (s *Service) AnswerAgentQuestion(ctx context.Context, conversationID, requestID string, answers map[string]string) error {
	return MapError(s.dependencies.AgentConversations.AnswerQuestion(ctx, conversationID, requestID, answers))
}

func (s *Service) ResolveAgentApproval(ctx context.Context, conversationID, requestID string, decision conversations.ApprovalDecision) error {
	return MapError(s.dependencies.AgentConversations.ResolveApproval(ctx, conversationID, requestID, decision))
}

func (s *Service) GrantAgentConsent(ctx context.Context, conversationID, requestID string, kind model.ConsentKind, granted bool) error {
	return MapError(s.dependencies.AgentConversations.GrantConsent(ctx, conversationID, requestID, kind, granted))
}

func (s *Service) InterruptAgentConversation(ctx context.Context, conversationID string) error {
	return MapError(s.dependencies.AgentConversations.Interrupt(ctx, conversationID))
}

func (s *Service) SubscribeAgentConversation(ctx context.Context, conversationID string, after uint64) (<-chan model.Event, error) {
	events, err := s.dependencies.AgentConversations.Subscribe(ctx, conversationID, after)
	return events, MapError(err)
}

func (s *Service) InstallDependency(ctx context.Context, component, sessionID string) (jobs.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return jobs.Snapshot{}, MapError(err)
	}
	component = strings.ToLower(strings.TrimSpace(component))
	if component != "git" && component != "bun" && component != "couchdb" {
		return jobs.Snapshot{}, ValidationError("component", "Component must be git, bun, or couchdb")
	}
	snapshot, err := s.dependencies.Jobs.Start(jobs.Request{
		Type:      "dependency.install." + component,
		SessionID: sessionID,
		Run: func(jobContext context.Context, reporter jobs.Reporter) error {
			if err := s.dependencies.Installer.Install(jobContext, component, reporter); err != nil {
				return &Error{Code: ErrorUnavailable, Message: "Dependency installation failed", cause: err}
			}
			return nil
		},
	})
	return snapshot, MapError(err)
}

func (s *Service) CreateSite(ctx context.Context, workspaceID string, request CreateSiteRequest, sessionID string) (jobs.Snapshot, error) {
	if err := validateCreateSiteRequest(request); err != nil {
		return jobs.Snapshot{}, err
	}
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return jobs.Snapshot{}, err
	}
	return s.startSiteJob(ctx, workspaceID, entry.Root, request, sessionID)
}

func (s *Service) CreateSiteAtRoot(ctx context.Context, root string, request CreateSiteRequest, sessionID string) (jobs.Snapshot, error) {
	if err := validateCreateSiteRequest(request); err != nil {
		return jobs.Snapshot{}, err
	}
	if strings.TrimSpace(root) == "" {
		return jobs.Snapshot{}, ValidationError("root", "Workspace root is required")
	}
	return s.startSiteJob(ctx, root, root, request, sessionID)
}

func validateCreateSiteRequest(request CreateSiteRequest) error {
	if strings.TrimSpace(request.Name) == "" {
		return ValidationError("name", "Site name is required")
	}
	if err := workspace.ValidateDBLoginPrefix(request.DBLoginPrefix); err != nil {
		return ValidationError("dbLoginPrefix", err.Error())
	}
	return nil
}

func (s *Service) startSiteJob(ctx context.Context, workspaceLock, root string, request CreateSiteRequest, sessionID string) (jobs.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return jobs.Snapshot{}, MapError(err)
	}
	snapshot, err := s.dependencies.Jobs.Start(jobs.Request{
		Type:        "site.create",
		WorkspaceID: workspaceLock,
		SiteID:      request.Name,
		SessionID:   sessionID,
		Run: func(jobContext context.Context, reporter jobs.Reporter) error {
			effective, err := s.dependencies.Configs.Load(s.configPath(request.ConfigPath), root)
			if err != nil {
				return MapError(err)
			}
			if err := s.dependencies.Provisioner.CreateSite(jobContext, effective.Config, root, request, reporter); err != nil {
				return &Error{Code: ErrorGit, Message: "Site provisioning failed", cause: err}
			}
			return nil
		},
	})
	return snapshot, MapError(err)
}

func (s *Service) CreateLayer(ctx context.Context, workspaceID string, request CreateLayerRequest, sessionID string) (jobs.Snapshot, error) {
	if strings.TrimSpace(request.Name) == "" {
		return jobs.Snapshot{}, ValidationError("name", "Layer name is required")
	}
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return jobs.Snapshot{}, err
	}
	return s.startLayerJob(ctx, workspaceID, entry.Root, request, sessionID)
}

func (s *Service) CreateLayerAtRoot(ctx context.Context, root string, request CreateLayerRequest, sessionID string) (jobs.Snapshot, error) {
	if strings.TrimSpace(request.Name) == "" {
		return jobs.Snapshot{}, ValidationError("name", "Layer name is required")
	}
	if strings.TrimSpace(root) == "" {
		return jobs.Snapshot{}, ValidationError("root", "Workspace root is required")
	}
	return s.startLayerJob(ctx, root, root, request, sessionID)
}

func (s *Service) startLayerJob(ctx context.Context, workspaceLock, root string, request CreateLayerRequest, sessionID string) (jobs.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return jobs.Snapshot{}, MapError(err)
	}
	snapshot, err := s.dependencies.Jobs.Start(jobs.Request{
		Type:        "layer.create",
		WorkspaceID: workspaceLock,
		SessionID:   sessionID,
		Run: func(jobContext context.Context, reporter jobs.Reporter) error {
			effective, err := s.dependencies.Configs.Load(s.configPath(request.ConfigPath), root)
			if err != nil {
				return MapError(err)
			}
			if err := s.dependencies.Provisioner.CreateLayer(jobContext, effective.Config, root, request, reporter); err != nil {
				return &Error{Code: ErrorGit, Message: "Layer provisioning failed", cause: err}
			}
			return nil
		},
	})
	return snapshot, MapError(err)
}

func (s *Service) Site(ctx context.Context, workspaceID, siteID string) (project.Site, error) {
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return project.Site{}, err
	}
	site, err := s.dependencies.Projects.Site(ctx, entry.Root, siteID)
	return site, MapError(err)
}

func (s *Service) Tasks(ctx context.Context, workspaceID, siteID string) ([]project.Task, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return nil, err
	}
	return s.dependencies.Tasks.Tasks(site), nil
}

func (s *Service) Components(ctx context.Context, workspaceID, siteID string) (ComponentRegistryView, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return ComponentRegistryView{}, err
	}
	registry := s.dependencies.Components.Inspect(ctx, site.Root)
	return ComponentRegistryView{
		ComponentRegistry: registry,
		CanRebuild:        hasComponentRegistryTask(s.dependencies.Tasks.Tasks(site)),
	}, nil
}

func (s *Service) RebuildComponentRegistry(ctx context.Context, workspaceID, siteID, sessionID string) (jobs.Snapshot, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return jobs.Snapshot{}, err
	}
	if !hasComponentRegistryTask(s.dependencies.Tasks.Tasks(site)) {
		return jobs.Snapshot{}, ValidationError("siteId", "Component registry rebuild is unavailable")
	}
	command, err := s.dependencies.Tasks.Resolve(site, "script:registry:build")
	if err != nil {
		return jobs.Snapshot{}, MapError(err)
	}
	snapshot, err := s.dependencies.Jobs.Start(jobs.Request{
		Type:        "component-registry.rebuild",
		WorkspaceID: workspaceID,
		SiteID:      site.ID,
		SessionID:   sessionID,
		Run: func(jobContext context.Context, reporter jobs.Reporter) error {
			if err := s.dependencies.Commands.Run(jobContext, command, reporter); err != nil {
				return &Error{Code: ErrorProcess, Message: "Component registry rebuild failed", cause: err}
			}
			return nil
		},
	})
	return snapshot, MapError(err)
}

func (s *Service) OpenComponentSource(ctx context.Context, workspaceID, siteID, componentID string) error {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return err
	}
	filename, err := s.dependencies.Components.ResolveSource(ctx, site.Root, componentID)
	if err != nil {
		return MapError(err)
	}
	if err := s.dependencies.Files.Open(filename); err != nil {
		return &Error{Code: ErrorProcess, Message: "Could not open component source", cause: err}
	}
	return nil
}

func (s *Service) ComponentPreviewHost(ctx context.Context, workspaceID, siteID string) (preview.View, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return preview.View{}, err
	}
	view, err := s.dependencies.PreviewHosts.Status(ctx, workspaceID, site.ID)
	return view, MapError(err)
}

func (s *Service) StartComponentPreviewHost(ctx context.Context, workspaceID, siteID string) (preview.View, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return preview.View{}, err
	}
	found := false
	for _, task := range s.dependencies.Tasks.Tasks(site) {
		if task.ID == "script:dev" && task.LongRunning {
			found = true
			break
		}
	}
	if !found {
		return preview.View{}, ValidationError("site", "Site must expose a managed dev task")
	}
	command, err := s.dependencies.Tasks.Resolve(site, "script:dev")
	if err != nil {
		return preview.View{}, MapError(err)
	}
	view, err := s.dependencies.PreviewHosts.Start(ctx, workspaceID, site.ID, command)
	return view, MapError(err)
}

func (s *Service) StopComponentPreviewHost(ctx context.Context, workspaceID, siteID string) (preview.View, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return preview.View{}, err
	}
	view, err := s.dependencies.PreviewHosts.Stop(ctx, workspaceID, site.ID)
	return view, MapError(err)
}

func hasComponentRegistryTask(tasks []project.Task) bool {
	for _, task := range tasks {
		if task.ID == "script:registry:build" && !task.LongRunning {
			return true
		}
	}
	return false
}

func (s *Service) RunTask(ctx context.Context, workspaceID, siteID, taskID, sessionID string) (TaskRunResult, error) {
	site, task, command, err := s.resolveTask(ctx, workspaceID, siteID, taskID)
	if err != nil {
		return TaskRunResult{}, err
	}
	if task.LongRunning {
		process, err := s.dependencies.Processes.Start(ctx, processes.StartRequest{WorkspaceID: workspaceID, SiteID: site.ID, TaskID: task.ID, Command: command})
		if err != nil {
			return TaskRunResult{}, MapError(err)
		}
		return TaskRunResult{Process: &process}, nil
	}
	job, err := s.dependencies.Jobs.Start(jobs.Request{
		Type:        "task.run",
		WorkspaceID: workspaceID,
		SiteID:      site.ID,
		SessionID:   sessionID,
		Run: func(jobContext context.Context, reporter jobs.Reporter) error {
			if err := s.dependencies.Commands.Run(jobContext, command, reporter); err != nil {
				return &Error{Code: ErrorProcess, Message: "Task execution failed", cause: err}
			}
			return nil
		},
	})
	if err != nil {
		return TaskRunResult{}, MapError(err)
	}
	return TaskRunResult{Job: &job}, nil
}

func (s *Service) StartProcess(ctx context.Context, workspaceID, siteID, taskID string) (processes.Snapshot, error) {
	site, task, command, err := s.resolveTask(ctx, workspaceID, siteID, taskID)
	if err != nil {
		return processes.Snapshot{}, err
	}
	if !task.LongRunning {
		return processes.Snapshot{}, ValidationError("taskId", "Task is not a managed long-running process")
	}
	snapshot, err := s.dependencies.Processes.Start(ctx, processes.StartRequest{WorkspaceID: workspaceID, SiteID: site.ID, TaskID: task.ID, Command: command})
	return snapshot, MapError(err)
}

func (s *Service) Job(id string) (jobs.Snapshot, error) {
	snapshot, err := s.dependencies.Jobs.Get(id)
	return snapshot, MapError(err)
}

func (s *Service) CancelJob(id string) error {
	return MapError(s.dependencies.Jobs.Cancel(id))
}

func (s *Service) SubscribeJob(id string, after uint64) (<-chan jobs.Event, func(), error) {
	events, unsubscribe, err := s.dependencies.Jobs.Subscribe(id, after)
	return events, unsubscribe, MapError(err)
}

func (s *Service) ConsumeJobSecret(id, sessionID string) (string, error) {
	secret, err := s.dependencies.Jobs.ConsumeSecret(id, sessionID)
	return secret, MapError(err)
}

func (s *Service) Process(id string) (processes.Snapshot, error) {
	snapshot, err := s.dependencies.Processes.Get(id)
	return snapshot, MapError(err)
}

func (s *Service) SiteProcess(_ context.Context, workspaceID, siteID, taskID string) (processes.Snapshot, error) {
	snapshot, err := s.dependencies.Processes.Find(workspaceID, siteID, taskID)
	return snapshot, MapError(err)
}

func (s *Service) StopProcess(ctx context.Context, id string) error {
	return MapError(s.dependencies.Processes.Stop(ctx, id))
}

func (s *Service) RestartProcess(ctx context.Context, id string) (processes.Snapshot, error) {
	snapshot, err := s.dependencies.Processes.Restart(ctx, id)
	return snapshot, MapError(err)
}

func (s *Service) SubscribeProcess(id string, after uint64) (<-chan processes.Event, func(), error) {
	events, unsubscribe, err := s.dependencies.Processes.Subscribe(id, after)
	return events, unsubscribe, MapError(err)
}

func (s *Service) Shutdown(ctx context.Context) error {
	agentErr := s.dependencies.AgentConversations.Shutdown(ctx)
	jobErr := s.dependencies.Jobs.Shutdown(ctx)
	processErr := s.dependencies.Processes.Shutdown(ctx)
	if agentErr != nil {
		return MapError(agentErr)
	}
	if jobErr != nil {
		return MapError(jobErr)
	}
	return MapError(processErr)
}

type agentBinding struct {
	appsRoot string
	siteRoot string
}

func (s *Service) recoverAgentBinding(ctx context.Context, workspaceID, siteID string, binding agentBinding) error {
	if err := s.dependencies.AgentConversations.Recover(ctx, []conversations.RecoveryBinding{{
		WorkspaceID: workspaceID, SiteID: siteID, AppsRoot: binding.appsRoot, SiteRoot: binding.siteRoot,
	}}); err != nil {
		return &Error{Code: ErrorUnavailable, Message: "Component generation is unavailable", cause: err}
	}
	return nil
}

func (s *Service) agentSiteBinding(ctx context.Context, workspaceID, siteID string) (agentBinding, error) {
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil {
		return agentBinding{}, err
	}
	workspaceRoot, err := filepath.EvalSymlinks(entry.Root)
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	workspaceRoot, err = filepath.Abs(workspaceRoot)
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	appsRoot, err := filepath.EvalSymlinks(filepath.Join(workspaceRoot, "apps"))
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	appsRoot, err = filepath.Abs(appsRoot)
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	appsRelative, err := filepath.Rel(workspaceRoot, appsRoot)
	if err != nil || filepath.Clean(appsRelative) != "apps" {
		return agentBinding{}, MapError(project.ErrPathEscape)
	}
	site, err := s.dependencies.Projects.Site(ctx, entry.Root, siteID)
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	if site.ID != siteID {
		return agentBinding{}, MapError(project.ErrPathEscape)
	}
	siteRoot, err := filepath.EvalSymlinks(site.Root)
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	siteRoot, err = filepath.Abs(siteRoot)
	if err != nil {
		return agentBinding{}, MapError(err)
	}
	relative, err := filepath.Rel(appsRoot, siteRoot)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return agentBinding{}, MapError(project.ErrPathEscape)
	}
	return agentBinding{appsRoot: appsRoot, siteRoot: siteRoot}, nil
}

func (s *Service) workspaceEntry(ctx context.Context, workspaceID string) (registry.Entry, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return registry.Entry{}, ValidationError("workspaceId", "Workspace ID is required")
	}
	entries, err := s.dependencies.Registry.List(ctx)
	if err != nil {
		return registry.Entry{}, MapError(err)
	}
	for _, entry := range entries {
		if entry.ID == workspaceID {
			return entry, nil
		}
	}
	return registry.Entry{}, MapError(registry.ErrNotFound)
}

func (s *Service) configPath(requestPath string) string {
	if strings.TrimSpace(requestPath) != "" {
		return requestPath
	}
	return s.dependencies.GlobalConfigPath
}

func (s *Service) resolveTask(ctx context.Context, workspaceID, siteID, taskID string) (project.Site, project.Task, project.Command, error) {
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return project.Site{}, project.Task{}, project.Command{}, err
	}
	var selected project.Task
	found := false
	for _, task := range s.dependencies.Tasks.Tasks(site) {
		if task.ID == taskID {
			selected = task
			found = true
			break
		}
	}
	if !found {
		return project.Site{}, project.Task{}, project.Command{}, MapError(project.ErrTaskNotFound)
	}
	command, err := s.dependencies.Tasks.Resolve(site, taskID)
	if err != nil {
		return project.Site{}, project.Task{}, project.Command{}, MapError(err)
	}
	return site, selected, command, nil
}

type registryCompletionHook struct {
	tasks    TaskCatalog
	commands CommandRunner
}

func NewRegistryCompletionHook(tasks TaskCatalog, commands CommandRunner) conversations.CompletionHook {
	return registryCompletionHook{tasks: tasks, commands: commands}
}

func (h registryCompletionHook) Complete(ctx context.Context, request conversations.CompletionRequest) model.RegistryResult {
	if h.tasks == nil || h.commands == nil {
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry rebuild failed"}
	}
	site := project.Site{ID: request.SiteID, Root: request.SiteRoot, Scripts: map[string]string{"registry:build": "managed"}}
	if !hasComponentRegistryTask(h.tasks.Tasks(site)) {
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry rebuild failed"}
	}
	command, err := h.tasks.Resolve(site, "script:registry:build")
	if err != nil || strings.TrimSpace(command.Executable) == "" {
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry rebuild failed"}
	}
	resolvedDir, err := filepath.Abs(filepath.Clean(command.Dir))
	if err != nil || resolvedDir != filepath.Clean(request.SiteRoot) {
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry rebuild failed"}
	}
	if err := h.commands.Run(ctx, command, boundedAgentReporter{}); err != nil {
		return model.RegistryResult{State: model.RegistryFailed, Problem: "Registry rebuild failed"}
	}
	return model.RegistryResult{State: model.RegistrySucceeded, GeneratedComponentID: generatedComponentID(request.ChangedFiles)}
}

type boundedAgentReporter struct{}

func (boundedAgentReporter) Event(string, string, string) {}
func (boundedAgentReporter) Secret(string)                {}

func generatedComponentID(changes []model.ChangedFile) string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		path := filepath.ToSlash(change.Path)
		if change.Kind == model.ChangeAdded && strings.HasSuffix(strings.ToLower(path), ".vue") &&
			(strings.Contains(path, "/components/content/") || strings.HasPrefix(path, "components/content/") || strings.HasPrefix(path, "app/components/content/")) {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	name := strings.TrimSuffix(filepath.Base(paths[0]), filepath.Ext(paths[0]))
	var result strings.Builder
	for index, character := range name {
		if unicode.IsUpper(character) && index > 0 {
			result.WriteByte('-')
		}
		result.WriteRune(unicode.ToLower(character))
	}
	return result.String()
}

type unavailableAgentConversations struct{}

func (unavailableAgentConversations) Readiness(context.Context) model.AgentReadiness {
	return model.AgentReadiness{Status: model.AgentMissing, Problem: "Codex component generation is unavailable"}
}
func (unavailableAgentConversations) ListSite(context.Context, string, string) ([]model.Conversation, error) {
	return []model.Conversation{}, nil
}
func (unavailableAgentConversations) Get(context.Context, string) (model.Conversation, error) {
	return model.Conversation{}, conversations.ErrUnavailable
}
func (unavailableAgentConversations) Create(context.Context, conversations.CreateRequest) (model.Conversation, error) {
	return model.Conversation{}, conversations.ErrUnavailable
}
func (unavailableAgentConversations) StartTurn(context.Context, string, conversations.TurnRequest) (model.Conversation, error) {
	return model.Conversation{}, conversations.ErrUnavailable
}
func (unavailableAgentConversations) AnswerQuestion(context.Context, string, string, map[string]string) error {
	return conversations.ErrUnavailable
}
func (unavailableAgentConversations) ResolveApproval(context.Context, string, string, conversations.ApprovalDecision) error {
	return conversations.ErrUnavailable
}
func (unavailableAgentConversations) GrantConsent(context.Context, string, string, model.ConsentKind, bool) error {
	return conversations.ErrUnavailable
}
func (unavailableAgentConversations) Interrupt(context.Context, string) error {
	return conversations.ErrUnavailable
}
func (unavailableAgentConversations) Subscribe(context.Context, string, uint64) (<-chan model.Event, error) {
	return nil, conversations.ErrUnavailable
}
func (unavailableAgentConversations) Recover(context.Context, []conversations.RecoveryBinding) error {
	return nil
}
func (unavailableAgentConversations) Shutdown(context.Context) error { return nil }

func NewCommandRunner() CommandRunner { return execCommandRunner{} }

type configAdapter struct{}

func (configAdapter) Load(globalPath, workspaceRoot string) (config.Effective, error) {
	return config.LoadEffective(globalPath, workspaceRoot)
}
func (configAdapter) WriteWorkspace(workspaceRoot, globalPath string, override config.Override) error {
	return config.WriteWorkspace(workspaceRoot, globalPath, override)
}
func (configAdapter) WriteGlobal(path string, cfg config.Config) error {
	return config.WriteGlobal(path, cfg)
}

type workspaceProvisioner struct{}

func (workspaceProvisioner) Initialize(ctx context.Context, cfg config.Config, request InitializeWorkspaceRequest, reporter jobs.Reporter) error {
	return workspace.Init(ctx, &cfg, workspace.InitRequest{Root: request.Root, LayerBranch: request.LayerBranch, Force: request.Force}, workspace.Dependencies{Events: workspaceEventReporter{reporter: reporter}})
}

func (workspaceProvisioner) CreateSite(ctx context.Context, cfg config.Config, root string, request CreateSiteRequest, reporter jobs.Reporter) error {
	var auth *workspace.AuthCredentials
	if request.Username != "" || request.Password != "" {
		auth = &workspace.AuthCredentials{Username: request.Username, Password: request.Password}
	}
	return workspace.CreateSite(ctx, &cfg, workspace.CreateSiteRequest{Root: root, Name: request.Name, Modules: append([]string{}, request.Modules...), DBLoginPrefix: request.DBLoginPrefix, Branch: request.Branch, Force: request.Force, Auth: auth}, workspace.Dependencies{Events: workspaceEventReporter{reporter: reporter}})
}

func (workspaceProvisioner) CreateLayer(ctx context.Context, cfg config.Config, root string, request CreateLayerRequest, reporter jobs.Reporter) error {
	return workspace.CreateLayer(ctx, &cfg, workspace.CreateLayerRequest{Root: root, Name: request.Name, Branch: request.Branch, Force: request.Force}, workspace.Dependencies{Events: workspaceEventReporter{reporter: reporter}})
}

type workspaceEventReporter struct{ reporter jobs.Reporter }

func (r workspaceEventReporter) Emit(event workspace.Event) {
	r.reporter.Event(string(event.Level), event.Code, event.Message)
}

type systemHealth struct {
	git     gitsetup.Prober
	bun     bun.Prober
	couchDB couchdb.Prober
}

func (h systemHealth) Check(ctx context.Context, _ string) (Health, error) {
	gitProbe := h.git
	if gitProbe == nil {
		gitProbe = gitsetup.CommandProber{}
	}
	bunProbe := h.bun
	if bunProbe == nil {
		bunProbe = bun.CommandProber{}
	}
	couchProbe := h.couchDB
	if couchProbe == nil {
		couchProbe = couchdb.HTTPProber{BaseURL: couchdb.DefaultURL}
	}
	gitStatus, err := gitProbe.Probe(ctx)
	if err != nil {
		return Health{}, err
	}
	bunStatus, err := bunProbe.Probe(ctx)
	if err != nil {
		return Health{}, err
	}
	couchStatus, err := couchProbe.Probe(ctx)
	if err != nil {
		return Health{}, err
	}
	nodeStatus := probeCommand(ctx, "node", "--version")
	return Health{
		Git:     DependencyHealth{Available: gitStatus.Available, Version: gitStatus.Version, Path: gitStatus.Path},
		Bun:     DependencyHealth{Available: bunStatus.Available, Version: bunStatus.Version, Path: bunStatus.Path},
		Node:    nodeStatus,
		CouchDB: DependencyHealth{Available: couchStatus.Running, Version: couchStatus.Version, URL: couchStatus.URL},
	}, nil
}

func probeCommand(ctx context.Context, name string, args ...string) DependencyHealth {
	path, err := exec.LookPath(name)
	if err != nil {
		return DependencyHealth{Available: false, Problem: name + " was not found on PATH"}
	}
	output, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return DependencyHealth{Available: false, Path: path, Problem: name + " did not report a version"}
	}
	return DependencyHealth{Available: true, Version: strings.TrimSpace(string(output)), Path: path}
}

type systemInstaller struct{}

func (systemInstaller) Install(ctx context.Context, component string, reporter jobs.Reporter) error {
	switch component {
	case "git":
		result, err := (gitsetup.Manager{AutoApprove: true}).Ensure(ctx)
		if err == nil {
			reporter.Event("success", "git.ready", "Git is ready ("+result.Status.Version+")")
		}
		return err
	case "bun":
		result, err := (bun.Manager{AutoApprove: true, Shell: os.Getenv("SHELL")}).Ensure(ctx)
		if err == nil {
			reporter.Event("success", "bun.ready", "Bun is ready ("+result.Status.Version+")")
		}
		return err
	case "couchdb":
		result, err := (couchdb.Manager{AutoApprove: true}).Ensure(ctx)
		if err == nil {
			reporter.Event("success", "couchdb.ready", "CouchDB is ready ("+result.Status.Version+")")
			if result.AdminPassword != "" {
				reporter.Secret(result.AdminPassword)
			}
		}
		return err
	default:
		return errors.New("unsupported dependency")
	}
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, command project.Command, reporter jobs.Reporter) error {
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go reportLines(stdout, "info", reporter, &readers)
	go reportLines(stderr, "error", reporter, &readers)
	waitErr := cmd.Wait()
	readers.Wait()
	return waitErr
}

func reportLines(reader io.Reader, level string, reporter jobs.Reporter, wait *sync.WaitGroup) {
	defer wait.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16*1024), 64*1024)
	for scanner.Scan() {
		reporter.Event(level, "task.output", scanner.Text())
	}
	if scanner.Err() != nil {
		reporter.Event("warn", "task.output_error", "Task output could not be read completely")
	}
}
