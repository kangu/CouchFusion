package processes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/project"
)

func TestManagerCapturesOutputExitAndWorkingDirectory(t *testing.T) {
	manager := newTestManager(t, Options{MaxEvents: 20})
	root := t.TempDir()
	started, err := manager.Start(context.Background(), StartRequest{
		WorkspaceID: "w1",
		SiteID:      "shop",
		TaskID:      "script:dev",
		Command:     helperCommand(root, "output"),
	})
	if err != nil {
		t.Fatal(err)
	}

	exited := waitForStatus(t, manager, started.ID, Exited)
	for _, text := range []string{"stdout line", "stderr line", filepath.Clean(root)} {
		if !hasEventText(exited.Events, text) {
			t.Fatalf("events do not contain %q: %#v", text, exited.Events)
		}
	}
	if exited.ExitCode == nil || *exited.ExitCode != 0 || exited.PID <= 0 {
		t.Fatalf("snapshot = %#v", exited)
	}
	encoded, err := json.Marshal(exited)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{os.Args[0], "-test.run"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("snapshot exposes command detail %q: %s", private, encoded)
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"command", "executable", "args", "dir"} {
		if _, exists := fields[field]; exists {
			t.Fatalf("snapshot exposes command field %q: %s", field, encoded)
		}
	}
}

func TestManagerRejectsDuplicateSiteProcessAndReleasesOwnership(t *testing.T) {
	manager := newTestManager(t, Options{})
	first, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "wait")})
	if err != nil {
		t.Fatal(err)
	}
	waitForEventText(t, manager, first.ID, "ready")

	_, err = manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "wait")})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("duplicate Start() error = %v", err)
	}
	var conflict ConflictError
	if !errors.As(err, &conflict) || conflict.ActiveProcessID != first.ID {
		t.Fatalf("conflict = %#v", err)
	}

	if err := manager.Stop(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, manager, first.ID, Stopped)
	second, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "exit")})
	if err != nil {
		t.Fatalf("Start() after stop error = %v", err)
	}
	waitForStatus(t, manager, second.ID, Exited)
}

func TestManagerFindsActiveProcessByOwner(t *testing.T) {
	manager := newTestManager(t, Options{})
	started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "wait")})
	if err != nil {
		t.Fatal(err)
	}
	waitForEventText(t, manager, started.ID, "ready")
	found, err := manager.Find("w", "s", "script:dev")
	if err != nil || found.ID != started.ID {
		t.Fatalf("Find() = %#v, %v", found, err)
	}
	if err := manager.Stop(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, manager, started.ID, Stopped)
	if _, err := manager.Find("w", "s", "script:dev"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find() after stop error = %v", err)
	}
}

func TestManagerInterruptsBeforeForceKilling(t *testing.T) {
	t.Run("graceful", func(t *testing.T) {
		manager := newTestManager(t, Options{StopGrace: time.Second})
		started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "graceful", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "wait")})
		if err != nil {
			t.Fatal(err)
		}
		waitForEventText(t, manager, started.ID, "ready")
		if err := manager.Stop(context.Background(), started.ID); err != nil {
			t.Fatal(err)
		}
		stopped := waitForStatus(t, manager, started.ID, Stopped)
		if !hasEventText(stopped.Events, "interrupted") || !hasEventCode(stopped.Events, "process.interrupt") {
			t.Fatalf("events = %#v", stopped.Events)
		}
	})

	t.Run("forced", func(t *testing.T) {
		grace := 80 * time.Millisecond
		manager := newTestManager(t, Options{StopGrace: grace})
		started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "forced", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "ignore")})
		if err != nil {
			t.Fatal(err)
		}
		waitForEventText(t, manager, started.ID, "ready")
		before := time.Now()
		if err := manager.Stop(context.Background(), started.ID); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(before); elapsed < grace/2 {
			snapshot, _ := manager.Get(started.ID)
			t.Fatalf("Stop() forced termination after %v, want grace near %v; snapshot=%#v", elapsed, grace, snapshot)
		}
		stopped := waitForStatus(t, manager, started.ID, Stopped)
		if !hasEventCode(stopped.Events, "process.kill") {
			t.Fatalf("events = %#v", stopped.Events)
		}
	})
}

