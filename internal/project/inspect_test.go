package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/nuxt-apps/couchfusion/internal/registry"
)

func TestWorkspaceListsDirectSitesAndLayersInStableOrder(t *testing.T) {
	root := newProjectWorkspace(t)
	writeSiteFixture(t, root, "zeta", `{"name":"zeta","scripts":{"dev":"nuxt dev"}}`, `{"appName":"zeta","modules":["auth"]}`)
	writeSiteFixture(t, root, "alpha", `{"name":"alpha"}`, `{"appName":"alpha","modules":[]}`)
	if err := os.WriteFile(filepath.Join(root, "apps", "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, layer := range []string{"orders", "analytics"} {
		if err := os.Mkdir(filepath.Join(root, "layers", layer), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	workspace, err := (Inspector{}).Workspace(context.Background(), registry.Entry{ID: "w1", Name: "Demo", Root: root})

	if err != nil {
		t.Fatalf("Workspace() error = %v", err)
	}
	if !workspace.Available || workspace.ID != "w1" || workspace.Name != "Demo" {
		t.Fatalf("workspace = %#v", workspace)
	}
	if got := siteIDs(workspace.Sites); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("site IDs = %#v", got)
	}
	if got := layerIDs(workspace.Layers); !reflect.DeepEqual(got, []string{"analytics", "orders"}) {
		t.Fatalf("layer IDs = %#v", got)
	}
}

func TestWorkspaceRepresentsMissingRootAsUnavailable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	workspace, err := (Inspector{}).Workspace(context.Background(), registry.Entry{ID: "w1", Root: missing})

	if err != nil {
		t.Fatalf("Workspace() error = %v", err)
	}
	if workspace.Available || workspace.Problem == "" {
		t.Fatalf("workspace = %#v", workspace)
	}
}

func TestWorkspaceSkipsSiteSymlinkThatEscapesAppsRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	root := newProjectWorkspace(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "apps", "escaped")); err != nil {
		t.Fatal(err)
	}

	workspace, err := (Inspector{}).Workspace(context.Background(), registry.Entry{ID: "w1", Root: root})

	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Sites) != 0 {
		t.Fatalf("sites = %#v", workspace.Sites)
	}
	if len(workspace.Warnings) != 1 || workspace.Warnings[0].Code != "site.path_escape" {
		t.Fatalf("warnings = %#v", workspace.Warnings)
	}
}

func TestSiteRejectsTraversalAndSymlinkEscapes(t *testing.T) {
	root := newProjectWorkspace(t)
	inspector := Inspector{}

	if _, err := inspector.Site(context.Background(), root, "../outside"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("traversal error = %v, want ErrPathEscape", err)
	}

	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "apps", "linked")); err != nil {
			t.Fatal(err)
		}
		if _, err := inspector.Site(context.Background(), root, "linked"); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("symlink error = %v, want ErrPathEscape", err)
		}
	}
}

func TestSiteReadsMetadataDependenciesScriptsAndInstallationState(t *testing.T) {
	root := newProjectWorkspace(t)
	writeSiteFixture(t, root, "shop", `{
  "name": "shop-package",
  "packageManager": "bun@1.2.0",
  "scripts": {"dev":"nuxt dev","build":"nuxt build","publish-site":"node publish.mjs"},
  "dependencies": {"@my/content":"file:../../layers/content","vue":"^3"},
  "devDependencies": {"nuxt":"^4"}
}`, `{"appName":"shop","modules":["auth","content"]}`)
	siteRoot := filepath.Join(root, "apps", "shop")
	if err := os.Mkdir(filepath.Join(siteRoot, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "bun.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	site, err := (Inspector{}).Site(context.Background(), root, "shop")

	if err != nil {
		t.Fatalf("Site() error = %v", err)
	}
	if site.ID != "shop" || site.Name != "shop" || site.PackageName != "shop-package" {
		t.Fatalf("site identity = %#v", site)
	}
	if !site.DependenciesInstalled || site.PackageManager != "bun@1.2.0" {
		t.Fatalf("dependency state = %#v", site)
	}
	if !reflect.DeepEqual(site.Lockfiles, []string{"bun.lock"}) {
		t.Fatalf("lockfiles = %#v", site.Lockfiles)
	}
	if !reflect.DeepEqual(site.Modules, []string{"auth", "content"}) {
		t.Fatalf("modules = %#v", site.Modules)
	}
	if site.Scripts["publish-site"] == "" || site.Dependencies["@my/content"] == "" {
		t.Fatalf("package data = %#v", site)
	}
}

func TestSiteReportsMalformedMetadataAsWarnings(t *testing.T) {
	root := newProjectWorkspace(t)
	writeSiteFixture(t, root, "broken", `{not-json`, `{also-not-json`)

	site, err := (Inspector{}).Site(context.Background(), root, "broken")

	if err != nil {
		t.Fatalf("Site() error = %v", err)
	}
	if len(site.Warnings) != 2 {
		t.Fatalf("warnings = %#v", site.Warnings)
	}
	if site.Warnings[0].Code != "site.metadata_invalid" || site.Warnings[1].Code != "site.package_invalid" {
		t.Fatalf("warnings = %#v", site.Warnings)
	}
}

func newProjectWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"apps", "layers"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeSiteFixture(t *testing.T, workspaceRoot, id, packageJSON, metadataJSON string) {
	t.Helper()
	root := filepath.Join(workspaceRoot, "apps", id)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(packageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "couchfusion.json"), []byte(metadataJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func siteIDs(sites []Site) []string {
	ids := make([]string, len(sites))
	for index, site := range sites {
		ids[index] = site.ID
	}
	return ids
}

func layerIDs(layers []Layer) []string {
	ids := make([]string, len(layers))
	for index, layer := range layers {
		ids[index] = layer.ID
	}
	return ids
}
