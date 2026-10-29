package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxt-apps/couchfusion/internal/couchdb"
	"github.com/nuxt-apps/couchfusion/internal/registry"
)

func TestNewUIServiceMaterializesAgentSkillWithoutStartingCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	service, err := newUIService("")
	if err != nil {
		t.Fatalf("newUIService() error = %v", err)
	}
	_ = service.Shutdown(context.Background())
	matches, err := filepath.Glob(filepath.Join(home, ".couchfusion", "agent", "skills", "*", "couchfusion-component-builder", "SKILL.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("materialized skills = %#v, error = %v", matches, err)
	}
}

func TestRegisteredCouchDBCredentialStoresOnlyUseExplicitWorkspaces(t *testing.T) {
	registeredRoot := filepath.Join(t.TempDir(), "registered")
	unregisteredRoot := filepath.Join(t.TempDir(), "unregistered")
	for _, root := range []string{registeredRoot, unregisteredRoot} {
		for _, directory := range []string{"apps/shop", "layers"} {
			if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "apps", "shop", ".env"), []byte("COUCHDB_ADMIN_AUTH=token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	registryService := &registry.Service{Store: registry.Store{Path: filepath.Join(t.TempDir(), "workspaces.json")}}
	if _, err := registryService.Register(context.Background(), registeredRoot, "Registered"); err != nil {
		t.Fatal(err)
	}

	stores, err := registeredCouchDBCredentialStores(context.Background(), registryService)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(registeredRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 1 || stores[0].Path != filepath.Join(canonicalRoot, "apps", "shop", ".env") {
		t.Fatalf("credential stores = %#v", stores)
	}
}

func TestWorkflowFlagErrorsReturnWithoutExiting(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{name: "init", run: func() error { return runInit(context.Background(), nil, []string{"--unknown"}) }},
		{name: "new", run: func() error { return runNew(context.Background(), nil, []string{"--unknown"}) }},
		{name: "create layer", run: func() error { return runCreateLayer(context.Background(), nil, []string{"--unknown"}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("expected flag parsing error")
			}
		})
	}
}

func TestAddYesFlagsAcceptsLongAndShortForms(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"-y"}} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		yes := addYesFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatalf("Parse(%v) error = %v", args, err)
		}
		if !*yes {
			t.Fatalf("yes flag for args %v = false, want true", args)
		}
	}
}

func TestWaitForEnterReturnsAfterNewline(t *testing.T) {
	original := os.Stdin
	defer func() { os.Stdin = original }()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	os.Stdin = reader

	if _, err := writer.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := waitForEnter(""); err != nil {
		t.Fatalf("waitForEnter() error = %v", err)
	}
}

func TestFormatCouchDBInstallDetailsIncludesPhotonURL(t *testing.T) {
	output := formatCouchDBInstallDetails(couchdb.Result{
		AdminUser:     "admin",
		AdminPassword: "secret",
		StorePath:     "/tmp/couchdb.env",
	})

	if !strings.Contains(output, "CouchDB admin credentials") {
		t.Fatalf("output missing credentials heading:\n%s", output)
	}
	if !strings.Contains(output, "Photon UI") {
		t.Fatalf("output missing Photon section:\n%s", output)
	}
	if !strings.Contains(output, "http://localhost:5984/photon/_design/photon/index.html") {
		t.Fatalf("output missing Photon URL:\n%s", output)
	}
}
