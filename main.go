package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/nuxt-apps/couchfusion/internal/agent/appserver"
	"github.com/nuxt-apps/couchfusion/internal/agent/conversations"
	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/prompt"
	agentstore "github.com/nuxt-apps/couchfusion/internal/agent/store"
	"github.com/nuxt-apps/couchfusion/internal/app"
	"github.com/nuxt-apps/couchfusion/internal/bun"
	"github.com/nuxt-apps/couchfusion/internal/checks"
	"github.com/nuxt-apps/couchfusion/internal/cli"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/couchdb"
	"github.com/nuxt-apps/couchfusion/internal/devui"
	"github.com/nuxt-apps/couchfusion/internal/gitsetup"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/logging"
	"github.com/nuxt-apps/couchfusion/internal/mutationlease"
	"github.com/nuxt-apps/couchfusion/internal/onboarding"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
	"github.com/nuxt-apps/couchfusion/internal/ui"
	webserver "github.com/nuxt-apps/couchfusion/internal/web"
	"github.com/nuxt-apps/couchfusion/internal/workspace"
)

const version = cli.Version

func main() {
	os.Exit(runMain())
}

func runMain() int {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	service, err := app.NewDefaultService("")
	if err != nil {
		logging.Errorf("%v", err)
		return 1
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	}()

	streams := cli.IO{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	}
	err = cli.Run(ctx, os.Args[1:], streams, cli.Commands{
		Init: func(ctx context.Context, args []string) error {
			return runInit(ctx, service, args)
		},
		New: func(ctx context.Context, args []string) error {
			return runNew(ctx, service, args)
		},
		CreateLayer: func(ctx context.Context, args []string) error {
			return runCreateLayer(ctx, service, args)
		},
		UI: func(ctx context.Context, args []string) error {
			return cli.RunUI(ctx, args, streams, cli.UIOptions{NewServer: func(listener net.Listener, configPath string, development bool) (cli.UIServer, cli.UIApplication, error) {
				uiApplication, err := newUIService(configPath)
				if err != nil {
					return nil, nil, err
				}
				runtime := uiRuntime{application: uiApplication}
				serverOptions := webserver.Options{Listener: listener, Application: uiApplication}
				if development {
					root, err := os.Getwd()
					if err != nil {
						return nil, nil, errors.Join(fmt.Errorf("resolve CouchFusion source checkout: %w", err), shutdownUIRuntime(runtime))
					}
					process, err := devui.Start(ctx, devui.Options{Root: root, Out: streams.Out, Err: streams.Err})
					if err != nil {
						return nil, nil, errors.Join(err, shutdownUIRuntime(runtime))
					}
					runtime.development = process
					proxy, err := webserver.NewDevelopmentProxy(process.URL(), nil)
					if err != nil {
						return nil, nil, errors.Join(err, shutdownUIRuntime(runtime))
					}
					serverOptions.FrontendHandler = proxy
				}
				server, err := webserver.NewServer(serverOptions)
				if err != nil {
					return nil, nil, errors.Join(err, shutdownUIRuntime(runtime))
				}
				return server, runtime, nil
			}})
		},
	})
	if err == nil {
		return 0
	}

	logging.Errorf("%v", err)
	var exitErr cli.ExitError
	if errors.As(err, &exitErr) && exitErr.Code == 2 {
		printUsage()
	}
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}

