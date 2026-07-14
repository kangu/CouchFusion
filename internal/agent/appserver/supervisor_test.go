package appserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
)

func TestSupervisorInitializesAccountAndSkillRoot(t *testing.T) {
	binary := buildFakeServer(t)
	record := filepath.Join(t.TempDir(), "requests.jsonl")
	skillRoot := filepath.Join(t.TempDir(), "skills")
	supervisor := NewSupervisor(Options{
		Command: func(ctx context.Context) *exec.Cmd {
			command := exec.CommandContext(ctx, binary)
			command.Env = append(os.Environ(), "FAKE_APP_SERVER_RECORD="+record)
			return command
		},
		VersionCommand: func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, binary, "--version") },
		SkillRoot:      skillRoot,
		Version:        "test-version",
	})
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })

	readiness, err := supervisor.EnsureReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != model.AgentReady || readiness.Version != "0.144.4" || readiness.AccountType != "chatgpt" {
		t.Fatalf("readiness = %#v", readiness)
	}

	requests := readRecordedRequests(t, record)
	assertRecordedMethod(t, requests, "initialize", `"name":"couchfusion"`, `"experimentalApi":true`)
	assertRecordedMethod(t, requests, "initialized")
	assertRecordedMethod(t, requests, "skills/extraRoots/set", `"extraRoots":["`+skillRoot+`"]`)
	assertRecordedMethod(t, requests, "account/read")
	if _, err := supervisor.Client(context.Background()); err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if supervisor.Events() == nil || supervisor.Requests() == nil {
		t.Fatal("supervisor event streams are unavailable")
	}
}

func TestSupervisorRejectsUnsupportedMinorVersion(t *testing.T) {
	binary := buildFakeServer(t)
	supervisor := NewSupervisor(Options{
		Command:        func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, binary) },
		VersionCommand: func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, binary, "--version=0.145.0") },
	})
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	readiness, err := supervisor.EnsureReady(context.Background())
	if err == nil || readiness.Status != model.AgentUnsupported {
		t.Fatalf("readiness = %#v, error = %v", readiness, err)
	}
}

func TestSupervisorReportsMissingAndLaunchFailedBinaries(t *testing.T) {
	for _, test := range []struct {
		name    string
		version func(context.Context) *exec.Cmd
		command func(context.Context) *exec.Cmd
		status  model.AgentReadinessStatus
	}{
		{
			name: "missing",
			version: func(ctx context.Context) *exec.Cmd {
				return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "missing-codex"), "--version")
			},
			status: model.AgentMissing,
		},
		{
			name:    "launch failed",
			version: func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, buildFakeServer(t), "--version") },
			command: func(ctx context.Context) *exec.Cmd {
				return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "missing-native-binary"))
			},
			status: model.AgentLaunchFailed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			supervisor := NewSupervisor(Options{Command: test.command, VersionCommand: test.version})
			t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
			readiness, err := supervisor.EnsureReady(context.Background())
			if err == nil || readiness.Status != test.status || strings.Contains(readiness.Problem, os.Getenv("HOME")) {
				t.Fatalf("readiness = %#v, error = %v", readiness, err)
			}
		})
	}
}

func TestSupervisorRestartsAfterUnexpectedExit(t *testing.T) {
	binary := buildFakeServer(t)
	temporary := t.TempDir()
	record := filepath.Join(temporary, "requests.jsonl")
	marker := filepath.Join(temporary, "exit-once")
	supervisor := NewSupervisor(Options{
		Command: func(ctx context.Context) *exec.Cmd {
			command := exec.CommandContext(ctx, binary)
			command.Env = append(os.Environ(), "FAKE_APP_SERVER_RECORD="+record, "FAKE_EXIT_ONCE_MARKER="+marker)
			return command
		},
		VersionCommand: func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, binary, "--version") },
		Backoff:        10 * time.Millisecond,
	})
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	if _, err := supervisor.EnsureReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	initialGeneration := supervisor.Generation()
	if initialGeneration == 0 {
		t.Fatal("initial app-server generation is zero")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		content, _ := os.ReadFile(record)
		if strings.Count(string(content), `"method":"initialize"`) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("app-server was not restarted: %s", content)
		}
		time.Sleep(10 * time.Millisecond)
	}
	readiness, err := supervisor.EnsureReady(context.Background())
	if err != nil || readiness.Status != model.AgentReady {
		t.Fatalf("readiness = %#v, error = %v", readiness, err)
	}
	if supervisor.Generation() <= initialGeneration {
		t.Fatalf("generation = %d, want greater than %d after restart", supervisor.Generation(), initialGeneration)
	}
}

func buildFakeServer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake-app-server")
	command := exec.Command("go", "build", "-o", binary, "./testdata/fake-server.go")
	command.Dir = "."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake server: %v: %s", err, output)
	}
	return binary
}

func readRecordedRequests(t *testing.T, filename string) []map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		content, err := os.ReadFile(filename)
		if err == nil && strings.Count(string(content), "\n") >= 4 {
			var requests []map[string]json.RawMessage
			for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
				var request map[string]json.RawMessage
				if json.Unmarshal([]byte(line), &request) == nil {
					requests = append(requests, request)
				}
			}
			return requests
		}
		if time.Now().After(deadline) {
			t.Fatalf("recorded requests unavailable: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertRecordedMethod(t *testing.T, requests []map[string]json.RawMessage, method string, fragments ...string) {
	t.Helper()
	for _, request := range requests {
		if string(request["method"]) != `"`+method+`"` {
			continue
		}
		encoded, _ := json.Marshal(request)
		for _, fragment := range fragments {
			if !strings.Contains(string(encoded), fragment) {
				t.Fatalf("%s request %s does not contain %s", method, encoded, fragment)
			}
		}
		return
	}
	t.Fatalf("method %q not recorded in %#v", method, requests)
}
