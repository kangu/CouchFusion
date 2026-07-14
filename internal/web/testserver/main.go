package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/appserver"
	"github.com/nuxt-apps/couchfusion/internal/agent/conversations"
	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/app"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/preview"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
	webserver "github.com/nuxt-apps/couchfusion/internal/web"
)

const workspaceRoot = "/tmp/couchfusion-e2e-workspace"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var fakeSupervisor *appserver.Supervisor
	if binary := os.Getenv("COUCHFUSION_AGENT_FAKE"); os.Getenv("COUCHFUSION_AGENT_E2E") == "1" && binary != "" {
		fakeSupervisor = appserver.NewSupervisor(appserver.Options{
			Command: func(commandContext context.Context) *exec.Cmd {
				command := exec.CommandContext(commandContext, binary)
				command.Env = append(os.Environ(), "FAKE_AGENT_SCENARIO=component-generation")
				return command
			},
			VersionCommand: func(commandContext context.Context) *exec.Cmd {
				return exec.CommandContext(commandContext, binary, "--version")
			},
		})
		if _, err := fakeSupervisor.EnsureReady(ctx); err != nil {
			panic(err)
		}
		defer func() { _ = fakeSupervisor.Shutdown(context.Background()) }()
	}
	options := webserver.Options{Port: 4178, Application: newFixtureApplication(), Random: zeroReader{}}
	if rawTarget := os.Getenv("COUCHFUSION_TEST_UI_PROXY"); rawTarget != "" {
		target, err := url.Parse(rawTarget)
		if err != nil {
			panic(err)
		}
		frontend, err := webserver.NewDevelopmentProxy(target, zeroReader{})
		if err != nil {
			panic(err)
		}
		options.FrontendHandler = frontend
	}
	server, err := webserver.NewServer(options)
	if err != nil {
		panic(err)
	}
	if _, err := server.Start(ctx); err != nil {
		panic(err)
	}
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, context.Canceled) {
		panic(err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) { clear(value); return len(value), nil }

type fixtureApplication struct {
	mu                sync.Mutex
	registered        bool
	entry             registry.Entry
	jobs              map[string]jobs.Snapshot
	process           processes.Snapshot
	openedComponentID string
	previewHost       preview.View
	agentConversation model.Conversation
	agentScenario     bool
	recoverOnList     bool
}

func newFixtureApplication() *fixtureApplication {
	now := time.Now().UTC()
	agentScenario := os.Getenv("COUCHFUSION_AGENT_E2E") == "1"
	return &fixtureApplication{
		entry:             registry.Entry{ID: "workspace-e2e", Name: "E2E Workspace", Root: workspaceRoot, Available: true, CreatedAt: now, LastOpenedAt: now},
		jobs:              map[string]jobs.Snapshot{},
		agentConversation: model.Conversation{ID: "conversation-e2e", WorkspaceID: "workspace-e2e", SiteID: "shop", Title: "Fixture generation", Status: model.ConversationReady, Registry: model.RegistryResult{State: model.RegistrySucceeded}, BindingValid: true, CreatedAt: now, UpdatedAt: now},
		agentScenario:     agentScenario,
	}
}

func (f *fixtureApplication) ListWorkspaces(context.Context) ([]registry.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.registered {
		return []registry.Entry{}, nil
	}
	return []registry.Entry{f.entry}, nil
}
func (f *fixtureApplication) RegisterWorkspace(_ context.Context, root, name string) (registry.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = true
	f.entry.Root = root
	if name != "" {
		f.entry.Name = name
	}
	return f.entry, nil
}
func (f *fixtureApplication) InitializeWorkspace(context.Context, app.InitializeWorkspaceRequest, string) (jobs.Snapshot, error) {
	return f.newJob("workspace.initialize", ""), nil
}
func (f *fixtureApplication) Workspace(context.Context, string) (project.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.registered {
		return project.Workspace{}, project.ErrNotFound
	}
	return project.Workspace{ID: f.entry.ID, Name: f.entry.Name, Root: f.entry.Root, Available: true, Sites: []project.Site{fixtureSite()}, Layers: []project.Layer{{ID: "content", Name: "Content", Root: f.entry.Root + "/layers/content"}}, Warnings: []project.Warning{}}, nil
}
func (f *fixtureApplication) RenameWorkspace(_ context.Context, _ string, name string) (registry.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entry.Name = name
	return f.entry, nil
}
func (f *fixtureApplication) ForgetWorkspace(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = false
	return nil
}
func (f *fixtureApplication) RelinkWorkspace(_ context.Context, _ string, root string) (registry.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entry.Root = root
	return f.entry, nil
}
func (f *fixtureApplication) EffectiveConfig(context.Context, string) (config.Effective, error) {
	return fixtureConfig("global"), nil
}
func (f *fixtureApplication) WriteWorkspaceConfig(context.Context, string, config.Override) (config.Effective, error) {
	return fixtureConfig("workspace"), nil
}
func (f *fixtureApplication) Health(context.Context, string) (app.Health, error) {
	ready := app.DependencyHealth{Available: true, Version: "fixture"}
	return app.Health{Git: ready, Bun: ready, Node: ready, CouchDB: ready}, nil
}
func (f *fixtureApplication) InstallDependency(context.Context, string, string) (jobs.Snapshot, error) {
	return f.newJob("dependency.install", ""), nil
}
func (f *fixtureApplication) CreateSite(context.Context, string, app.CreateSiteRequest, string) (jobs.Snapshot, error) {
	return f.newJob("site.create", "catalog"), nil
}
func (f *fixtureApplication) CreateLayer(context.Context, string, app.CreateLayerRequest, string) (jobs.Snapshot, error) {
	return f.newJob("layer.create", ""), nil
}
func (f *fixtureApplication) Site(context.Context, string, string) (project.Site, error) {
	return fixtureSite(), nil
}
func (f *fixtureApplication) Tasks(context.Context, string, string) ([]project.Task, error) {
	return []project.Task{{ID: "script:build", Name: "Build", Kind: "script"}, {ID: "script:dev", Name: "Dev server", Kind: "script", LongRunning: true}, {ID: "script:registry:build", Name: "registry:build", Kind: "script"}}, nil
}
func (f *fixtureApplication) Components(context.Context, string, string) (app.ComponentRegistryView, error) {
	return app.ComponentRegistryView{
		ComponentRegistry: project.ComponentRegistry{
			Status: project.ComponentRegistryReady,
			Components: []project.ComponentDefinition{
				{ID: "hero", Label: "Hero", Description: "Lead block", Source: "app/components/content/Hero.vue", PropCount: 2, AllowChildren: true},
				{ID: "parallax", Label: "Parallax", Description: "Scrolling media block", Source: "app/components/content/Parallax.vue", PropCount: 4},
			},
		},
		CanRebuild: true,
	}, nil
}
func (f *fixtureApplication) RebuildComponentRegistry(context.Context, string, string, string) (jobs.Snapshot, error) {
	return f.newJob("component-registry.rebuild", "shop"), nil
}
func (f *fixtureApplication) OpenComponentSource(_ context.Context, _, _, componentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openedComponentID = componentID
	return nil
}
func (f *fixtureApplication) ComponentPreviewHost(context.Context, string, string) (preview.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.previewHost.Status == "" {
		return preview.View{Status: preview.Stopped}, nil
	}
	return f.previewHost, nil
}
func (f *fixtureApplication) StartComponentPreviewHost(context.Context, string, string) (preview.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.previewHost = preview.View{
		Status: preview.Ready, ProcessID: "component-preview-e2e",
		URL:       "http://127.0.0.1:4311/__couchfusion/components/preview?token=fixture",
		StartedAt: &now,
	}
	return f.previewHost, nil
}
func (f *fixtureApplication) StopComponentPreviewHost(context.Context, string, string) (preview.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previewHost = preview.View{Status: preview.Stopped}
	return f.previewHost, nil
}
func (f *fixtureApplication) RunTask(context.Context, string, string, string, string) (app.TaskRunResult, error) {
	job := f.newJob("task.run", "shop")
	return app.TaskRunResult{Job: &job}, nil
}
func (f *fixtureApplication) StartProcess(context.Context, string, string, string) (processes.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.process = processes.Snapshot{ID: "process-e2e", WorkspaceID: f.entry.ID, SiteID: "shop", TaskID: "script:dev", Status: processes.Running, PID: 4310, StartedAt: now, URLs: []string{"http://127.0.0.1:4310/"}, Events: []processes.Event{{ID: 1, Time: now, Stream: "stdout", Code: "process.ready", Text: "Local: http://127.0.0.1:4310/"}}}
	return f.process, nil
}
func (f *fixtureApplication) Process(string) (processes.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.process, nil
}
func (f *fixtureApplication) StopProcess(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.process.Status = processes.Stopped
	f.process.CompletedAt = time.Now().UTC()
	return nil
}
func (f *fixtureApplication) RestartProcess(context.Context, string) (processes.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.process.Status = processes.Running
	return f.process, nil
}
func (f *fixtureApplication) SubscribeProcess(string, uint64) (<-chan processes.Event, func(), error) {
	stream := make(chan processes.Event, 1)
	stream <- processes.Event{ID: 2, Time: time.Now().UTC(), Stream: "stdout", Code: "process.snapshot", Text: "ready"}
	return stream, func() {}, nil
}
func (f *fixtureApplication) Job(id string) (jobs.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if !ok {
		return jobs.Snapshot{}, jobs.ErrNotFound
	}
	return job, nil
}
func (f *fixtureApplication) CancelJob(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[id]
	if !ok {
		return jobs.ErrNotFound
	}
	job.Status = jobs.Cancelled
	f.jobs[id] = job
	return nil
}
func (f *fixtureApplication) SubscribeJob(id string, _ uint64) (<-chan jobs.Event, func(), error) {
	stream := make(chan jobs.Event, 1)
	stream <- jobs.Event{ID: 2, Time: time.Now().UTC(), Level: "info", Code: "job.snapshot", Text: id + " complete"}
	return stream, func() {}, nil
}
func (f *fixtureApplication) ConsumeJobSecret(string, string) (string, error) {
	return "", jobs.ErrSecretUnavailable
}

func (f *fixtureApplication) AgentConversationList(context.Context, string, string) (app.AgentConversationCollection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agentScenario && f.recoverOnList {
		f.recoverOnList = false
		f.agentConversation.Status = model.ConversationInterrupted
		f.agentConversation.ActiveTurn = nil
		f.agentConversation.PendingInteraction = nil
		f.agentConversation.LastTurn = &model.LastTurn{Status: model.TurnInterrupted, Summary: "Interrupted during fixture restart recovery"}
		f.agentConversation.Sequence++
		f.agentConversation.UpdatedAt = time.Now().UTC()
	}
	conversations := []model.Conversation{}
	if !f.agentScenario || f.agentConversation.Sequence > 0 {
		conversations = append(conversations, f.agentConversation)
	}
	return app.AgentConversationCollection{Readiness: model.AgentReadiness{Status: model.AgentReady, Available: true, Version: "0.144.4", AccountType: "chatgpt"}, Conversations: conversations}, nil
}
func (f *fixtureApplication) AgentConversation(context.Context, string) (model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agentConversation, nil
}
func (f *fixtureApplication) CreateAgentConversation(_ context.Context, _, _ string, _ app.AgentPromptRequest) (model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.agentScenario {
		return f.agentConversation, nil
	}
	now := time.Now().UTC()
	f.agentConversation = model.Conversation{
		ID: "conversation-agent-e2e", WorkspaceID: "workspace-e2e", SiteID: "shop", Title: "Editorial testimonial rail",
		Status: model.ConversationWaiting, ActiveTurn: &model.ConversationTurn{ID: "turn-1", Status: model.TurnActive, StartedAt: now},
		Items: []model.ConversationItem{{ID: "user-1", TurnID: "turn-1", Type: "userMessage", Status: "completed", Text: "Create an editorial testimonial rail with useful defaults"}},
		PendingInteraction: &model.Interaction{RequestID: "question-1", Kind: model.InteractionQuestion, Questions: []model.Question{{
			ID: "tone", Header: "Visual tone", Question: "Which visual tone?", Options: []model.InteractionOption{{Label: "Quiet"}, {Label: "Bold"}},
		}}},
		Registry: model.RegistryResult{State: model.RegistryNotNeeded}, Sequence: 1, BindingValid: true, CreatedAt: now, UpdatedAt: now,
	}
	return f.agentConversation, nil
}
func (f *fixtureApplication) StartAgentTurn(context.Context, string, app.AgentPromptRequest) (model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.agentConversation.Status = model.ConversationWaiting
	f.agentConversation.ActiveTurn = &model.ConversationTurn{ID: "turn-2", Status: model.TurnActive, StartedAt: now}
	f.agentConversation.PendingInteraction = &model.Interaction{
		RequestID: "follow-up-file", Kind: model.InteractionApproval, Action: model.ApprovalFileChange,
		Summary: "Apply component file changes", AffectedPaths: []string{"app/components/content/Hero.vue"},
	}
	f.agentConversation.Sequence++
	f.agentConversation.UpdatedAt = now
	f.recoverOnList = true
	return f.agentConversation, nil
}
func (f *fixtureApplication) AnswerAgentQuestion(context.Context, string, string, map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentConversation.PendingInteraction = &model.Interaction{
		RequestID: "file-1", Kind: model.InteractionApproval, Action: model.ApprovalFileChange,
		Summary: "Apply component file changes", AffectedPaths: []string{"app/components/content/Hero.vue"},
	}
	f.agentConversation.Sequence++
	f.agentConversation.UpdatedAt = time.Now().UTC()
	return nil
}
func (f *fixtureApplication) ResolveAgentApproval(context.Context, string, string, conversations.ApprovalDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agentConversation.PendingInteraction != nil && f.agentConversation.PendingInteraction.RequestID == "file-1" {
		f.agentConversation.PendingInteraction = &model.Interaction{
			RequestID: "dependency-1", Kind: model.InteractionConsent, ConsentKind: model.ConsentDependencyChange,
			Summary: "Add the carousel helper dependency",
		}
		f.agentConversation.Sequence++
		f.agentConversation.UpdatedAt = time.Now().UTC()
		return nil
	}
	f.agentConversation.Status = model.ConversationReady
	f.agentConversation.ActiveTurn = nil
	f.agentConversation.PendingInteraction = nil
	f.agentConversation.LastTurn = &model.LastTurn{Status: model.TurnCompleted, Summary: "Generated the editorial testimonial rail"}
	f.agentConversation.Registry = model.RegistryResult{State: model.RegistrySucceeded, GeneratedComponentID: "hero"}
	f.agentConversation.GeneratedComponentID = "hero"
	f.agentConversation.ChangedFiles = []model.ChangedFile{
		{Path: "pre-existing.txt", Kind: model.ChangeModified, BaselineDirty: true},
		{Path: "app/components/content/Hero.vue", Kind: model.ChangeAdded},
	}
	f.agentConversation.Sequence++
	f.agentConversation.UpdatedAt = time.Now().UTC()
	return nil
}
func (f *fixtureApplication) GrantAgentConsent(context.Context, string, string, model.ConsentKind, bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentConversation.PendingInteraction = &model.Interaction{
		RequestID: "dependency-1", Kind: model.InteractionApproval, Action: model.ApprovalCommand,
		Summary: "Run bun add carousel-helper",
	}
	f.agentConversation.Sequence++
	f.agentConversation.UpdatedAt = time.Now().UTC()
	return nil
}
func (f *fixtureApplication) InterruptAgentConversation(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentConversation.Status = model.ConversationInterrupted
	f.agentConversation.ActiveTurn = nil
	f.agentConversation.PendingInteraction = nil
	f.agentConversation.Sequence++
	return nil
}
func (f *fixtureApplication) SubscribeAgentConversation(context.Context, string, uint64) (<-chan model.Event, error) {
	stream := make(chan model.Event)
	return stream, nil
}

func (f *fixtureApplication) newJob(kind, siteID string) jobs.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	id := kind + "-e2e"
	job := jobs.Snapshot{ID: id, Type: kind, WorkspaceID: f.entry.ID, SiteID: siteID, Status: jobs.Succeeded, CreatedAt: now, StartedAt: now, CompletedAt: now, Events: []jobs.Event{{ID: 1, Time: now, Level: "success", Code: kind + ".complete", Text: "Fixture operation complete"}}}
	f.jobs[id] = job
	return job
}
func fixtureSite() project.Site {
	return project.Site{ID: "shop", Name: "Shop", Root: workspaceRoot + "/apps/shop", PackageName: "@fixture/shop", PackageManager: "bun", Modules: []string{"content"}, Scripts: map[string]string{"build": "nuxt build", "dev": "nuxt dev", "registry:build": "bun run ../../cli-content/generate-component-registry.mjs"}, Dependencies: map[string]string{"nuxt": "latest"}, DevDependencies: map[string]string{}, DependenciesInstalled: true, Lockfiles: []string{"bun.lock"}, Warnings: []project.Warning{}}
}
func fixtureConfig(source config.Source) config.Effective {
	return config.Effective{Config: config.Config{Repos: map[string]config.RepoConfig{}, Modules: map[string]config.ModuleConfig{}, Workspace: config.WorkspaceConfig{DefaultRoot: workspaceRoot + "/apps"}, Prompts: config.PromptConfig{DefaultLayerSelection: []string{"content"}}}, Sources: config.SourceMap{"workspace.defaultRoot": source}}
}
