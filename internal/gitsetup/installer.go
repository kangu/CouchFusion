package gitsetup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type CommandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
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
	Path    string
}

type SystemInstaller struct {
	Runner CommandRunner
	OS     string
}

func (i SystemInstaller) Install(ctx context.Context) (InstallResult, error) {
	runner := i.Runner
	if runner == nil {
		runner = execRunner{}
	}
	goos := i.OS
	if goos == "" {
		goos = runtime.GOOS
	}

	switch goos {
	case "linux":
		if _, err := runner.Output(ctx, "bash", "-lc", buildLinuxInstallScript()); err != nil {
			return InstallResult{}, fmt.Errorf("failed to install Git on Debian/Ubuntu: %w", err)
		}
	case "darwin":
		if _, err := runner.Output(ctx, "bash", "-lc", buildDarwinInstallScript()); err != nil {
			return InstallResult{}, fmt.Errorf("failed to install Git with Homebrew: %w", err)
		}
	default:
		return InstallResult{}, fmt.Errorf("managed Git installation is supported on macOS and Linux only (detected %s)", goos)
	}

	output, err := runner.Output(ctx, "bash", "-lc", buildVerifyScript())
	if err != nil {
		return InstallResult{}, fmt.Errorf("failed to verify Git after installation: %w", err)
	}
	return InstallResult{
		Version: lastNonEmptyLine(string(output)),
		Path:    "git",
	}, nil
}

func buildLinuxInstallScript() string {
	return `
set -euo pipefail
sudo -v
sudo apt update -y
sudo apt install -y git
`
}

func buildDarwinInstallScript() string {
	return `
set -euo pipefail
if [ -x /opt/homebrew/bin/brew ]; then
  eval "$(/opt/homebrew/bin/brew shellenv)"
elif [ -x /usr/local/bin/brew ]; then
  eval "$(/usr/local/bin/brew shellenv)"
fi
if ! command -v brew >/dev/null 2>&1; then
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  if [ -x /opt/homebrew/bin/brew ]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
  elif [ -x /usr/local/bin/brew ]; then
    eval "$(/usr/local/bin/brew shellenv)"
  fi
fi
brew update
brew install git
`
}

func buildVerifyScript() string {
	return `
set -euo pipefail
if [ -x /opt/homebrew/bin/brew ]; then
  eval "$(/opt/homebrew/bin/brew shellenv)"
elif [ -x /usr/local/bin/brew ]; then
  eval "$(/usr/local/bin/brew shellenv)"
fi
git --version
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
