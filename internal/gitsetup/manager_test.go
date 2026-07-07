package gitsetup

import (
	"context"
	"errors"
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

func TestEnsureReturnsDetectedVersionWhenGitExists(t *testing.T) {
	installer := &fakeInstaller{}
	manager := Manager{
		Probe:     &fakeProbe{status: Status{Available: true, Version: "git version 2.43.0"}},
		Installer: installer,
	}

	result, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result.Installed {
		t.Fatal("Installed = true, want false")
	}
	if result.Status.Version != "git version 2.43.0" {
		t.Fatalf("Version = %q, want git version 2.43.0", result.Status.Version)
	}
	if installer.calls != 0 {
		t.Fatalf("installer calls = %d, want 0", installer.calls)
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

func TestEnsureInstallsWhenMissingAndApproved(t *testing.T) {
	installer := &fakeInstaller{result: InstallResult{Version: "git version 2.43.0", Path: "/usr/bin/git"}}
	manager := Manager{
		Probe:       &fakeProbe{status: Status{Available: false}},
		Installer:   installer,
		Interactive: false,
		AutoApprove: true,
	}

	result, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !result.Installed {
		t.Fatal("Installed = false, want true")
	}
	if result.Status.Version != "git version 2.43.0" {
		t.Fatalf("Version = %q, want git version 2.43.0", result.Status.Version)
	}
	if result.Status.Path != "/usr/bin/git" {
		t.Fatalf("Path = %q, want /usr/bin/git", result.Status.Path)
	}
	if installer.calls != 1 {
		t.Fatalf("installer calls = %d, want 1", installer.calls)
	}
}
