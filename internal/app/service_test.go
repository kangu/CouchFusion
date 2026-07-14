package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
)

func TestInitializeRegistersOnlyAfterProvisioningSuccess(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		registry := &registryFake{}
		provisioner := &provisionerFake{initializeErr: errors.New("clone failed")}
		service := newServiceForTest(t, Dependencies{Registry: registry, Provisioner: provisioner})

		started, err := service.InitializeWorkspace(context.Background(), InitializeWorkspaceRequest{Root: t.TempDir(), Name: "Demo"}, "session")
		if err != nil {
			t.Fatal(err)
		}
		waitForJobStatus(t, service, started.ID, jobs.Failed)
		if registry.registerCalls != 0 {
			t.Fatalf("register calls = %d", registry.registerCalls)
		}
	})

	t.Run("success", func(t *testing.T) {
		registry := &registryFake{registerEntry: registry.Entry{ID: "workspace-1", Name: "Demo", Root: "/workspace"}}
		provisioner := &provisionerFake{}
		service := newServiceForTest(t, Dependencies{Registry: registry, Provisioner: provisioner})

		started, err := service.InitializeWorkspace(context.Background(), InitializeWorkspaceRequest{Root: "/workspace", Name: "Demo"}, "session")
		if err != nil {
			t.Fatal(err)
		}
		waitForJobStatus(t, service, started.ID, jobs.Succeeded)
		if provisioner.initializeCalls != 1 || registry.registerCalls != 1 {
			t.Fatalf("initialize calls = %d, register calls = %d", provisioner.initializeCalls, registry.registerCalls)
		}
		if registry.registerRoot != "/workspace" || registry.registerName != "Demo" {
			t.Fatalf("register args = %q, %q", registry.registerRoot, registry.registerName)
		}
	})
}

func TestSiteAndLayerProvisioningRunAsWorkspaceJobs(t *testing.T) {
	registry := &registryFake{entries: []registry.Entry{{ID: "w1", Name: "Demo", Root: "/workspace", Available: true}}}
	provisioner := &provisionerFake{}
	service := newServiceForTest(t, Dependencies{Registry: registry, Provisioner: provisioner})

	site, err := service.CreateSite(context.Background(), "w1", CreateSiteRequest{Name: "shop", Modules: []string{"auth"}}, "browser")
	if err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, service, site.ID, jobs.Succeeded)
	layer, err := service.CreateLayer(context.Background(), "w1", CreateLayerRequest{Name: "content"}, "browser")
	if err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, service, layer.ID, jobs.Succeeded)

	if provisioner.siteCalls != 1 || provisioner.layerCalls != 1 {
		t.Fatalf("site calls = %d, layer calls = %d", provisioner.siteCalls, provisioner.layerCalls)
	}
	if provisioner.siteRoot != "/workspace" || provisioner.layerRoot != "/workspace" {
		t.Fatalf("provision roots = %q, %q", provisioner.siteRoot, provisioner.layerRoot)
	}
}

func TestCLIProvisioningUsesExplicitRootWithoutImplicitRegistration(t *testing.T) {
	registry := &registryFake{}
	provisioner := &provisionerFake{}
	service := newServiceForTest(t, Dependencies{Registry: registry, Provisioner: provisioner})

	site, err := service.CreateSiteAtRoot(context.Background(), "/workspace", CreateSiteRequest{Name: "shop", ConfigPath: "/config.yaml"}, "cli")
	if err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, service, site.ID, jobs.Succeeded)
	layer, err := service.CreateLayerAtRoot(context.Background(), "/workspace", CreateLayerRequest{Name: "content", ConfigPath: "/config.yaml"}, "cli")
	if err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, service, layer.ID, jobs.Succeeded)

	if registry.registerCalls != 0 {
		t.Fatalf("CLI provisioning registered workspace %d times", registry.registerCalls)
	}
	if provisioner.siteRoot != "/workspace" || provisioner.layerRoot != "/workspace" {
		t.Fatalf("provision roots = %q, %q", provisioner.siteRoot, provisioner.layerRoot)
	}
}

func TestHealthInspectionDoesNotInstallDependencies(t *testing.T) {
	health := &healthFake{result: Health{Git: DependencyHealth{Available: false}, Bun: DependencyHealth{Available: true}, CouchDB: DependencyHealth{Available: false}}}
	installer := &installerFake{}
	service := newServiceForTest(t, Dependencies{Health: health, Installer: installer})

	result, err := service.Health(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Bun.Available != true || health.calls != 1 {
		t.Fatalf("health = %#v, calls = %d", result, health.calls)
	}
	if installer.calls != 0 {
		t.Fatalf("health triggered %d install calls", installer.calls)
	}
}

func TestExplicitDependencyInstallRunsAsJobAndHandsOffSecret(t *testing.T) {
	installer := &installerFake{secret: "generated-password"}
	service := newServiceForTest(t, Dependencies{Installer: installer})

	started, err := service.InstallDependency(context.Background(), "couchdb", "browser-a")
	if err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, service, started.ID, jobs.Succeeded)
	secret, err := service.ConsumeJobSecret(started.ID, "browser-a")
	if err != nil || secret != "generated-password" {
		t.Fatalf("secret = %q, error = %v", secret, err)
	}
	if installer.component != "couchdb" || installer.calls != 1 {
		t.Fatalf("installer = %#v", installer)
	}
}

