package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/preview"
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

	site, err := service.CreateSite(context.Background(), "w1", CreateSiteRequest{Name: "shop", Modules: []string{"auth"}, DBLoginPrefix: "ac"}, "browser")
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
	if provisioner.siteRequest.DBLoginPrefix != "ac" {
		t.Fatalf("database login prefix = %q, want ac", provisioner.siteRequest.DBLoginPrefix)
	}
}

func TestCreateSiteValidatesDBLoginPrefixBeforeStartingJob(t *testing.T) {
	for _, prefix := range []string{"", "Aq", "a1", "a", "abc"} {
		t.Run(prefix, func(t *testing.T) {
			registry := &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}}
			provisioner := &provisionerFake{}
			service := newServiceForTest(t, Dependencies{Registry: registry, Provisioner: provisioner})

			_, err := service.CreateSite(context.Background(), "w1", CreateSiteRequest{Name: "shop", DBLoginPrefix: prefix}, "browser")
			var validation *Error
			if !errors.As(err, &validation) || validation.Code != ErrorValidation {
				t.Fatalf("CreateSite() error = %#v, want validation error", err)
			}
			if validation.Fields["dbLoginPrefix"] == "" {
				t.Fatalf("validation fields = %#v, want dbLoginPrefix message", validation.Fields)
			}
			if provisioner.siteCalls != 0 {
				t.Fatalf("provisioner calls = %d, want 0", provisioner.siteCalls)
			}
		})
	}
}

