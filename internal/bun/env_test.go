package bun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshEnvPrependsBunBinAndSetsBunInstall(t *testing.T) {
	home := t.TempDir()
	env := RefreshEnv([]string{"PATH=/usr/bin"}, home)

	if env.Get("BUN_INSTALL") != filepath.Join(home, ".bun") {
		t.Fatalf("BUN_INSTALL = %q", env.Get("BUN_INSTALL"))
	}
	if got := env.Get("PATH"); !strings.HasPrefix(got, filepath.Join(home, ".bun", "bin")) {
		t.Fatalf("PATH = %q, want bun bin first", got)
	}
}

func TestShellProfilePath(t *testing.T) {
	home := "/home/test"
	cases := map[string]string{
		"/bin/zsh":  "/home/test/.zshrc",
		"/bin/bash": "/home/test/.bashrc",
		"/bin/fish": "/home/test/.config/fish/config.fish",
		"":          "/home/test/.profile",
	}
	for shell, want := range cases {
		if got := ShellProfilePath(home, shell); got != want {
			t.Fatalf("ShellProfilePath(%q) = %q, want %q", shell, got, want)
		}
	}
}

func TestEnsureProfileBlockIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := EnsureProfileBlock(path, false); err != nil {
		t.Fatalf("EnsureProfileBlock first error = %v", err)
	}
	if err := EnsureProfileBlock(path, false); err != nil {
		t.Fatalf("EnsureProfileBlock second error = %v", err)
	}
	data, err := osReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "# Bun runtime"); count != 1 {
		t.Fatalf("Bun block count = %d, want 1\n%s", count, string(data))
	}
}

func TestEnsureCouchFusionBinShimCreatesExecutableLink(t *testing.T) {
	home := t.TempDir()
	bunBin := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(bunBin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bunBin, "bun")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	shim, err := EnsureCouchFusionBinShim(home, bunBin)
	if err != nil {
		t.Fatalf("EnsureCouchFusionBinShim() error = %v", err)
	}
	if shim != filepath.Join(home, ".couchfusion", "bin", "bun") {
		t.Fatalf("shim = %q", shim)
	}
	if _, err := os.Stat(shim); err != nil {
		t.Fatalf("shim was not created: %v", err)
	}
}

func TestEnsureCouchFusionBinShimSkipsMissingTarget(t *testing.T) {
	home := t.TempDir()
	shim, err := EnsureCouchFusionBinShim(home, filepath.Join(home, ".bun", "bin"))
	if err != nil {
		t.Fatalf("EnsureCouchFusionBinShim() error = %v", err)
	}
	if shim != "" {
		t.Fatalf("shim = %q, want empty when target is missing", shim)
	}
}
