package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
)

func TestManagerStartsTrustedPreviewAndBecomesReady(t *testing.T) {
	processManager := newProcessFake()
	manager := newTestManager(processManager, 44001, "preview-token", time.Second)
	command := project.Command{
		Executable:  "bun",
		Args:        []string{"run", "dev"},
		Dir:         "/workspace/apps/shop",
		Environment: map[string]string{"EXISTING": "kept"},
	}

	started, err := manager.Start(context.Background(), "w1", "shop", command)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != Starting || started.ProcessID != "process-1" || started.URL != "" {
		t.Fatalf("started view = %#v", started)
	}
	request := processManager.lastStart()
	if request.TaskID != TaskID {
		t.Fatalf("task ID = %q, want %q", request.TaskID, TaskID)
	}
	wantEnvironment := map[string]string{
		"EXISTING":                  "kept",
		"COUCHFUSION_PREVIEW":       "1",
		"COUCHFUSION_PREVIEW_TOKEN": "preview-token",
		"NUXT_HOST":                 "127.0.0.1",
		"NUXT_PORT":                 "44001",
	}
	if fmt.Sprint(request.Command.Environment) != fmt.Sprint(wantEnvironment) {
		t.Fatalf("environment = %#v, want %#v", request.Command.Environment, wantEnvironment)
	}
	if fmt.Sprint(request.SecretValues) != "[preview-token]" {
		t.Fatalf("secret values = %#v", request.SecretValues)
	}
	processManager.setURLs("process-1", []string{"http://127.0.0.1:44001/"})

	ready := waitForView(t, manager, "w1", "shop", Ready)
	wantURL := "http://127.0.0.1:44001/__couchfusion/components/preview?token=preview-token"
	if ready.URL != wantURL {
		t.Fatalf("ready URL = %q, want %q", ready.URL, wantURL)
	}
	encoded, _ := json.Marshal(ready)
	if strings.Contains(string(encoded), `"token":`) {
		t.Fatalf("view exposes standalone token: %s", encoded)
	}

	again, err := manager.Start(context.Background(), "w1", "shop", command)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProcessID != started.ProcessID || processManager.startCount() != 1 {
		t.Fatalf("idempotent start = %#v, starts = %d", again, processManager.startCount())
	}
}

func TestManagerRejectsWrongOriginAndStopsAfterReadinessTimeout(t *testing.T) {
	processManager := newProcessFake()
	manager := newTestManager(processManager, 44002, "timeout-token", 35*time.Millisecond)

	_, err := manager.Start(context.Background(), "w1", "shop", project.Command{
		Executable: "bun",
		Args:       []string{"run", "dev"},
		Dir:        "/workspace/apps/shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	processManager.setURLs("process-1", []string{
		"http://127.0.0.1:9999/",
		"https://127.0.0.1:44002/",
		"http://example.test:44002/",
	})

	failed := waitForView(t, manager, "w1", "shop", Failed)
	if !strings.Contains(failed.Problem, "60 seconds") {
		t.Fatalf("problem = %q", failed.Problem)
	}
	if processManager.stopCount() != 1 {
		t.Fatalf("stop count = %d, want 1", processManager.stopCount())
	}
}

