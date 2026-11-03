package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegisterPersistsCanonicalWorkspaceWithPrivatePermissions(t *testing.T) {
	root := newValidWorkspace(t)
	registryPath := filepath.Join(t.TempDir(), "state", "workspaces.json")
	service := newTestService(registryPath)

	entry, err := service.Register(context.Background(), filepath.Join(root, "."), "Demo")

	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "workspace-1" || entry.Name != "Demo" || entry.Root != wantRoot {
		t.Fatalf("entry = %#v", entry)
	}
	if !entry.Available || entry.Problem != "" {
		t.Fatalf("availability = %v problem = %q", entry.Available, entry.Problem)
	}
	info, err := os.Stat(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("registry mode = %o, want 600", got)
	}
	parentInfo, err := os.Stat(filepath.Dir(registryPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("registry directory mode = %o, want 700", got)
	}

	reloaded := newTestService(registryPath)
	entries, err := reloaded.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID || entries[0].Root != entry.Root {
		t.Fatalf("reloaded entries = %#v", entries)
	}
}

func TestRegisterRejectsDuplicateCanonicalRoot(t *testing.T) {
	root := newValidWorkspace(t)
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))
	if _, err := service.Register(context.Background(), root, "Demo"); err != nil {
		t.Fatal(err)
	}

	_, err := service.Register(context.Background(), filepath.Join(root, "..", filepath.Base(root)), "Duplicate")

	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("Register() error = %v, want ErrDuplicate", err)
	}
}

func TestRegisterRejectsInvalidWorkspace(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))
	root := t.TempDir()

	_, err := service.Register(context.Background(), root, "Invalid")

	if !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatalf("Register() error = %v, want ErrInvalidWorkspace", err)
	}
}

func TestListKeepsMissingWorkspaceAsUnavailable(t *testing.T) {
	root := newValidWorkspace(t)
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))
	entry, err := service.Register(context.Background(), root, "Demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	entries, err := service.List(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Available || entries[0].Problem == "" {
		t.Fatalf("availability = %v problem = %q", entries[0].Available, entries[0].Problem)
	}
}

func TestForgetDoesNotDeleteWorkspace(t *testing.T) {
	root := newValidWorkspace(t)
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))
	entry, err := service.Register(context.Background(), root, "Demo")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Forget(context.Background(), entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("workspace removed: %v", err)
	}
	entries, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty", entries)
	}
}

func TestRelinkPreservesIdentityAndUpdatesRoot(t *testing.T) {
	oldRoot := newValidWorkspace(t)
	newRoot := newValidWorkspace(t)
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))
	entry, err := service.Register(context.Background(), oldRoot, "Demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(oldRoot); err != nil {
		t.Fatal(err)
	}

	updated, err := service.Relink(context.Background(), entry.ID, newRoot)

	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != entry.ID || updated.Name != entry.Name {
		t.Fatalf("updated identity = %#v, original = %#v", updated, entry)
	}
	wantRoot, _ := filepath.EvalSymlinks(newRoot)
	if updated.Root != wantRoot || !updated.Available {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestTouchUpdatesLastOpenedTime(t *testing.T) {
	root := newValidWorkspace(t)
	registryPath := filepath.Join(t.TempDir(), "workspaces.json")
	service := newTestService(registryPath)
	entry, err := service.Register(context.Background(), root, "Demo")
	if err != nil {
		t.Fatal(err)
	}
	service.Now = func() time.Time { return time.Date(2026, 7, 14, 9, 30, 0, 0, time.UTC) }

	if err := service.Touch(context.Background(), entry.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := entries[0].LastOpenedAt; !got.Equal(service.Now()) {
		t.Fatalf("last opened = %s", got)
	}
}

func TestRenameUpdatesOnlyWorkspaceDisplayName(t *testing.T) {
	root := newValidWorkspace(t)
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))
	entry, err := service.Register(context.Background(), root, "Demo")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := service.Rename(context.Background(), entry.ID, "  Storefront  ")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Storefront" || updated.Root != entry.Root || updated.ID != entry.ID {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := service.Rename(context.Background(), entry.ID, " "); err == nil {
		t.Fatal("expected empty name error")
	}
}

func TestForgetRejectsUnknownID(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "workspaces.json"))

	err := service.Forget(context.Background(), "missing")

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Forget() error = %v, want ErrNotFound", err)
	}
}

func newTestService(path string) *Service {
	return &Service{
		Store: Store{Path: path},
		Now: func() time.Time {
			return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
		},
		NewID: func() (string, error) { return "workspace-1", nil },
	}
}

func newValidWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"apps", "layers"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
