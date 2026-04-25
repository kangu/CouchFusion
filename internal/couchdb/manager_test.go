package couchdb

import (
	"context"
	"errors"
	"path/filepath"
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

func (f *fakeInstaller) Install(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	f.calls++
	return f.result, f.err
}

func TestEnsureRecordsAcceptedRunningVersion(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "couchdb.env")}
	probe := &fakeProbe{status: Status{Running: true, Version: "3.4.2", URL: DefaultURL}}
	installer := &fakeInstaller{}
	prompted := false

	manager := Manager{
		Store:       store,
		Probe:       probe,
		Installer:   installer,
		Interactive: true,
		PromptProceed: func(Status) (bool, error) {
			prompted = true
			return true, nil
		},
	}

	result, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result.Installed {
		t.Fatal("Installed = true, want false")
	}
	if !prompted {
		t.Fatal("PromptProceed was not called")
	}
	if installer.calls != 0 {
		t.Fatalf("installer calls = %d, want 0", installer.calls)
	}

	creds, ok, err := store.Load()
	if err != nil {
		t.Fatalf("store load error = %v", err)
	}
	if !ok || creds.AcceptedVersion != "3.4.2" {
		t.Fatalf("stored creds = %+v ok=%v", creds, ok)
	}
}

func TestEnsureRequiresYesWhenMissingInNonInteractiveMode(t *testing.T) {
	manager := Manager{
		Store:       Store{Path: filepath.Join(t.TempDir(), "couchdb.env")},
		Probe:       &fakeProbe{status: Status{Running: false}},
		Installer:   &fakeInstaller{},
		Interactive: false,
		AutoApprove: false,
	}

	_, err := manager.Ensure(context.Background())
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("Ensure() error = %v, want ErrConfirmationRequired", err)
	}
}

func TestEnsureInstallsAndStoresGeneratedAdminAuth(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "couchdb.env")}
	installer := &fakeInstaller{result: InstallResult{
		URL:           DefaultURL,
		AdminUser:     "admin",
		AdminPassword: "generated",
		AdminAuth:     "YWRtaW46Z2VuZXJhdGVk",
		Version:       "3.4.2",
	}}
	manager := Manager{
		Store:       store,
		Probe:       &fakeProbe{status: Status{Running: false}},
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
	if installer.calls != 1 {
		t.Fatalf("installer calls = %d, want 1", installer.calls)
	}
	creds, ok, err := store.Load()
	if err != nil {
		t.Fatalf("store load error = %v", err)
	}
	if !ok || creds.AdminAuth != "YWRtaW46Z2VuZXJhdGVk" {
		t.Fatalf("stored creds = %+v ok=%v", creds, ok)
	}
	if result.AdminUser != "admin" || result.AdminPassword != "generated" {
		t.Fatalf("install credentials = %q/%q, want admin/generated", result.AdminUser, result.AdminPassword)
	}
}
