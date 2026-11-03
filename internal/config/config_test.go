package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEffectiveUsesEmbeddedDefaultsWhenFilesAreMissing(t *testing.T) {
	root := t.TempDir()

	effective, err := LoadEffective(filepath.Join(t.TempDir(), "missing.yaml"), root)

	if err != nil {
		t.Fatalf("LoadEffective() error = %v", err)
	}
	if effective.Config.Repos["new"].Branch != "main" {
		t.Fatalf("new branch = %q", effective.Config.Repos["new"].Branch)
	}
	if effective.Sources["repos.new.branch"] != SourceEmbedded {
		t.Fatalf("source = %q", effective.Sources["repos.new.branch"])
	}
}

func TestLoadEffectiveMergesGlobalAndWorkspaceValuesWithSources(t *testing.T) {
	base := t.TempDir()
	globalPath := filepath.Join(base, "global.yaml")
	workspaceRoot := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, globalPath, `
repos:
  new:
    branch: preview
    authPrompt: true
modules:
  content:
    description: Global content
  search:
    description: Search layer
    extends: "@layers/search"
prompts:
  defaultLayerSelection: [content, search]
`)
	writeConfigFixture(t, filepath.Join(workspaceRoot, WorkspaceConfigName), `
repos:
  new:
    branch: workspace-preview
    authPrompt: false
modules:
  content:
    description: Workspace content
prompts:
  defaultLayerSelection: []
`)

	effective, err := LoadEffective(globalPath, workspaceRoot)

	if err != nil {
		t.Fatalf("LoadEffective() error = %v", err)
	}
	newRepo := effective.Config.Repos["new"]
	if newRepo.URL == "" || newRepo.Branch != "workspace-preview" || newRepo.AuthPrompt {
		t.Fatalf("new repo = %#v", newRepo)
	}
	if effective.Config.Modules["content"].Description != "Workspace content" {
		t.Fatalf("content = %#v", effective.Config.Modules["content"])
	}
	if effective.Config.Modules["search"].Extends != "@layers/search" {
		t.Fatalf("search = %#v", effective.Config.Modules["search"])
	}
	if len(effective.Config.Prompts.DefaultLayerSelection) != 0 {
		t.Fatalf("defaults = %#v", effective.Config.Prompts.DefaultLayerSelection)
	}
	wantSources := map[string]Source{
		"repos.new.url":                 SourceEmbedded,
		"repos.new.branch":              SourceWorkspace,
		"repos.new.authPrompt":          SourceWorkspace,
		"modules.content.description":   SourceWorkspace,
		"modules.search.extends":        SourceGlobal,
		"prompts.defaultLayerSelection": SourceWorkspace,
	}
	for path, want := range wantSources {
		if got := effective.Sources[path]; got != want {
			t.Errorf("source[%q] = %q, want %q", path, got, want)
		}
	}
}

func TestLoadEffectiveRejectsUnknownDefaultModule(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFixture(t, globalPath, "prompts:\n  defaultLayerSelection: [missing]\n")

	_, err := LoadEffective(globalPath, t.TempDir())

	if err == nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LoadEffective() error = %v, want ErrInvalidConfig", err)
	}
}

func TestWriteGlobalRejectsInvalidConfigWithoutReplacingExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("existing: true\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteGlobal(path, Config{})

	if err == nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("WriteGlobal() error = %v, want ErrInvalidConfig", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != string(original) {
		t.Fatalf("file changed to %q", data)
	}
}

func TestWriteGlobalPersistsValidatedConfigPrivately(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "state", "config.yaml")
	cfg, _, err := Load("")
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteGlobal(path, *cfg); err != nil {
		t.Fatalf("WriteGlobal() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Repos["new"].URL != cfg.Repos["new"].URL {
		t.Fatalf("loaded new repo = %#v", loaded.Repos["new"])
	}
}

func TestWriteGlobalRejectsCredentialBearingRepositoryURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, _, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	repo := cfg.Repos["new"]
	repo.URL = "https://user:secret@example.test/repo.git"
	cfg.Repos["new"] = repo

	err = WriteGlobal(filepath.Join(t.TempDir(), "config.yaml"), *cfg)

	if err == nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("WriteGlobal() error = %v, want ErrInvalidConfig", err)
	}
}

func TestWriteWorkspaceValidatesMergedConfiguration(t *testing.T) {
	root := t.TempDir()
	selection := []string{"missing"}
	override := Override{Prompts: &PromptOverride{DefaultLayerSelection: &selection}}

	err := WriteWorkspace(root, filepath.Join(t.TempDir(), "missing-global.yaml"), override)

	if err == nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("WriteWorkspace() error = %v, want ErrInvalidConfig", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, WorkspaceConfigName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace config stat error = %v", statErr)
	}
}

func writeConfigFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