func newUIService(configPath string) (*app.Service, error) {
	leases := mutationlease.NewManager()
	tasks := project.Catalog{}
	commands := app.NewCommandRunner()
	registryStore, err := registry.DefaultStore()
	if err != nil {
		return nil, err
	}
	workspaceRegistry := &registry.Service{Store: registryStore}
	onboardingStore, err := onboarding.DefaultStore()
	if err != nil {
		return nil, err
	}
	dependencies := app.Dependencies{
		GlobalConfigPath: configPath,
		Registry:         workspaceRegistry,
		Tasks:            tasks,
		Commands:         commands,
		Jobs:             jobs.NewManager(jobs.Options{Leases: leases}),
		Onboarding:       onboardingStore,
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return app.NewService(dependencies)
	}
	skill, err := prompt.MaterializeSkill(filepath.Join(home, ".couchfusion", "agent"))
	if err != nil {
		return app.NewService(dependencies)
	}
	credentials, err := couchdb.DefaultStore()
	if err != nil {
		return app.NewService(dependencies)
	}
	credentialFallbacks, err := registeredCouchDBCredentialStores(context.Background(), workspaceRegistry)
	if err != nil {
		return app.NewService(dependencies)
	}
	supervisor := appserver.NewSupervisor(appserver.Options{SkillRoot: skill.Root, Version: version})
	repository := agentstore.NewCouchDBRepository(agentstore.CouchDBOptions{
		Credentials: credentials, CredentialFallbacks: credentialFallbacks,
	})
	manager := conversations.NewManager(conversations.Options{
		Repository: repository,
		Leases:     leases,
		Skill:      skill,
		Client: func(ctx context.Context) (conversations.RPCClient, error) {
			return supervisor.Client(ctx)
		},
		Events:            supervisor.Events(),
		Requests:          supervisor.Requests(),
		RuntimeGeneration: supervisor.Generation,
		ShutdownClient:    supervisor.Shutdown,
		Completion:        app.NewRegistryCompletionHook(tasks, commands),
		Readiness: func(ctx context.Context) model.AgentReadiness {
			if err := repository.Ensure(ctx); err != nil {
				return model.AgentReadiness{Status: model.AgentLaunchFailed, Problem: "CouchFusion control database is unavailable"}
			}
			readiness, _ := supervisor.EnsureReady(ctx)
			return readiness
		},
	})
	dependencies.AgentConversations = manager
	service, err := app.NewService(dependencies)
	if err != nil {
		_ = manager.Shutdown(context.Background())
		return nil, err
	}
	return service, nil
}

type workspaceRegistryLister interface {
	List(context.Context) ([]registry.Entry, error)
}

func registeredCouchDBCredentialStores(ctx context.Context, workspaces workspaceRegistryLister) ([]couchdb.Store, error) {
	entries, err := workspaces.List(ctx)
	if err != nil {
		return nil, err
	}
	stores := []couchdb.Store{}
	inspector := project.Inspector{}
	for _, entry := range entries {
		if !entry.Available {
			continue
		}
		workspace, inspectErr := inspector.Workspace(ctx, entry)
		if inspectErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		for _, site := range workspace.Sites {
			path := filepath.Join(site.Root, ".env")
			info, statErr := os.Stat(path)
			if statErr == nil && info.Mode().IsRegular() {
				stores = append(stores, couchdb.Store{Path: path})
			}
		}
	}
	return stores, nil
}

func printUsage() {
	fmt.Println("couchfusion " + version)
	fmt.Println("Usage:")
	fmt.Println("  couchfusion init [--config path] [--path dir] [--layers-branch name] [--force] [--yes]")
	fmt.Println("  couchfusion new [--config path] [--name app] [--modules m1,m2] [--db-login-prefix ab] [--branch name] [--force] [--yes]")
	fmt.Println("  couchfusion create_layer [--config path] [--name layer] [--branch name] [--force] [--yes]")
	fmt.Println("  couchfusion ui [--port 0] [--no-open] [--config path] [--dev]")
}

