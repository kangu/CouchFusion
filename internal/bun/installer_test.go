package bun

import (
	"context"
	"strings"
	"testing"
)

type recordingRunner struct {
	commands []string
	envs     [][]string
	err      error
	output   []byte
	errs     []error
	outputs  [][]byte
}

func (r *recordingRunner) Run(ctx context.Context, env []string, name string, args ...string) error {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	r.envs = append(r.envs, env)
	return r.err
}

func (r *recordingRunner) Output(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	r.envs = append(r.envs, env)
	if r.output != nil {
		return r.output, r.err
	}
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
	return []byte("1.2.3\n"), r.err
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

func TestSystemInstallerUsesOfficialBunInstaller(t *testing.T) {
	runner := &recordingRunner{}
	_, err := (SystemInstaller{OS: "linux", Runner: runner, HomeDir: "/tmp/home"}).Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "sudo apt install -y unzip ca-certificates curl") {
		t.Fatalf("commands did not install Linux Bun prerequisites:\n%s", joined)
	}
	if !strings.Contains(joined, "curl -fsSL https://bun.com/install | bash") {
		t.Fatalf("commands did not invoke official installer:\n%s", joined)
	}
	if strings.Contains(joined, "bash -lc") {
		t.Fatalf("commands used login shell, which can reset PATH:\n%s", joined)
	}
	if !strings.Contains(joined, "if command -v bun") {
		t.Fatalf("commands did not verify bun through PATH first:\n%s", joined)
	}
	if !strings.Contains(joined, "$BUN_INSTALL/bin/bun") {
		t.Fatalf("commands did not include canonical fallback:\n%s", joined)
	}
	if !envList(runner.envs[0]).Contains("BUN_INSTALL=/tmp/home/.bun") {
		t.Fatalf("installer env missing BUN_INSTALL: %#v", runner.envs[0])
	}
}

func TestSystemInstallerUsesOfficialBunInstallerOnMacOS(t *testing.T) {
	runner := &recordingRunner{}
	_, err := (SystemInstaller{OS: "darwin", Runner: runner, HomeDir: "/tmp/home"}).Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	joined := strings.Join(runner.commands, "\n")
	if strings.Contains(joined, "sudo apt install") {
		t.Fatalf("macOS install should not run apt commands:\n%s", joined)
	}
	if !strings.Contains(joined, "curl -fsSL https://bun.com/install | bash") {
		t.Fatalf("commands did not invoke official installer:\n%s", joined)
	}
	if !strings.Contains(joined, "if command -v bun") {
		t.Fatalf("commands did not verify bun through PATH first:\n%s", joined)
	}
	if !envList(runner.envs[0]).Contains("BUN_INSTALL=/tmp/home/.bun") {
		t.Fatalf("installer env missing BUN_INSTALL: %#v", runner.envs[0])
	}
}

func TestVerifyFailureIncludesInstallerOutput(t *testing.T) {
	runner := &recordingRunner{
		outputs: [][]byte{
			[]byte("installed prereqs\n"),
			[]byte("bun installer said unzip missing\n"),
			[]byte("BUN_INSTALL=/tmp/home/.bun\nUnable to find bun\n"),
		},
		errs: []error{nil, nil, context.Canceled},
	}
	_, err := (SystemInstaller{OS: "linux", Runner: runner, HomeDir: "/tmp/home"}).Install(context.Background())
	if err == nil {
		t.Fatal("Install() error = nil, want verification error")
	}
	if !strings.Contains(err.Error(), "bun installer said unzip missing") {
		t.Fatalf("error did not include installer output:\n%v", err)
	}
}

func TestSystemInstallerTrimsDiagnosticLinesFromVersionOutput(t *testing.T) {
	runner := &recordingRunner{output: []byte("diagnostic\n1.2.3\n")}
	result, err := (SystemInstaller{OS: "linux", Runner: runner, HomeDir: "/tmp/home"}).Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", result.Version)
	}
}

type envList []string

func (e envList) Contains(value string) bool {
	for _, item := range e {
		if item == value {
			return true
		}
	}
	return false
}
