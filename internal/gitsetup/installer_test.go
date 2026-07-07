package gitsetup

import (
	"context"
	"strings"
	"testing"
)

type recordingRunner struct {
	commands []string
	err      error
	outputs  [][]byte
	errs     []error
}

func (r *recordingRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if len(r.outputs) > 0 {
		output := r.outputs[0]
		r.outputs = r.outputs[1:]
		err := r.err
		if len(r.errs) > 0 {
			err = r.errs[0]
			r.errs = r.errs[1:]
		}
		return output, err
	}
	return []byte("git version 2.43.0\n"), r.err
}

func TestSystemInstallerRejectsUnsupportedOS(t *testing.T) {
	_, err := (SystemInstaller{OS: "windows"}).Install(context.Background())
	if err == nil {
		t.Fatal("Install() error = nil, want unsupported OS error")
	}
	if !strings.Contains(err.Error(), "macOS and Linux") {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestSystemInstallerInstallsGitOnLinuxWithApt(t *testing.T) {
	runner := &recordingRunner{}
	result, err := (SystemInstaller{OS: "linux", Runner: runner}).Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "sudo -v") {
		t.Fatalf("commands did not validate sudo first:\n%s", joined)
	}
	if !strings.Contains(joined, "sudo apt update -y") {
		t.Fatalf("commands did not update apt metadata:\n%s", joined)
	}
	if !strings.Contains(joined, "sudo apt install -y git") {
		t.Fatalf("commands did not install Git through apt:\n%s", joined)
	}
	if !strings.Contains(joined, "git --version") {
		t.Fatalf("commands did not verify Git after install:\n%s", joined)
	}
	if result.Version != "git version 2.43.0" {
		t.Fatalf("Version = %q, want git version 2.43.0", result.Version)
	}
}

func TestSystemInstallerInstallsGitOnMacOSWithHomebrew(t *testing.T) {
	runner := &recordingRunner{}
	_, err := (SystemInstaller{OS: "darwin", Runner: runner}).Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "brew install git") {
		t.Fatalf("commands did not install Git through Homebrew:\n%s", joined)
	}
	if !strings.Contains(joined, "git --version") {
		t.Fatalf("commands did not verify Git after install:\n%s", joined)
	}
}