func runInit(ctx context.Context, service *app.Service, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "Path to config file")
	targetPath := fs.String("path", ".", "Target directory to initialize")
	layerBranch := fs.String("layers-branch", "", "Override branch for layers clone")
	force := fs.Bool("force", false, "Allow reinitialization when directories exist")
	yes := addYesFlags(fs)
	if err := fs.Parse(args); err != nil {
		return cli.ExitError{Code: 2, Err: err}
	}

	cfg, usedDefaultConfig, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if usedDefaultConfig {
		logging.Warnf("No ~/.couchfusion/config.yaml found; using embedded default configuration.")
	}

	if err := ensureGit(ctx, *yes); err != nil {
		return fmt.Errorf("git preflight failed: %w", err)
	}

	if err := ensureBun(ctx, *yes); err != nil {
		return fmt.Errorf("bun preflight failed: %w", err)
	}

	if err := ensureCouchDB(ctx, *yes); err != nil {
		return fmt.Errorf("couchdb preflight failed: %w", err)
	}

	warnings := checks.Run(ctx)
	for _, w := range warnings {
		logging.Warnf(w)
	}

	if workspace.ShouldUseTUI() {
		target, err := workspace.RunInitTUIWithOperation(ctx, cfg, *targetPath, *layerBranch, *force, func(ctx context.Context, root, branch string, force bool, logs *ui.LogBuffer) error {
			started, err := service.InitializeWorkspace(ctx, app.InitializeWorkspaceRequest{Root: root, LayerBranch: branch, Force: force, ConfigPath: *configPath}, "cli")
			if err != nil {
				return err
			}
			return waitForJobWithEvents(ctx, service, started.ID, func(event jobs.Event) {
				logJobEvent(logs, event)
			})
		})
		if err != nil {
			if errors.Is(err, workspace.ErrAborted) {
				logging.Warnf("init cancelled by user")
				return nil
			}
			return fmt.Errorf("init failed: %w", err)
		}
		logging.Infof("Initialization complete at %s", target)
		return nil
	}

	started, err := service.InitializeWorkspace(ctx, app.InitializeWorkspaceRequest{
		Root: *targetPath, LayerBranch: *layerBranch, Force: *force, ConfigPath: *configPath,
	}, "cli")
	if err != nil {
		return fmt.Errorf("init failed: %w", err)
	}
	if err := waitForJob(ctx, service, started.ID); err != nil {
		return fmt.Errorf("init failed: %w", err)
	}

	logging.Infof("Initialization complete.")
	return nil
}

func runNew(ctx context.Context, service *app.Service, args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "Path to config file")
	name := fs.String("name", "", "Name of the new app")
	modules := fs.String("modules", "", "Comma-separated module list")
	dbLoginPrefix := fs.String("db-login-prefix", "", "Two lowercase letters used for database login names")
	branch := fs.String("branch", "", "Override starter branch")
	force := fs.Bool("force", false, "Allow overwriting empty existing directories")
	yes := addYesFlags(fs)
	if err := fs.Parse(args); err != nil {
		return cli.ExitError{Code: 2, Err: err}
	}

	if *name == "" && len(fs.Args()) > 0 {
		*name = fs.Args()[0]
	}

	if err := workspace.EnsureCurrentWorkspace(); err != nil {
		return fmt.Errorf("workspace validation failed: %w", err)
	}

	cfg, usedDefaultConfig, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if usedDefaultConfig {
		logging.Warnf("No ~/.couchfusion/config.yaml found; using embedded default configuration.")
	}

	if err := ensureGit(ctx, *yes); err != nil {
		return fmt.Errorf("git preflight failed: %w", err)
	}

	if err := ensureBun(ctx, *yes); err != nil {
		return fmt.Errorf("bun preflight failed: %w", err)
	}

	if err := ensureCouchDB(ctx, *yes); err != nil {
		return fmt.Errorf("couchdb preflight failed: %w", err)
	}

	warnings := checks.Run(ctx)
	for _, w := range warnings {
		logging.Warnf(w)
	}

	if workspace.ShouldUseTUI() {
		appName, selectedModules, err := workspace.RunNewTUIWithOperation(ctx, cfg, *name, *modules, *dbLoginPrefix, *branch, *force, func(ctx context.Context, name string, modules []string, branch string, force bool, dbLoginPrefix, username, password string, logs *ui.LogBuffer) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			started, err := service.CreateSiteAtRoot(ctx, root, app.CreateSiteRequest{Name: name, Modules: modules, DBLoginPrefix: dbLoginPrefix, Branch: branch, Force: force, Username: username, Password: password, ConfigPath: *configPath}, "cli")
			if err != nil {
				return err
			}
			return waitForJobWithEvents(ctx, service, started.ID, func(event jobs.Event) {
				logJobEvent(logs, event)
			})
		})
		if err != nil {
			if errors.Is(err, workspace.ErrAborted) {
				logging.Warnf("new cancelled by user")
				return nil
			}
			return fmt.Errorf("new failed: %w", err)
		}

		logging.Infof("App '%s' created with modules: %s", appName, strings.Join(selectedModules, ", "))
		return nil
	}

	appName, selectedModules, err := workspace.ResolveAppCreationInputs(cfg, *name, *modules)
	if err != nil {
		return fmt.Errorf("input error: %w", err)
	}
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine workspace root: %w", err)
	}
	started, err := service.CreateSiteAtRoot(ctx, root, app.CreateSiteRequest{
		Name: appName, Modules: selectedModules, DBLoginPrefix: *dbLoginPrefix, Branch: *branch, Force: *force, ConfigPath: *configPath,
	}, "cli")
	if err != nil {
		return fmt.Errorf("new failed: %w", err)
	}
	if err := waitForJob(ctx, service, started.ID); err != nil {
		return fmt.Errorf("new failed: %w", err)
	}

	logging.Infof("App '%s' created with modules: %s", appName, strings.Join(selectedModules, ", "))
	return nil
}