func TestManagerMapsTerminalExitAndStopIsIdempotent(t *testing.T) {
	processManager := newProcessFake()
	manager := newTestManager(processManager, 44003, "terminal-token", time.Second)
	command := project.Command{Executable: "bun", Args: []string{"run", "dev"}, Dir: "/workspace/apps/shop"}

	_, err := manager.Start(context.Background(), "w1", "shop", command)
	if err != nil {
		t.Fatal(err)
	}
	processManager.setStatus("process-1", processes.Failed)
	failed := waitForView(t, manager, "w1", "shop", Failed)
	if failed.Problem == "" {
		t.Fatalf("failed view = %#v", failed)
	}

	stopped, err := manager.Stop(context.Background(), "w1", "shop")
	if err != nil || stopped.Status != Stopped || stopped.URL != "" {
		t.Fatalf("Stop() = %#v, %v", stopped, err)
	}
	stopped, err = manager.Stop(context.Background(), "w1", "shop")
	if err != nil || stopped.Status != Stopped {
		t.Fatalf("second Stop() = %#v, %v", stopped, err)
	}
	status, err := manager.Status(context.Background(), "w1", "shop")
	if err != nil || status.Status != Stopped {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
}

func TestManagerDetectsProcessFailureAfterBecomingReady(t *testing.T) {
	processManager := newProcessFake()
	manager := newTestManager(processManager, 44004, "crash-token", time.Second)
	command := project.Command{Executable: "bun", Args: []string{"run", "dev"}, Dir: "/workspace/apps/shop"}

	_, err := manager.Start(context.Background(), "w1", "shop", command)
	if err != nil {
		t.Fatal(err)
	}
	processManager.setURLs("process-1", []string{"http://localhost:44004/"})
	waitForView(t, manager, "w1", "shop", Ready)

	processManager.setStatus("process-1", processes.Failed)
	failed := waitForView(t, manager, "w1", "shop", Failed)
	if failed.URL != "" || failed.Problem == "" {
		t.Fatalf("failed view = %#v", failed)
	}
}

func newTestManager(processManager *processFake, port int, token string, timeout time.Duration) *Manager {
	return NewManager(Options{
		Processes:    processManager,
		AllocatePort: func() (int, error) { return port, nil },
		NewToken:     func() (string, error) { return token, nil },
		Now:          func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) },
		ReadyTimeout: timeout,
		PollInterval: 2 * time.Millisecond,
	})
}

func waitForView(t *testing.T, manager *Manager, workspaceID, siteID string, want Status) View {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		view, err := manager.Status(context.Background(), workspaceID, siteID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Status == want {
			return view
		}
		time.Sleep(time.Millisecond)
	}
	view, _ := manager.Status(context.Background(), workspaceID, siteID)
	t.Fatalf("status = %q, want %q; view=%#v", view.Status, want, view)
	return View{}
}

type processFake struct {
	mu        sync.Mutex
	starts    []processes.StartRequest
	snapshots map[string]processes.Snapshot
	stops     int
}

func newProcessFake() *processFake {
	return &processFake{snapshots: map[string]processes.Snapshot{}}
}

func (f *processFake) Start(_ context.Context, request processes.StartRequest) (processes.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("process-%d", len(f.starts)+1)
	f.starts = append(f.starts, request)
	snapshot := processes.Snapshot{
		ID: id, WorkspaceID: request.WorkspaceID, SiteID: request.SiteID,
		TaskID: request.TaskID, Status: processes.Running,
		StartedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), URLs: []string{}, Events: []processes.Event{},
	}
	f.snapshots[id] = snapshot
	return snapshot, nil
}

func (f *processFake) Get(id string) (processes.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot, exists := f.snapshots[id]
	if !exists {
		return processes.Snapshot{}, processes.ErrNotFound
	}
	return snapshot, nil
}

func (f *processFake) Stop(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot, exists := f.snapshots[id]
	if !exists {
		return processes.ErrNotFound
	}
	f.stops++
	snapshot.Status = processes.Stopped
	f.snapshots[id] = snapshot
	return nil
}

func (f *processFake) setURLs(id string, urls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := f.snapshots[id]
	snapshot.URLs = append([]string{}, urls...)
	f.snapshots[id] = snapshot
}

func (f *processFake) setStatus(id string, status processes.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := f.snapshots[id]
	snapshot.Status = status
	if status == processes.Failed {
		snapshot.Error = &processes.SafeError{Code: "process_failed", Message: "Process exited unexpectedly"}
	}
	f.snapshots[id] = snapshot
}

func (f *processFake) lastStart() processes.StartRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts[len(f.starts)-1]
}

func (f *processFake) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

func (f *processFake) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}
