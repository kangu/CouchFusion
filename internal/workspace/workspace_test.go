package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/gitutil"
)

func TestCreateSiteUsesExplicitRoot(t *testing.T) {
	root := newWorkspaceFixture(t)
	other := t.TempDir()
	t.Chdir(other)
	clone := &fakeClone{scaffoldSite: true}

	err := CreateSite(context.Background(), testWorkspaceConfig(), CreateSiteRequest{
		Root:          root,
		Name:          "alpha",
		Modules:       []string{"content"},
		DBLoginPrefix: "ns",
	}, Dependencies{Clone: clone.Clone})

	if err != nil {
		t.Fatalf("CreateSite() error = %v", err)
	}
	wantTarget := filepath.Join(root, "apps", "alpha")
	if clone.target != wantTarget {
		t.Fatalf("clone target = %q, want %q", clone.target, wantTarget)
	}
	if _, err := os.Stat(filepath.Join(wantTarget, "couchfusion.json")); err != nil {
		t.Fatalf("site metadata missing: %v", err)
	}
}

func TestCreateLayerUsesExplicitRoot(t *testing.T) {
	root := newWorkspaceFixture(t)
	t.Chdir(t.TempDir())
	clone := &fakeClone{}

	err := CreateLayer(context.Background(), testWorkspaceConfig(), CreateLayerRequest{
		Root: root,
		Name: "search",
	}, Dependencies{Clone: clone.Clone})

	if err != nil {
		t.Fatalf("CreateLayer() error = %v", err)
	}
	wantTarget := filepath.Join(root, "layers", "search")
	if clone.target != wantTarget {
		t.Fatalf("clone target = %q, want %q", clone.target, wantTarget)
	}
}

func TestInitUsesExplicitRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	t.Chdir(t.TempDir())
	clone := &fakeClone{}

	err := Init(context.Background(), testWorkspaceConfig(), InitRequest{Root: root}, Dependencies{Clone: clone.Clone})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if clone.target != filepath.Join(root, "layers") {
		t.Fatalf("clone target = %q", clone.target)
	}
	if info, err := os.Stat(filepath.Join(root, "apps")); err != nil || !info.IsDir() {
		t.Fatalf("apps directory not created: info=%v err=%v", info, err)
	}
}

func TestCreateSiteRejectsExistingNonEmptyTargetWithoutCallingClone(t *testing.T) {
	root := newWorkspaceFixture(t)
	target := filepath.Join(root, "apps", "alpha")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	clone := &fakeClone{scaffoldSite: true}

	err := CreateSite(context.Background(), testWorkspaceConfig(), CreateSiteRequest{
		Root:          root,
		Name:          "alpha",
		DBLoginPrefix: "ns",
	}, Dependencies{Clone: clone.Clone})

	if err == nil {
		t.Fatal("CreateSite() error = nil, want existing target error")
	}
	if clone.calls != 0 {
		t.Fatalf("clone calls = %d, want 0", clone.calls)
	}
	if data, readErr := os.ReadFile(filepath.Join(target, "keep.txt")); readErr != nil || string(data) != "keep" {
		t.Fatalf("existing content changed: data=%q err=%v", data, readErr)
	}
}

func TestCreateSitePassesCredentialsWithoutTerminalInput(t *testing.T) {
	root := newWorkspaceFixture(t)
	clone := &fakeClone{scaffoldSite: true}
	var got AuthCredentials

	err := CreateSite(context.Background(), testWorkspaceConfig(), CreateSiteRequest{
		Root:          root,
		Name:          "alpha",
		Modules:       []string{"auth"},
		DBLoginPrefix: "ns",
		Auth:          &AuthCredentials{Username: "admin", Password: "secret"},
	}, Dependencies{
		Clone: clone.Clone,
		ApplyLayerParameters: func(ctx context.Context, _ string, modules []string) error {
			credentials, ok := credentialsFromContext(ctx)
			if !ok {
				return errors.New("credentials missing from context")
			}
			got = AuthCredentials(credentials)
			if !reflect.DeepEqual(modules, []string{"auth"}) {
				t.Fatalf("modules = %#v", modules)
			}
			return nil
		},
	})

	if err != nil {
		t.Fatalf("CreateSite() error = %v", err)
	}
	if got.Username != "admin" || got.Password != "secret" {
		t.Fatalf("credentials = %#v", got)
	}
}

func TestCreateSiteRejectsInvalidDBLoginPrefixBeforeClone(t *testing.T) {
	root := newWorkspaceFixture(t)
	clone := &fakeClone{scaffoldSite: true}

	err := CreateSite(context.Background(), testWorkspaceConfig(), CreateSiteRequest{
		Root:          root,
		Name:          "alpha",
		DBLoginPrefix: "A1",
	}, Dependencies{Clone: clone.Clone})

	if err == nil {
		t.Fatal("CreateSite() error = nil, want database login prefix validation error")
	}
	if clone.calls != 0 {
		t.Fatalf("clone calls = %d, want 0", clone.calls)
	}
}

func TestCreateSiteConfiguresDBLoginPrefixAfterClone(t *testing.T) {
	root := newWorkspaceFixture(t)
	clone := &fakeClone{scaffoldSite: true}
	events := &recordingEventSink{}

	err := CreateSite(context.Background(), testWorkspaceConfig(), CreateSiteRequest{
		Root:          root,
		Name:          "alpha",
		DBLoginPrefix: "ac",
	}, Dependencies{Clone: clone.Clone, Events: events})

	if err != nil {
		t.Fatalf("CreateSite() error = %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "apps", "alpha", "nuxt.config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "dbLoginPrefix: 'ac-'") {
		t.Fatalf("nuxt.config.ts does not contain configured prefix:\n%s", contents)
	}
	if !events.contains(Event{Level: EventSuccess, Code: "site.config.database_prefix", Message: "Database login prefix configured"}) {
		t.Fatalf("events = %#v, want database prefix success event", events.events)
	}
}

type fakeClone struct {
	calls        int
	target       string
	scaffoldSite bool
}

func (f *fakeClone) Clone(_ context.Context, _, _, targetDir, _ string, _ bool, _ ...gitutil.CloneOption) error {
	f.calls++
	f.target = targetDir
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	if !f.scaffoldSite {
		return nil
	}
	if err := os.WriteFile(filepath.Join(targetDir, "package.json"), []byte("{\"dependencies\":{}}\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, "nuxt.config.ts"), []byte("export default defineNuxtConfig({ extends: [], runtimeConfig: { dbLoginPrefix: 'ns-' } })\n"), 0o644)
}

type recordingEventSink struct {
	events []Event
}

func (s *recordingEventSink) Emit(event Event) {
	s.events = append(s.events, event)
}

func (s *recordingEventSink) contains(want Event) bool {
	for _, event := range s.events {
		if event == want {
			return true
		}
	}
	return false
}

func newWorkspaceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"apps", "layers"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testWorkspaceConfig() *config.Config {
	return &config.Config{
		Repos: map[string]config.RepoConfig{
			"init":         {URL: "https://example.test/layers.git", Branch: "main", Protocol: "https"},
			"new":          {URL: "https://example.test/app.git", Branch: "main", Protocol: "https"},
			"create_layer": {URL: "https://example.test/layer.git", Branch: "main", Protocol: "https"},
		},
		Modules: map[string]config.ModuleConfig{
			"auth":    {Description: "Authentication"},
			"content": {Description: "Content"},
		},
	}
}