func TestTaskRoutingUsesJobsForFiniteWorkAndProcessesForLongRunningWork(t *testing.T) {
	registry := &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}}
	projects := &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}}
	tasks := &taskFake{
		tasks: []project.Task{
			{ID: "script:build", Kind: "build"},
			{ID: "script:dev", Kind: "dev", LongRunning: true},
		},
		commands: map[string]project.Command{
			"script:build": {Executable: "bun", Args: []string{"run", "build"}, Dir: "/workspace/apps/shop"},
			"script:dev":   {Executable: "bun", Args: []string{"run", "dev"}, Dir: "/workspace/apps/shop"},
		},
	}
	runner := &commandRunnerFake{}
	processManager := &processManagerFake{}
	service := newServiceForTest(t, Dependencies{Registry: registry, Projects: projects, Tasks: tasks, Commands: runner, Processes: processManager})

	finite, err := service.RunTask(context.Background(), "w1", "shop", "script:build", "browser")
	if err != nil {
		t.Fatal(err)
	}
	if finite.Job == nil || finite.Process != nil {
		t.Fatalf("finite result = %#v", finite)
	}
	waitForJobStatus(t, service, finite.Job.ID, jobs.Succeeded)
	if runner.calls != 1 || runner.command.Executable != "bun" {
		t.Fatalf("runner = %#v", runner)
	}

	longRunning, err := service.RunTask(context.Background(), "w1", "shop", "script:dev", "browser")
	if err != nil {
		t.Fatal(err)
	}
	if longRunning.Process == nil || longRunning.Job != nil {
		t.Fatalf("long-running result = %#v", longRunning)
	}
	if processManager.startCalls != 1 || processManager.request.Command.Args[1] != "dev" {
		t.Fatalf("process manager = %#v", processManager)
	}
}

func TestForgetWorkspaceOnlyMutatesRegistry(t *testing.T) {
	registry := &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace"}}}
	service := newServiceForTest(t, Dependencies{Registry: registry})

	if err := service.ForgetWorkspace(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	if registry.forgotID != "w1" || registry.forgetCalls != 1 {
		t.Fatalf("registry = %#v", registry)
	}
}

func TestServiceMapsDomainErrorsToStableCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code ErrorCode
	}{
		{name: "not found", err: registry.ErrNotFound, code: ErrorNotFound},
		{name: "conflict", err: registry.ErrDuplicate, code: ErrorConflict},
		{name: "config", err: config.ErrInvalidConfig, code: ErrorConfig},
		{name: "process", err: processes.ErrAlreadyRunning, code: ErrorProcess},
		{name: "unavailable", err: context.DeadlineExceeded, code: ErrorUnavailable},
		{name: "internal", err: errors.New("unexpected"), code: ErrorInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := MapError(test.err)
			var appErr *Error
			if !errors.As(mapped, &appErr) || appErr.Code != test.code {
				t.Fatalf("MapError(%v) = %#v", test.err, mapped)
			}
		})
	}
}

func newServiceForTest(t *testing.T, dependencies Dependencies) *Service {
	t.Helper()
	counter := 0
	if dependencies.Jobs == nil {
		dependencies.Jobs = jobs.NewManager(jobs.Options{
			NewID: func() (string, error) {
				counter++
				return fmt.Sprintf("job-%d", counter), nil
			},
		})
	}
	if dependencies.Registry == nil {
		dependencies.Registry = &registryFake{}
	}
	if dependencies.Configs == nil {
		dependencies.Configs = &configFake{effective: config.Effective{Config: config.Config{Modules: map[string]config.ModuleConfig{}}}}
	}
	if dependencies.Projects == nil {
		dependencies.Projects = &projectFake{}
	}
	if dependencies.Tasks == nil {
		dependencies.Tasks = &taskFake{}
	}
	if dependencies.Provisioner == nil {
		dependencies.Provisioner = &provisionerFake{}
	}
	if dependencies.Health == nil {
		dependencies.Health = &healthFake{}
	}
	if dependencies.Installer == nil {
		dependencies.Installer = &installerFake{}
	}
	if dependencies.Commands == nil {
		dependencies.Commands = &commandRunnerFake{}
	}
	if dependencies.Processes == nil {
		dependencies.Processes = &processManagerFake{}
	}
	service, err := NewService(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	})
	return service
}

