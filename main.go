package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/checks"
	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/logging"
	"github.com/nuxt-apps/couchfusion/internal/workspace"
)

const version = "0.4.1"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	case "init":
		runInit(os.Args[2:])
	case "create_app":
		runCreateApp(os.Args[2:])
	case "create_layer":
		runCreateLayer(os.Args[2:])
	default:
		logging.Errorf("unknown command: %s", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("couchfusion " + version)
	fmt.Println("Usage:")
	fmt.Println("  couchfusion init [--config path] [--path dir] [--layers-branch name] [--force]")
	fmt.Println("  couchfusion create_app [--config path] [--name app] [--modules m1,m2] [--branch name] [--force]")
	fmt.Println("  couchfusion create_layer [--config path] [--name layer] [--branch name] [--force]")
}

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	targetPath := fs.String("path", ".", "Target directory to initialize")
	layerBranch := fs.String("layers-branch", "", "Override branch for layers clone")
	force := fs.Bool("force", false, "Allow reinitialization when directories exist")
	_ = fs.Parse(args)

	cfg, usedDefaultConfig, err := config.Load(*configPath)
	if err != nil {
		logging.Fatalf("failed to load config: %v", err)
	}
	if usedDefaultConfig {
		logging.Warnf("No ~/.couchfusion/config.yaml found; using embedded default configuration.")
	}

	ctx := context.Background()
	warnings := checks.Run(ctx)
	for _, w := range warnings {
		logging.Warnf(w)
	}

	if workspace.ShouldUseTUI() {
		target, err := workspace.RunInitTUI(ctx, cfg, *targetPath, *layerBranch, *force)
		if err != nil {
			if errors.Is(err, workspace.ErrAborted) {
				logging.Warnf("init cancelled by user")
				return
			}
			logging.Fatalf("init failed: %v", err)
		}
		logging.Infof("Initialization complete at %s", target)
		return
	}

	if err := workspace.RunInit(ctx, cfg, *targetPath, *layerBranch, *force); err != nil {
		logging.Fatalf("init failed: %v", err)
	}

	logging.Infof("Initialization complete.")
}

func runCreateApp(args []string) {
	fs := flag.NewFlagSet("create_app", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	name := fs.String("name", "", "Name of the new app")
	modules := fs.String("modules", "", "Comma-separated module list")
	branch := fs.String("branch", "", "Override starter branch")
	force := fs.Bool("force", false, "Allow overwriting empty existing directories")
	_ = fs.Parse(args)

	if *name == "" && len(fs.Args()) > 0 {
		*name = fs.Args()[0]
	}

	if err := workspace.EnsureCurrentWorkspace(); err != nil {
		logging.Fatalf("workspace validation failed: %v", err)
	}

	cfg, usedDefaultConfig, err := config.Load(*configPath)
	if err != nil {
		logging.Fatalf("failed to load config: %v", err)
	}
	if usedDefaultConfig {
		logging.Warnf("No ~/.couchfusion/config.yaml found; using embedded default configuration.")
	}

	ctx := context.Background()
	warnings := checks.Run(ctx)
	for _, w := range warnings {
		logging.Warnf(w)
	}

	if workspace.ShouldUseTUI() {
		appName, selectedModules, err := workspace.RunCreateAppTUI(ctx, cfg, *name, *modules, *branch, *force)
		if err != nil {
			if errors.Is(err, workspace.ErrAborted) {
				logging.Warnf("create_app cancelled by user")
				return
			}
			logging.Fatalf("create_app failed: %v", err)
		}

		logging.Infof("App '%s' created with modules: %s", appName, strings.Join(selectedModules, ", "))
		return
	}

	appName, selectedModules, err := workspace.ResolveAppCreationInputs(cfg, *name, *modules)
	if err != nil {
		logging.Fatalf("input error: %v", err)
	}

	if err := workspace.RunCreateApp(ctx, cfg, appName, selectedModules, *branch, *force); err != nil {
		logging.Fatalf("create_app failed: %v", err)
	}

	logging.Infof("App '%s' created with modules: %s", appName, strings.Join(selectedModules, ", "))
}

func runCreateLayer(args []string) {
	fs := flag.NewFlagSet("create_layer", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	name := fs.String("name", "", "Name of the new layer")
	branch := fs.String("branch", "", "Override starter branch")
	force := fs.Bool("force", false, "Allow overwriting empty existing directories")
	_ = fs.Parse(args)

	cfg, usedDefaultConfig, err := config.Load(*configPath)
	if err != nil {
		logging.Fatalf("failed to load config: %v", err)
	}
	if usedDefaultConfig {
		logging.Warnf("No ~/.couchfusion/config.yaml found; using embedded default configuration.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	warnings := checks.Run(ctx)
	for _, w := range warnings {
		logging.Warnf(w)
	}

	if workspace.ShouldUseTUI() {
		layerName, err := workspace.RunCreateLayerTUI(ctx, cfg, *name, *branch, *force)
		if err != nil {
			if errors.Is(err, workspace.ErrAborted) {
				logging.Warnf("create_layer cancelled by user")
				return
			}
			logging.Fatalf("create_layer failed: %v", err)
		}
		logging.Infof("Layer '%s' created.", layerName)
		return
	}

	layerName, err := workspace.ResolveLayerName(*name)
	if err != nil {
		logging.Fatalf("input error: %v", err)
	}

	if err := workspace.RunCreateLayer(ctx, cfg, layerName, *branch, *force); err != nil {
		logging.Fatalf("create_layer failed: %v", err)
	}

	logging.Infof("Layer '%s' created.", layerName)
}

func init() {
	logging.SetVersion(version)
	os.Setenv("COUCHFUSION_VERSION", version)
}