func runCreateLayer(ctx context.Context, service *app.Service, args []string) error {
	fs := flag.NewFlagSet("create_layer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "Path to config file")
	name := fs.String("name", "", "Name of the new layer")
	branch := fs.String("branch", "", "Override starter branch")
	force := fs.Bool("force", false, "Allow overwriting empty existing directories")
	yes := addYesFlags(fs)
	if err := fs.Parse(args); err != nil {
		return cli.ExitError{Code: 2, Err: err}
	}

	cfg, usedDefaultConfig, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if usedDefaultConfig {
		logging.Warnf("No ~/.couchfusion/config.yaml found; using embedded default configuration.")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if err := ensureGit(ctx, *yes); err != nil {
		return fmt.Errorf("git preflight failed: %w", err)
	}

	if err := ensureBun(ctx, *yes); err != nil {
		return fmt.Errorf("bun preflight failed: %w", err)
	}

	if err := ensureCouchDB(ctx, *yes); err != nil {
		return fmt.Errorf("couchdb preflight failed: %w", err)
	}

	warnings := checks.Run(ctx)
	for _, w := range warnings {
		logging.Warnf(w)
	}

	if workspace.ShouldUseTUI() {
		layerName, err := workspace.RunCreateLayerTUIWithOperation(ctx, cfg, *name, *branch, *force, func(ctx context.Context, name, branch string, force bool, logs *ui.LogBuffer) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			started, err := service.CreateLayerAtRoot(ctx, root, app.CreateLayerRequest{Name: name, Branch: branch, Force: force, ConfigPath: *configPath}, "cli")
			if err != nil {
				return err
			}
			return waitForJobWithEvents(ctx, service, started.ID, func(event jobs.Event) {
				logJobEvent(logs, event)
			})
		})
		if err != nil {
			if errors.Is(err, workspace.ErrAborted) {
				logging.Warnf("create_layer cancelled by user")
				return nil
			}
			return fmt.Errorf("create_layer failed: %w", err)
		}
		logging.Infof("Layer '%s' created.", layerName)
		return nil
	}

	layerName, err := workspace.ResolveLayerName(*name)
	if err != nil {
		return fmt.Errorf("input error: %w", err)
	}
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine workspace root: %w", err)
	}
	started, err := service.CreateLayerAtRoot(ctx, root, app.CreateLayerRequest{
		Name: layerName, Branch: *branch, Force: *force, ConfigPath: *configPath,
	}, "cli")
	if err != nil {
		return fmt.Errorf("create_layer failed: %w", err)
	}
	if err := waitForJob(ctx, service, started.ID); err != nil {
		return fmt.Errorf("create_layer failed: %w", err)
	}

	logging.Infof("Layer '%s' created.", layerName)
	return nil
}

func waitForJob(ctx context.Context, service *app.Service, id string) error {
	return waitForJobWithEvents(ctx, service, id, func(event jobs.Event) {
		switch event.Level {
		case "error":
			logging.Errorf("%s", event.Text)
		case "warn":
			logging.Warnf("%s", event.Text)
		default:
			logging.Infof("%s", event.Text)
		}
	})
}

func waitForJobWithEvents(ctx context.Context, service *app.Service, id string, onEvent func(jobs.Event)) error {
	events, unsubscribe, err := service.SubscribeJob(id, 0)
	if err != nil {
		return err
	}
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			_ = service.CancelJob(id)
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				snapshot, err := service.Job(id)
				if err != nil {
					return err
				}
				switch snapshot.Status {
				case jobs.Succeeded:
					return nil
				case jobs.Cancelled:
					return context.Canceled
				default:
					if snapshot.Error != nil {
						return errors.New(snapshot.Error.Message)
					}
					return errors.New("job failed")
				}
			}
			if onEvent != nil {
				onEvent(event)
			}
		}
	}
}

