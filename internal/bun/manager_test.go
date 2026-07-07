package bun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeProbe struct {
	status Status
	err    error
	calls  int
}

func (f *fakeProbe) Probe(ctx context.Context) (Status, error) {
	f.calls++
	return f.status, f.err
}

type fakeInstaller struct {
	result InstallResult
	err    error
	calls  int
}

func (f *fakeInstaller) Install(ctx context.Context) (InstallResult, error) {
	f.calls++
	return f.result, f.err
}

func TestEnsureReturnsDetectedVersionWhenBunExists(t *testing.T) {
	manager := Manager{
		Probe: &fakeProbe{status: Status{Available: true, Version: "1.2.3"}},
	}

	result, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result.Installed {
		t.Fatal("Installed = true, want false")
	}
	if result.Status.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", result.Status.Version)
	}
}

func TestEnsureRequiresYesWhenMissingInNonInteractiveMode(t *testing.T) {
	manager := Manager{
		Probe:       &fakeProbe{status: Status{Available: false}},
		Installer:   &fakeInstaller{},
		Interactive: false,
		AutoApprove: false,
	}

	_, err := manager.Ensure(context.Background())
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("Ensure() error = %v, want ErrConfirmationRequired", err)
	}
}

func TestEnsureInstallsRefreshesEnvironmentAndUpdatesProfile(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".bashrc")
	currentPathDir := filepath.Join(home, "current-bin")
	bunBin := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(currentPathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bunBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bunBin, "bun"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := Manager{
		Probe:       &fakeProbe{status: Status{Available: false}},
		Installer:   &fakeInstaller{result: InstallResult{Version: "1.2.3", BunBin: bunBin}},
		Interactive: false,
		AutoApprove: true,
		HomeDir:     home,
		Shell:       "/bin/bash",
		Env:         []string{"PATH=" + currentPathDir + string(os.PathListSeparator) + "/usr/bin"},
	}

	result, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !result.Installed {
		t.Fatal("Installed = false, want true")
	}
	if result.Env.Get("BUN_INSTALL") != filepath.Join(home, ".bun") {
		t.Fatalf("BUN_INSTALL = %q", result.Env.Get("BUN_INSTALL"))
	}
	pathValue := result.Env.Get("PATH")
	if !strings.Contains(pathValue, filepath.Join(home, ".bun", "bin")) {
		t.Fatalf("PATH = %q, want bun bin present", pathValue)
	}
	if !strings.Contains(pathValue, filepath.Join(home, ".couchfusion", "bin")) {
		t.Fatalf("PATH = %q, want couchfusion bin present", pathValue)
	}
	if _, err := os.Stat(filepath.Join(currentPathDir, "bun")); err != nil {
		t.Fatalf("current PATH shim was not created: %v", err)
	}
	content, err := readFile(profile)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if !strings.Contains(content, "BUN_INSTALL") {
		t.Fatalf("profile content missing Bun block: %q", content)
	}
}

func readFile(path string) (string, error) {
	data, err := osReadFile(path)
	return string(data), err
}
