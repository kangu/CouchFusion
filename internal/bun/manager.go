package bun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrConfirmationRequired = errors.New("bun installation requires interactive confirmation or --yes")

type Result struct {
	Installed bool
	Status    Status
	Env       Env
	BunBin    string
}

type Manager struct {
	Probe       Prober
	Installer   Installer
	Interactive bool
	AutoApprove bool
	HomeDir     string
	Shell       string
	Env         []string

	PromptInstall func() (bool, error)
}

func (m Manager) Ensure(ctx context.Context) (Result, error) {
	env := m.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	home := m.HomeDir
	if strings.TrimSpace(home) == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Result{}, err
		}
	}
	prober := m.Probe
	if prober == nil {
		prober = CommandProber{Env: env}
	}
	status, err := prober.Probe(ctx)
	if err != nil {
		return Result{}, err
	}
	if status.Available {
		return Result{Installed: false, Status: status, Env: Env(env)}, nil
	}
	if !m.AutoApprove {
		if !m.Interactive {
			return Result{}, ErrConfirmationRequired
		}
		if m.PromptInstall == nil {
			return Result{}, fmt.Errorf("%w: no prompt function configured", ErrConfirmationRequired)
		}
		confirmed, err := m.PromptInstall()
		if err != nil {
			return Result{}, err
		}
		if !confirmed {
			return Result{}, errors.New("bun installation cancelled")
		}
	}
	installer := m.Installer
	if installer == nil {
		installer = SystemInstaller{HomeDir: home, Env: env}
	}
	installResult, err := installer.Install(ctx)
	if err != nil {
		return Result{}, err
	}
	originalPath := Env(env).Get("PATH")
	refreshed := RefreshEnv(env, home)
	profilePaths := ShellProfilePaths(home, m.Shell)
	if err := EnsureProfileBlocks(profilePaths, filepath.Base(m.Shell) == "fish"); err != nil {
		return Result{}, err
	}
	currentPathShim, err := EnsureCurrentPathShim(originalPath, installResult.BunBin)
	if err != nil {
		return Result{}, err
	}
	if currentPathShim != "" && !pathContains(refreshed.Get("PATH"), filepath.Dir(currentPathShim)) {
		refreshed = refreshed.Set("PATH", filepath.Dir(currentPathShim)+string(os.PathListSeparator)+refreshed.Get("PATH"))
	}
	shim, err := EnsureCouchFusionBinShim(home, installResult.BunBin)
	if err != nil {
		return Result{}, err
	}
	if shim != "" && !pathContains(refreshed.Get("PATH"), filepath.Dir(shim)) {
		refreshed = refreshed.Set("PATH", filepath.Dir(shim)+string(os.PathListSeparator)+refreshed.Get("PATH"))
	}
	ApplyEnv(refreshed)
	return Result{
		Installed: true,
		Status: Status{
			Available: true,
			Version:   installResult.Version,
			Path:      filepath.Join(home, ".bun", "bin", "bun"),
		},
		Env:    refreshed,
		BunBin: installResult.BunBin,
	}, nil
}
