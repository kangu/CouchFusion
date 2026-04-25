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

func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