func TestCLIProvisioningUsesExplicitRootWithoutImplicitRegistration(t *testing.T) {
	registry := &registryFake{}
	provisioner := &provisionerFake{}
	service := newServiceForTest(t, Dependencies{Registry: registry, Provisioner: provisioner})

	site, err := service.CreateSiteAtRoot(context.Background(), "/workspace", CreateSiteRequest{Name: "shop", DBLoginPrefix: "ns", ConfigPath: "/config.yaml"}, "cli")
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
	health := &healthFake{result: Health{Git: DependencyHealth{Available: false}, Bun: DependencyHealth{Available: true}, Node: DependencyHealth{Available: true}, CouchDB: DependencyHealth{Available: false}}}
	installer := &installerFake{}
	service := newServiceForTest(t, Dependencies{Health: health, Installer: installer})

	result, err := service.Health(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Bun.Available || !result.Node.Available || health.calls != 1 {
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

func TestComponentsAddsRebuildCapabilityFromCuratedTasks(t *testing.T) {
	components := &componentServiceFake{registry: project.ComponentRegistry{
		Status:     project.ComponentRegistryReady,
		Components: []project.ComponentDefinition{},
	}}
	tasks := &taskFake{tasks: []project.Task{{ID: "script:registry:build", Name: "registry:build", Kind: "script"}}}
	service := newServiceForTest(t, Dependencies{
		Registry:   &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects:   &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Tasks:      tasks,
		Components: components,
	})

	got, err := service.Components(context.Background(), "w1", "shop")

	if err != nil || !got.CanRebuild || components.inspectedRoot != "/workspace/apps/shop" {
		t.Fatalf("Components() = %#v, inspected root = %q, error = %v", got, components.inspectedRoot, err)
	}
}

func TestComponentsRejectsLongRunningRegistryTaskAsRebuildCapability(t *testing.T) {
	service := newServiceForTest(t, Dependencies{
		Registry:   &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects:   &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Tasks:      &taskFake{tasks: []project.Task{{ID: "script:registry:build", LongRunning: true}}},
		Components: &componentServiceFake{registry: project.ComponentRegistry{Status: project.ComponentRegistryMissing, Components: []project.ComponentDefinition{}}},
	})

	got, err := service.Components(context.Background(), "w1", "shop")

	if err != nil || got.CanRebuild {
		t.Fatalf("Components() = %#v, error = %v", got, err)
	}
}

func TestRebuildComponentRegistryRunsOnlyExactCuratedTask(t *testing.T) {
	tasks := &taskFake{
		tasks: []project.Task{{ID: "script:registry:build", Name: "registry:build", Kind: "script"}},
		commands: map[string]project.Command{
			"script:registry:build": {Executable: "bun", Args: []string{"run", "registry:build"}, Dir: "/workspace/apps/shop"},
		},
	}
	runner := &commandRunnerFake{}
	service := newServiceForTest(t, Dependencies{
		Registry: &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects: &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Tasks:    tasks,
		Commands: runner,
	})

	job, err := service.RebuildComponentRegistry(context.Background(), "w1", "shop", "browser")

	if err != nil || job.Type != "component-registry.rebuild" {
		t.Fatalf("RebuildComponentRegistry() = %#v, error = %v", job, err)
	}
	waitForJobStatus(t, service, job.ID, jobs.Succeeded)
	if runner.calls != 1 || !reflect.DeepEqual(runner.command.Args, []string{"run", "registry:build"}) {
		t.Fatalf("runner = %#v", runner)
	}
}

func TestRebuildComponentRegistryRejectsUnavailableOrManagedTask(t *testing.T) {
	for _, tasks := range [][]project.Task{
		{{ID: "script:build"}},
		{{ID: "script:registry:build", LongRunning: true}},
	} {
		service := newServiceForTest(t, Dependencies{
			Registry: &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
			Projects: &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
			Tasks:    &taskFake{tasks: tasks},
		})
		if _, err := service.RebuildComponentRegistry(context.Background(), "w1", "shop", "browser"); err == nil {
			t.Fatalf("RebuildComponentRegistry() accepted tasks %#v", tasks)
		}
	}
}

func TestOpenComponentSourceResolvesRegisteredIDBeforeOpening(t *testing.T) {
	components := &componentServiceFake{source: "/workspace/apps/shop/app/components/content/Hero.vue"}
	opener := &fileOpenerFake{}
	service := newServiceForTest(t, Dependencies{
		Registry:   &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects:   &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Components: components,
		Files:      opener,
	})

	err := service.OpenComponentSource(context.Background(), "w1", "shop", "hero")

	if err != nil || components.resolvedID != "hero" || opener.filename != components.source {
		t.Fatalf("components = %#v, opener = %#v, error = %v", components, opener, err)
	}
}

func TestOpenComponentSourceMapsResolverAndLauncherErrors(t *testing.T) {
	resolver := &componentServiceFake{err: project.ErrComponentNotFound}
	service := newServiceForTest(t, Dependencies{
		Registry:   &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects:   &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Components: resolver,
		Files:      &fileOpenerFake{},
	})
	if err := service.OpenComponentSource(context.Background(), "w1", "shop", "missing"); errorCode(err) != ErrorNotFound {
		t.Fatalf("resolver error = %#v", err)
	}
	resolver.err = project.ErrComponentRegistryInvalid
	if err := service.OpenComponentSource(context.Background(), "w1", "shop", "hero"); errorCode(err) != ErrorValidation {
		t.Fatalf("invalid registry error = %#v", err)
	}

	launcher := &fileOpenerFake{err: errors.New("launcher unavailable")}
	resolver.err = nil
	resolver.source = "/workspace/apps/shop/app/components/content/Hero.vue"
	service = newServiceForTest(t, Dependencies{
		Registry:   &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects:   &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Components: resolver,
		Files:      launcher,
	})
	err := service.OpenComponentSource(context.Background(), "w1", "shop", "hero")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != ErrorProcess || appErr.Message != "Could not open component source" {
		t.Fatalf("launcher error = %#v", err)
	}
}

func TestComponentPreviewHostUsesOnlyCuratedDevTask(t *testing.T) {
	hosts := &previewHostFake{view: preview.View{Status: preview.Starting, ProcessID: "preview-1"}}
	tasks := &taskFake{
		tasks: []project.Task{{ID: "script:dev", Name: "dev", Kind: "dev", LongRunning: true}},
		commands: map[string]project.Command{
			"script:dev": {Executable: "bun", Args: []string{"run", "dev"}, Dir: "/workspace/apps/shop"},
		},
	}
	service := newServiceForTest(t, Dependencies{
		Registry: &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects: &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
		Tasks:    tasks, PreviewHosts: hosts,
	})

	started, err := service.StartComponentPreviewHost(context.Background(), "w1", "shop")
	if err != nil || started.ProcessID != "preview-1" {
		t.Fatalf("StartComponentPreviewHost() = %#v, %v", started, err)
	}
	if hosts.startCalls != 1 || hosts.command.Executable != "bun" || !reflect.DeepEqual(hosts.command.Args, []string{"run", "dev"}) {
		t.Fatalf("preview hosts = %#v", hosts)
	}
	status, err := service.ComponentPreviewHost(context.Background(), "w1", "shop")
	if err != nil || status.Status != preview.Starting || hosts.statusCalls != 1 {
		t.Fatalf("ComponentPreviewHost() = %#v, %v", status, err)
	}
	stopped, err := service.StopComponentPreviewHost(context.Background(), "w1", "shop")
	if err != nil || stopped.Status != preview.Starting || hosts.stopCalls != 1 {
		t.Fatalf("StopComponentPreviewHost() = %#v, %v", stopped, err)
	}
}

func TestComponentPreviewHostRejectsMissingOrFiniteDevTask(t *testing.T) {
	for _, availableTasks := range [][]project.Task{
		{{ID: "script:build"}},
		{{ID: "script:dev", LongRunning: false}},
	} {
		hosts := &previewHostFake{}
		service := newServiceForTest(t, Dependencies{
			Registry: &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
			Projects: &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop"}},
			Tasks:    &taskFake{tasks: availableTasks}, PreviewHosts: hosts,
		})

		_, err := service.StartComponentPreviewHost(context.Background(), "w1", "shop")
		var appError *Error
		if !errors.As(err, &appError) || appError.Code != ErrorValidation {
			t.Fatalf("tasks %#v error = %#v, want validation", availableTasks, err)
		}
		if hosts.startCalls != 0 {
			t.Fatalf("tasks %#v started preview host", availableTasks)
		}
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

func TestRenameWorkspaceDelegatesToRegistry(t *testing.T) {
	registry := &registryFake{renameEntry: registry.Entry{ID: "w1", Name: "Storefront", Root: "/workspace"}}
	service := newServiceForTest(t, Dependencies{Registry: registry})

	updated, err := service.RenameWorkspace(context.Background(), "w1", "Storefront")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Storefront" || registry.renamedID != "w1" || registry.renamedName != "Storefront" {
		t.Fatalf("updated = %#v, registry = %#v", updated, registry)
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
	renameEntry   registry.Entry
	renameErr     error
	renamedID     string
	renamedName   string
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

func (f *registryFake) Rename(_ context.Context, id, name string) (registry.Entry, error) {
	f.renamedID = id
	f.renamedName = name
	return f.renameEntry, f.renameErr
}

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
	siteRequest     CreateSiteRequest
	layerRoot       string
}

func (f *provisionerFake) Initialize(context.Context, config.Config, InitializeWorkspaceRequest, jobs.Reporter) error {
	f.initializeCalls++
	return f.initializeErr
}

func (f *provisionerFake) CreateSite(_ context.Context, _ config.Config, root string, request CreateSiteRequest, _ jobs.Reporter) error {
	f.siteCalls++
	f.siteRoot = root
	f.siteRequest = request
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

type componentServiceFake struct {
	registry      project.ComponentRegistry
	source        string
	err           error
	inspectedRoot string
	resolvedRoot  string
	resolvedID    string
}

func (f *componentServiceFake) Inspect(_ context.Context, root string) project.ComponentRegistry {
	f.inspectedRoot = root
	return f.registry
}

func (f *componentServiceFake) ResolveSource(_ context.Context, root, componentID string) (string, error) {
	f.resolvedRoot = root
	f.resolvedID = componentID
	return f.source, f.err
}

type fileOpenerFake struct {
	filename string
	err      error
}

func (f *fileOpenerFake) Open(filename string) error {
	f.filename = filename
	return f.err
}

func errorCode(err error) ErrorCode {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return ""
}

type processManagerFake struct {
	request    processes.StartRequest
	startCalls int
}

type previewHostFake struct {
	view        preview.View
	err         error
	startCalls  int
	statusCalls int
	stopCalls   int
	command     project.Command
}

func (f *previewHostFake) Start(_ context.Context, _, _ string, command project.Command) (preview.View, error) {
	f.startCalls++
	f.command = command
	return f.view, f.err
}

func (f *previewHostFake) Status(context.Context, string, string) (preview.View, error) {
	f.statusCalls++
	return f.view, f.err
}

func (f *previewHostFake) Stop(context.Context, string, string) (preview.View, error) {
	f.stopCalls++
	return f.view, f.err
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
