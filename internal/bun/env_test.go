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

func TestShellProfilePathsCoversBashInteractiveAndLoginSessions(t *testing.T) {
	home := t.TempDir()
	paths := ShellProfilePaths(home, "/bin/bash")

	want := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".profile"),
	}
	for _, path := range want {
		if !containsPath(paths, path) {
			t.Fatalf("ShellProfilePaths() = %#v, want %s", paths, path)
		}
	}
}

func TestShellProfilePathsIncludesExistingBashLoginFiles(t *testing.T) {
	home := t.TempDir()
	bashProfile := filepath.Join(home, ".bash_profile")
	if err := os.WriteFile(bashProfile, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := ShellProfilePaths(home, "/bin/bash")
	if !containsPath(paths, bashProfile) {
		t.Fatalf("ShellProfilePaths() = %#v, want existing .bash_profile", paths)
	}
}

func TestShellProfilePathsCoversZshInteractiveAndLoginSessions(t *testing.T) {
	home := t.TempDir()
	paths := ShellProfilePaths(home, "/bin/zsh")

	want := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".zprofile"),
	}
	for _, path := range want {
		if !containsPath(paths, path) {
			t.Fatalf("ShellProfilePaths() = %#v, want %s", paths, path)
		}
	}
}

func TestEnsureProfileBlocksWritesAllStartupFiles(t *testing.T) {
	home := t.TempDir()
	paths := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".profile"),
	}

	if err := EnsureProfileBlocks(paths, false); err != nil {
		t.Fatalf("EnsureProfileBlocks() error = %v", err)
	}

	for _, path := range paths {
		data, err := osReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "BUN_INSTALL") {
			t.Fatalf("%s missing Bun block: %q", path, string(data))
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

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
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

func TestEnsureCurrentPathShimCreatesShimInExistingPathDirectory(t *testing.T) {
	home := t.TempDir()
	pathDir := filepath.Join(home, "bin")
	bunBin := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bunBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bunBin, "bun"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	shim, err := EnsureCurrentPathShim(pathDir+string(os.PathListSeparator)+"/usr/bin", bunBin)
	if err != nil {
		t.Fatalf("EnsureCurrentPathShim() error = %v", err)
	}
	if shim != filepath.Join(pathDir, "bun") {
		t.Fatalf("shim = %q, want %q", shim, filepath.Join(pathDir, "bun"))
	}
	if _, err := os.Stat(shim); err != nil {
		t.Fatalf("shim was not created: %v", err)
	}
}

func TestEnsureCurrentPathShimSkipsWhenTargetMissing(t *testing.T) {
	pathDir := t.TempDir()

	shim, err := EnsureCurrentPathShim(pathDir, filepath.Join(t.TempDir(), ".bun", "bin"))
	if err != nil {
		t.Fatalf("EnsureCurrentPathShim() error = %v", err)
	}
	if shim != "" {
		t.Fatalf("shim = %q, want empty when target is missing", shim)
	}
}

func TestEnsureCurrentPathShimDoesNotOverwriteExistingBun(t *testing.T) {
	home := t.TempDir()
	pathDir := filepath.Join(home, "bin")
	bunBin := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bunBin, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(pathDir, "bun")
	if err := os.WriteFile(existing, []byte("existing\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bunBin, "bun"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	shim, err := EnsureCurrentPathShim(pathDir, bunBin)
	if err != nil {
		t.Fatalf("EnsureCurrentPathShim() error = %v", err)
	}
	if shim != "" {
		t.Fatalf("shim = %q, want empty when bun already exists", shim)
	}
	data, err := osReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("existing bun was overwritten: %q", string(data))
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