func TestManagerRestartsWithOriginalResolvedCommand(t *testing.T) {
	manager := newTestManager(t, Options{})
	first, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "wait")})
	if err != nil {
		t.Fatal(err)
	}
	waitForEventText(t, manager, first.ID, "ready")

	restarted, err := manager.Restart(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.ID == first.ID || restarted.WorkspaceID != first.WorkspaceID || restarted.TaskID != first.TaskID {
		t.Fatalf("restarted = %#v, first = %#v", restarted, first)
	}
	waitForStatus(t, manager, first.ID, Stopped)
	waitForEventText(t, manager, restarted.ID, "ready")
	if err := manager.Stop(context.Background(), restarted.ID); err != nil {
		t.Fatal(err)
	}
}

func TestManagerExtractsOnlyAdvisoryLoopbackURLs(t *testing.T) {
	manager := newTestManager(t, Options{})
	started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "urls")})
	if err != nil {
		t.Fatal(err)
	}
	exited := waitForStatus(t, manager, started.ID, Exited)
	want := []string{"http://localhost:3000/", "https://127.0.0.1:4443/admin"}
	if fmt.Sprint(exited.URLs) != fmt.Sprint(want) {
		t.Fatalf("URLs = %#v, want %#v", exited.URLs, want)
	}
	encoded, _ := json.Marshal(exited)
	for _, secret := range []string{"url-password", "query-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("snapshot contains URL secret %q: %s", secret, encoded)
		}
	}
}

func TestManagerBoundsOutput(t *testing.T) {
	manager := newTestManager(t, Options{MaxEvents: 4})
	started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "chatty")})
	if err != nil {
		t.Fatal(err)
	}
	exited := waitForStatus(t, manager, started.ID, Exited)
	if len(exited.Events) != 4 {
		t.Fatalf("event count = %d, want 4", len(exited.Events))
	}
	for index := 1; index < len(exited.Events); index++ {
		if exited.Events[index].ID <= exited.Events[index-1].ID {
			t.Fatalf("event IDs are not monotonic: %#v", exited.Events)
		}
	}
}

func TestManagerRedactsOutputBeforeStorage(t *testing.T) {
	manager := newTestManager(t, Options{MaxEvents: 20, SecretKeys: []string{"DEPLOY_TOKEN"}})
	started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "chatty")})
	if err != nil {
		t.Fatal(err)
	}
	exited := waitForStatus(t, manager, started.ID, Exited)
	encoded, _ := json.Marshal(exited)
	for _, secret := range []string{"deploy-secret", "user:password", "bearer-secret", "cookie-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("snapshot contains %q: %s", secret, encoded)
		}
	}
}

func TestManagerMergesTrustedEnvironmentAndRedactsRuntimeSecrets(t *testing.T) {
	t.Setenv("CF_INHERITED_ENV", "inherited")
	environment := map[string]string{"CF_TEST_ENV": "preview"}
	secretValues := []string{"raw-preview-secret"}
	manager := newTestManager(t, Options{MaxEvents: 20})
	command := helperCommand(t.TempDir(), "environment")
	command.Environment = environment

	started, err := manager.Start(context.Background(), StartRequest{
		WorkspaceID:  "w",
		SiteID:       "environment",
		TaskID:       "component-preview",
		Command:      command,
		SecretValues: secretValues,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment["CF_TEST_ENV"] = "mutated"
	secretValues[0] = "mutated-secret"

	exited := waitForStatus(t, manager, started.ID, Exited)
	if !hasEventText(exited.Events, "preview inherited") {
		t.Fatalf("events do not contain merged environment: %#v", exited.Events)
	}
	encoded, _ := json.Marshal(exited)
	if strings.Contains(string(encoded), "raw-preview-secret") {
		t.Fatalf("snapshot exposes runtime secret: %s", encoded)
	}
	if !strings.Contains(string(encoded), "[redacted]") {
		t.Fatalf("snapshot does not contain redaction marker: %s", encoded)
	}

	restarted, err := manager.Restart(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	restarted = waitForStatus(t, manager, restarted.ID, Exited)
	if !hasEventText(restarted.Events, "preview inherited") || hasEventText(restarted.Events, "mutated inherited") {
		t.Fatalf("restart did not preserve cloned environment: %#v", restarted.Events)
	}
	encoded, _ = json.Marshal(restarted)
	if strings.Contains(string(encoded), "raw-preview-secret") {
		t.Fatalf("restarted snapshot exposes runtime secret: %s", encoded)
	}
}

func TestSubscribeReplaysAndStreamsProcessEvents(t *testing.T) {
	manager := newTestManager(t, Options{MaxEvents: 20})
	started, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "s", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "wait")})
	if err != nil {
		t.Fatal(err)
	}
	waitForEventText(t, manager, started.ID, "ready")

	events, unsubscribe, err := manager.Subscribe(started.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if event := receiveEvent(t, events); event.Code != "process.started" {
		t.Fatalf("first replay event = %#v", event)
	}
	if err := manager.Stop(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}
	if event := receiveUntilCode(t, events, "process.stopped"); event.ID == 0 {
		t.Fatalf("stopped event = %#v", event)
	}
}

func TestShutdownStopsAllChildrenAndRejectsNewProcesses(t *testing.T) {
	manager := newTestManager(t, Options{})
	ids := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		started, err := manager.Start(context.Background(), StartRequest{
			WorkspaceID: "w",
			SiteID:      fmt.Sprintf("site-%d", index),
			TaskID:      "script:dev",
			Command:     helperCommand(t.TempDir(), "wait"),
		})
		if err != nil {
			t.Fatal(err)
		}
		waitForEventText(t, manager, started.ID, "ready")
		ids = append(ids, started.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		waitForStatus(t, manager, id, Stopped)
	}
	_, err := manager.Start(context.Background(), StartRequest{WorkspaceID: "w", SiteID: "late", TaskID: "script:dev", Command: helperCommand(t.TempDir(), "exit")})
	if !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("late Start() error = %v", err)
	}
}

