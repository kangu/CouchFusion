package couchdb

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallerScriptsUseLocalBindAddressAndValidBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	scripts := map[string]string{
		"darwin-install": buildDarwinInstallScript(),
		"darwin-config":  buildDarwinConfigScript("admin", "secret"),
		"linux":          buildLinuxInstallScript("admin", "secret", "cookie"),
	}

	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			if name != "darwin-install" && !strings.Contains(script, "127.0.0.1") {
				t.Fatalf("script does not configure local bind address")
			}
			cmd := exec.Command("bash", "-n")
			cmd.Stdin = strings.NewReader(script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("bash -n failed: %v\n%s", err, string(output))
			}
		})
	}
}

func TestSystemInstallerRejectsUnsupportedOS(t *testing.T) {
	_, err := (SystemInstaller{OS: "windows"}).Install(testContext(t), InstallOptions{})
	if err == nil {
		t.Fatal("Install() error = nil, want unsupported OS error")
	}
	if !strings.Contains(err.Error(), "macOS and Debian/Ubuntu") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestLinuxInstallScriptPreflightsSudo(t *testing.T) {
	script := buildLinuxInstallScript("admin", "secret", "cookie")
	if !strings.Contains(script, "sudo -v") {
		t.Fatal("linux install script does not run sudo -v before apt operations")
	}
}

func TestDarwinInstallScriptPersistsHomebrewShellenv(t *testing.T) {
	script := buildDarwinInstallScript()
	for _, want := range []string{
		"/opt/homebrew/bin/brew shellenv",
		"/usr/local/bin/brew shellenv",
		".zprofile",
		".zshrc",
		".bash_profile",
		".bashrc",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("darwin install script missing %q:\n%s", want, script)
		}
	}
}

func TestDarwinConfigScriptPersistsHomebrewShellenv(t *testing.T) {
	script := buildDarwinConfigScript("admin", "secret")
	if !strings.Contains(script, "ensure_homebrew_shellenv_profiles") {
		t.Fatalf("darwin config script does not persist Homebrew shellenv:\n%s", script)
	}
	if !strings.Contains(script, "brew services start couchdb") {
		t.Fatalf("darwin config script does not start CouchDB service:\n%s", script)
	}
}

func TestCommandErrorIncludesCapturedOutput(t *testing.T) {
	err := commandError{
		name:   "bash",
		args:   []string{"-lc", "false"},
		err:    context.Canceled,
		output: "apt failed with useful details",
	}
	got := err.Error()
	if !strings.Contains(got, "apt failed with useful details") {
		t.Fatalf("error = %q, want captured output", got)
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