func logJobEvent(logs *ui.LogBuffer, event jobs.Event) {
	switch event.Level {
	case "error":
		logs.Errorf("%s", event.Text)
	case "warn":
		logs.Warnf("%s", event.Text)
	case "success":
		logs.Successf("%s", event.Text)
	default:
		logs.Infof("%s", event.Text)
	}
}

func init() {
	logging.SetVersion(version)
	os.Setenv("COUCHFUSION_VERSION", version)
}

func addYesFlags(fs *flag.FlagSet) *bool {
	yes := fs.Bool("yes", false, "Approve non-interactive setup prompts")
	fs.BoolVar(yes, "y", false, "Approve non-interactive setup prompts")
	return yes
}

func ensureGit(ctx context.Context, yes bool) error {
	manager := gitsetup.Manager{
		Interactive: isInteractiveTerminal(),
		AutoApprove: yes,
		PromptInstall: func() (bool, error) {
			return confirm("Git is not available in PATH. Install and configure it now?")
		},
	}
	result, err := manager.Ensure(ctx)
	if err != nil {
		return err
	}
	versionLabel := result.Status.Version
	if versionLabel == "" {
		versionLabel = "unknown"
	}
	if result.Installed {
		logging.Infof("Installed Git %s", versionLabel)
		return nil
	}
	logging.Infof("Detected Git %s", versionLabel)
	return nil
}

func ensureBun(ctx context.Context, yes bool) error {
	manager := bun.Manager{
		Interactive: isInteractiveTerminal(),
		AutoApprove: yes,
		Shell:       os.Getenv("SHELL"),
		PromptInstall: func() (bool, error) {
			return confirm("Bun is not available in PATH. Install and configure it now?")
		},
	}
	result, err := manager.Ensure(ctx)
	if err != nil {
		return err
	}
	if result.Installed {
		logging.Infof("Installed Bun %s and refreshed PATH with %s", result.Status.Version, result.BunBin)
		return nil
	}
	versionLabel := result.Status.Version
	if versionLabel == "" {
		versionLabel = "unknown"
	}
	logging.Infof("Detected Bun %s", versionLabel)
	return nil
}

func ensureCouchDB(ctx context.Context, yes bool) error {
	manager := couchdb.Manager{
		Interactive: isInteractiveTerminal(),
		AutoApprove: yes,
		PromptProceed: func(status couchdb.Status) (bool, error) {
			versionLabel := status.Version
			if versionLabel == "" {
				versionLabel = "unknown"
			}
			return confirm(fmt.Sprintf("Detected CouchDB %s at %s. Proceed?", versionLabel, status.URL))
		},
		PromptInstall: func() (bool, error) {
			return confirm("CouchDB is not running at http://127.0.0.1:5984. Install and configure it now?")
		},
	}

	result, err := manager.Ensure(ctx)
	if err != nil {
		return err
	}
	if result.Installed {
		logging.Infof("Installed CouchDB %s and stored admin auth in %s", result.Status.Version, result.StorePath)
		fmt.Print(formatCouchDBInstallDetails(result))
		if err := waitForEnter("Press Enter to continue..."); err != nil {
			return err
		}
		return nil
	}
	versionLabel := result.Status.Version
	if versionLabel == "" {
		versionLabel = "unknown"
	}
	logging.Infof("Detected CouchDB %s at %s", versionLabel, result.Status.URL)
	return nil
}

func formatCouchDBInstallDetails(result couchdb.Result) string {
	return fmt.Sprintf(`
CouchDB admin credentials
-------------------------
Username: %s
Password: %s
Auth env: %s

Photon UI
---------
http://localhost:5984/photon/_design/photon/index.html

`, result.AdminUser, result.AdminPassword, result.StorePath)
}

func confirm(message string) (bool, error) {
	answer, err := promptLine(message + " [y/N]: ")
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func promptLine(message string) (string, error) {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func waitForEnter(message string) error {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	_, err := reader.ReadString('\n')
	return err
}

func isInteractiveTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}