func waitForJobStatus(t *testing.T, service *Service, id string, want jobs.Status) jobs.Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := service.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status == want {
			return snapshot
		}
		time.Sleep(2 * time.Millisecond)
	}
	snapshot, _ := service.Job(id)
	t.Fatalf("job status = %q, want %q; snapshot=%#v", snapshot.Status, want, snapshot)
	return jobs.Snapshot{}
}

type registryFake struct {
	entries       []registry.Entry
	listErr       error
	registerEntry registry.Entry
	registerErr   error
	registerCalls int
	registerRoot  string
	registerName  string
	forgetCalls   int
	forgotID      string
	relinkEntry   registry.Entry
	relinkErr     error
}

func (f *registryFake) List(context.Context) ([]registry.Entry, error) {
	return append([]registry.Entry{}, f.entries...), f.listErr
}

func (f *registryFake) Register(_ context.Context, root, name string) (registry.Entry, error) {
	f.registerCalls++
	f.registerRoot = root
	f.registerName = name
	return f.registerEntry, f.registerErr
}

func (f *registryFake) Forget(_ context.Context, id string) error {
	f.forgetCalls++
	f.forgotID = id
	return nil
}

func (f *registryFake) Relink(context.Context, string, string) (registry.Entry, error) {
	return f.relinkEntry, f.relinkErr
}

func (f *registryFake) Touch(context.Context, string) error { return nil }

type configFake struct {
	effective config.Effective
	err       error
}

func (f *configFake) Load(string, string) (config.Effective, error)        { return f.effective, f.err }
func (f *configFake) WriteWorkspace(string, string, config.Override) error { return f.err }
func (f *configFake) WriteGlobal(string, config.Config) error              { return f.err }

type projectFake struct {
	workspace project.Workspace
	site      project.Site
	err       error
}

func (f *projectFake) Workspace(context.Context, registry.Entry) (project.Workspace, error) {
	return f.workspace, f.err
}

func (f *projectFake) Site(context.Context, string, string) (project.Site, error) {
	return f.site, f.err
}

type taskFake struct {
	tasks    []project.Task
	commands map[string]project.Command
	err      error
}

func (f *taskFake) Tasks(project.Site) []project.Task { return append([]project.Task{}, f.tasks...) }
func (f *taskFake) Resolve(_ project.Site, id string) (project.Command, error) {
	if f.err != nil {
		return project.Command{}, f.err
	}
	command, exists := f.commands[id]
	if !exists {
		return project.Command{}, project.ErrTaskNotFound
	}
	return command, nil
}

type provisionerFake struct {
	initializeErr   error
	initializeCalls int
	siteCalls       int
	layerCalls      int
	siteRoot        string
	layerRoot       string
}

func (f *provisionerFake) Initialize(context.Context, config.Config, InitializeWorkspaceRequest, jobs.Reporter) error {
	f.initializeCalls++
	return f.initializeErr
}

func (f *provisionerFake) CreateSite(_ context.Context, _ config.Config, root string, _ CreateSiteRequest, _ jobs.Reporter) error {
	f.siteCalls++
	f.siteRoot = root
	return nil
}

func (f *provisionerFake) CreateLayer(_ context.Context, _ config.Config, root string, _ CreateLayerRequest, _ jobs.Reporter) error {
	f.layerCalls++
	f.layerRoot = root
	return nil
}

type healthFake struct {
	result Health
	err    error
	calls  int
}

func (f *healthFake) Check(context.Context, string) (Health, error) {
	f.calls++
	return f.result, f.err
}

type installerFake struct {
	component string
	secret    string
	err       error
	calls     int
}

func (f *installerFake) Install(_ context.Context, component string, reporter jobs.Reporter) error {
	f.calls++
	f.component = component
	if f.secret != "" {
		reporter.Secret(f.secret)
	}
	return f.err
}

type commandRunnerFake struct {
	command project.Command
	err     error
	calls   int
}

func (f *commandRunnerFake) Run(_ context.Context, command project.Command, _ jobs.Reporter) error {
	f.calls++
	f.command = command
	return f.err
}

type processManagerFake struct {
	request    processes.StartRequest
	startCalls int
}

func (f *processManagerFake) Start(_ context.Context, request processes.StartRequest) (processes.Snapshot, error) {
	f.startCalls++
	f.request = request
	return processes.Snapshot{ID: "process-1", WorkspaceID: request.WorkspaceID, SiteID: request.SiteID, TaskID: request.TaskID, Status: processes.Running}, nil
}

func (f *processManagerFake) Stop(context.Context, string) error { return nil }
func (f *processManagerFake) Restart(context.Context, string) (processes.Snapshot, error) {
	return processes.Snapshot{}, nil
}
func (f *processManagerFake) Get(string) (processes.Snapshot, error) {
	return processes.Snapshot{}, processes.ErrNotFound
}
func (f *processManagerFake) Subscribe(string, uint64) (<-chan processes.Event, func(), error) {
	return nil, nil, processes.ErrNotFound
}
func (f *processManagerFake) Shutdown(context.Context) error { return nil }
