package bun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type CommandRunner interface {
	Run(context.Context, []string, string, ...string) error
	Output(context.Context, []string, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	output, err := cmd.CombinedOutput()
	if err != nil {
		return commandError{name: name, args: args, err: err, output: string(output)}
	}
	return nil
}

func (execRunner) Output(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, commandError{name: name, args: args, err: err, output: string(output)}
	}
	return output, nil
}

type commandError struct {
	name   string
	args   []string
	err    error
	output string
}

func (e commandError) Error() string {
	output := strings.TrimSpace(e.output)
	if len(output) > 4000 {
		output = output[len(output)-4000:]
	}
	if output == "" {
		return fmt.Sprintf("%s %s failed: %v", e.name, strings.Join(e.args, " "), e.err)
	}
	return fmt.Sprintf("%s %s failed: %v\n%s", e.name, strings.Join(e.args, " "), e.err, output)
}

func (e commandError) Unwrap() error {
	return e.err
}

type Installer interface {
	Install(context.Context) (InstallResult, error)
}

type InstallResult struct {
	Version string
	BunBin  string
}

type SystemInstaller struct {
	Runner  CommandRunner
	OS      string
	HomeDir string
	Env     []string
}

func (i SystemInstaller) Install(ctx context.Context) (InstallResult, error) {
	goos := i.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "darwin" && goos != "linux" {
		return InstallResult{}, fmt.Errorf("managed Bun installation is supported on macOS and Linux only (detected %s)", goos)
	}
	home := i.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return InstallResult{}, err
		}
	}
	env := i.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	refreshed := RefreshEnv(env, home)
	runner := i.Runner
	if runner == nil {
		runner = execRunner{}
	}
	if goos == "linux" {
		if output, err := runner.Output(ctx, refreshed, "bash", "-c", buildLinuxPrereqScript()); err != nil {
			return InstallResult{}, fmt.Errorf("failed to install Bun prerequisites: %w\n%s", err, strings.TrimSpace(string(output)))
		}
	}
	installOutput, err := runner.Output(ctx, refreshed, "bash", "-c", "curl -fsSL https://bun.com/install | bash")
	if err != nil {
		return InstallResult{}, fmt.Errorf("failed to install Bun: %w", err)
	}
	output, err := runner.Output(ctx, refreshed, "bash", "-c", buildVerifyScript())
	if err != nil {
		return InstallResult{}, fmt.Errorf("failed to verify Bun after installation: %w\nInstaller output:\n%s", err, strings.TrimSpace(string(installOutput)))
	}
	return InstallResult{
		Version: lastNonEmptyLine(string(output)),
		BunBin:  filepath.Join(home, ".bun", "bin"),
	}, nil
}

func buildLinuxPrereqScript() string {
	return `
set -e
if command -v unzip >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
  exit 0
fi
sudo -v
sudo apt update -y
sudo apt install -y unzip ca-certificates curl
`
}

func buildVerifyScript() string {
	return `
set -e
echo "BUN_INSTALL=${BUN_INSTALL:-}"
echo "PATH=$PATH"
if command -v bun >/dev/null 2>&1; then
  bun --version
  exit 0
fi
if [ -n "${BUN_INSTALL:-}" ] && [ -x "$BUN_INSTALL/bin/bun" ]; then
  "$BUN_INSTALL/bin/bun" --version
  exit 0
fi
if [ -n "${HOME:-}" ] && [ -x "$HOME/.bun/bin/bun" ]; then
  "$HOME/.bun/bin/bun" --version
  exit 0
fi
echo "Unable to find bun after installation." >&2
if [ -n "${HOME:-}" ]; then
  find "$HOME/.bun" -maxdepth 3 -type f -o -type l 2>/dev/null || true
fi
exit 1
`
}

func lastNonEmptyLine(output string) string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}