func TestProcessHelper(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	mode := os.Args[separator+1]
	switch mode {
	case "output":
		root, _ := os.Getwd()
		fmt.Fprintln(os.Stdout, "stdout line")
		fmt.Fprintln(os.Stderr, "stderr line")
		fmt.Fprintln(os.Stdout, root)
	case "exit":
		fmt.Fprintln(os.Stdout, "done")
	case "wait":
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		defer signal.Stop(interrupts)
		fmt.Fprintln(os.Stdout, "ready")
		<-interrupts
		fmt.Fprintln(os.Stdout, "interrupted")
	case "ignore":
		signal.Ignore(os.Interrupt)
		fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	case "urls":
		fmt.Fprintln(os.Stdout, "Local: http://localhost:3000/")
		fmt.Fprintln(os.Stdout, "Secure: https://127.0.0.1:4443/admin")
		fmt.Fprintln(os.Stdout, "Ignore: https://example.com and http://localhost.evil.test")
		fmt.Fprintln(os.Stdout, "Credentials: http://user:url-password@localhost:3333/private")
		fmt.Fprintln(os.Stdout, "Query: http://localhost:3001/callback?token=query-secret#fragment")
	case "chatty":
		fmt.Fprintln(os.Stdout, "DEPLOY_TOKEN=deploy-secret")
		fmt.Fprintln(os.Stdout, "https://user:password@example.test/repo")
		fmt.Fprintln(os.Stdout, "Authorization: Bearer bearer-secret")
		fmt.Fprintln(os.Stdout, "Cookie: session=cookie-secret")
		for index := 0; index < 8; index++ {
			fmt.Fprintf(os.Stdout, "line %d\n", index)
		}
	case "environment":
		fmt.Fprintf(
			os.Stdout,
			"values: %s %s raw-preview-secret\n",
			os.Getenv("CF_TEST_ENV"),
			os.Getenv("CF_INHERITED_ENV"),
		)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func helperCommand(root, mode string) project.Command {
	return project.Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$", "--", mode},
		Dir:        root,
	}
}

func newTestManager(t *testing.T, options Options) *Manager {
	t.Helper()
	counter := 0
	options.NewID = func() (string, error) {
		counter++
		return fmt.Sprintf("process-%d", counter), nil
	}
	manager := NewManager(options)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})
	return manager
}

func waitForStatus(t *testing.T, manager *Manager, id string, want Status) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status == want {
			return snapshot
		}
		time.Sleep(2 * time.Millisecond)
	}
	snapshot, _ := manager.Get(id)
	t.Fatalf("status = %q, want %q; snapshot=%#v", snapshot.Status, want, snapshot)
	return Snapshot{}
}

func waitForEventText(t *testing.T, manager *Manager, id, text string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if hasEventText(snapshot.Events, text) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("event text %q not reported", text)
}

func hasEventText(events []Event, text string) bool {
	for _, event := range events {
		if strings.Contains(event.Text, text) {
			return true
		}
	}
	return false
}

func hasEventCode(events []Event, code string) bool {
	for _, event := range events {
		if event.Code == code {
			return true
		}
	}
	return false
}

func receiveEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed")
		}
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func receiveUntilCode(t *testing.T, events <-chan Event, code string) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed before %q", code)
			}
			if event.Code == code {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %q", code)
		}
	}
}
