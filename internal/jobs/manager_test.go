package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/mutationlease"
)

func TestManagerCompletesJobAndStoresSafeEvents(t *testing.T) {
	manager := newTestManager(Options{MaxEvents: 20})

	started, err := manager.Start(Request{
		Type:        "site.create",
		WorkspaceID: "w1",
		SiteID:      "shop",
		Run: func(_ context.Context, reporter Reporter) error {
			reporter.Event("info", "clone.started", "Cloning starter")
			return nil
		},
	})

	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	completed := waitForStatus(t, manager, started.ID, Succeeded)
	if completed.Type != "site.create" || completed.WorkspaceID != "w1" || completed.SiteID != "shop" {
		t.Fatalf("snapshot = %#v", completed)
	}
	if completed.StartedAt.IsZero() || completed.CompletedAt.IsZero() {
		t.Fatalf("timestamps = %#v", completed)
	}
	if !hasEventCode(completed.Events, "clone.started") {
		t.Fatalf("events = %#v", completed.Events)
	}
}

func TestManagerStoresRedactedFailure(t *testing.T) {
	manager := newTestManager(Options{MaxEvents: 20, SecretKeys: []string{"DEPLOY_TOKEN"}})

	started, err := manager.Start(Request{
		Type:        "build",
		WorkspaceID: "w1",
		Run: func(_ context.Context, reporter Reporter) error {
			reporter.Event("error", "remote.failed", "DEPLOY_TOKEN=super-secret ftp://user:password@example.test/repo.git Authorization: Bearer bearer-secret Cookie: session=cookie-secret")
			return errors.New("request failed with DEPLOY_TOKEN=super-secret at https://user:password@example.test/repo.git\nAuthorization: Bearer bearer-secret\nSet-Cookie: session=set-cookie-secret")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	failed := waitForStatus(t, manager, started.ID, Failed)
	encoded, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"super-secret", "user:password", "bearer-secret", "cookie-secret", "set-cookie-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("snapshot contains %q: %s", forbidden, text)
		}
	}
	if failed.Error == nil || failed.Error.Code != "job_failed" {
		t.Fatalf("safe error = %#v", failed.Error)
	}
}

func TestManagerRecoversJobPanicWithoutStoppingOtherJobs(t *testing.T) {
	manager := newTestManager(Options{})
	panicked, err := manager.Start(Request{
		Type:        "panic",
		WorkspaceID: "w1",
		Run: func(context.Context, Reporter) error {
			panic("sensitive panic value")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForStatus(t, manager, panicked.ID, Failed)
	if failed.Error == nil || strings.Contains(failed.Error.Message, "sensitive") {
		t.Fatalf("panic error = %#v", failed.Error)
	}

	next, err := manager.Start(Request{Type: "next", WorkspaceID: "w1", Run: func(context.Context, Reporter) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, manager, next.ID, Succeeded)
}

func TestManagerCancelsRunningJob(t *testing.T) {
	manager := newTestManager(Options{})
	running := make(chan struct{})
	started, err := manager.Start(Request{
		Type:        "install",
		WorkspaceID: "w1",
		Run: func(ctx context.Context, _ Reporter) error {
			close(running)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-running

	if err := manager.Cancel(started.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitForStatus(t, manager, started.ID, Cancelled)
}

func TestManagerRejectsConcurrentWorkspaceMutationAndReleasesLock(t *testing.T) {
	manager := newTestManager(Options{})
	release := make(chan struct{})
	first, err := manager.Start(Request{
		Type:        "first",
		WorkspaceID: "w1",
		Run: func(context.Context, Reporter) error {
			<-release
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = manager.Start(Request{Type: "second", WorkspaceID: "w1", Run: func(context.Context, Reporter) error { return nil }})
	var conflict ConflictError
	if !errors.As(err, &conflict) || conflict.ActiveJobID != first.ID {
		t.Fatalf("Start() error = %#v", err)
	}

	other, err := manager.Start(Request{Type: "other", WorkspaceID: "w2", Run: func(context.Context, Reporter) error { return nil }})
	if err != nil {
		t.Fatalf("other workspace Start() error = %v", err)
	}
	waitForStatus(t, manager, other.ID, Succeeded)
	close(release)
	waitForStatus(t, manager, first.ID, Succeeded)

	third, err := manager.Start(Request{Type: "third", WorkspaceID: "w1", Run: func(context.Context, Reporter) error { return nil }})
	if err != nil {
		t.Fatalf("Start() after completion error = %v", err)
	}
	waitForStatus(t, manager, third.ID, Succeeded)
}

func TestManagerSharesWorkspaceLeaseWithAgentOwners(t *testing.T) {
	leases := mutationlease.NewManager()
	manager := newTestManager(Options{Leases: leases})
	releaseJob := make(chan struct{})
	started, err := manager.Start(Request{
		Type:        "first",
		WorkspaceID: "w1",
		Run: func(context.Context, Reporter) error {
			<-releaseJob
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = leases.Acquire("w1", "conversation-1", "agent")
	var conflict mutationlease.ConflictError
	if !errors.As(err, &conflict) || conflict.ActiveOwnerID != started.ID || conflict.ActiveOwnerType != "job" {
		t.Fatalf("agent Acquire() error = %#v", err)
	}

	close(releaseJob)
	waitForStatus(t, manager, started.ID, Succeeded)
	agentRelease, err := leases.Acquire("w1", "conversation-1", "agent")
	if err != nil {
		t.Fatalf("agent Acquire() after job error = %v", err)
	}
	agentRelease()
}

func TestManagerBoundsEventsAndKeepsMonotonicIDs(t *testing.T) {
	manager := newTestManager(Options{MaxEvents: 3})
	started, err := manager.Start(Request{
		Type:        "chatty",
		WorkspaceID: "w1",
		Run: func(_ context.Context, reporter Reporter) error {
			for index := 0; index < 8; index++ {
				reporter.Event("info", "line", fmt.Sprintf("line %d", index))
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForStatus(t, manager, started.ID, Succeeded)
	if len(completed.Events) != 3 {
		t.Fatalf("event count = %d, want 3", len(completed.Events))
	}
	for index := 1; index < len(completed.Events); index++ {
		if completed.Events[index].ID <= completed.Events[index-1].ID {
			t.Fatalf("event IDs are not monotonic: %#v", completed.Events)
		}
	}
}

func TestManagerBoundsCompletedJobHistory(t *testing.T) {
	manager := newTestManager(Options{MaxJobs: 2})
	ids := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		started, err := manager.Start(Request{
			Type:        fmt.Sprintf("job-%d", index),
			WorkspaceID: "w1",
			Run:         func(context.Context, Reporter) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		waitForStatus(t, manager, started.ID, Succeeded)
		ids = append(ids, started.ID)
	}
	if _, err := manager.Get(ids[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest Get() error = %v, want ErrNotFound", err)
	}
	for _, id := range ids[1:] {
		if _, err := manager.Get(id); err != nil {
			t.Fatalf("Get(%q) error = %v", id, err)
		}
	}
}

func TestSubscribeReplaysBufferedEventsThenStreamsLiveEvents(t *testing.T) {
	manager := newTestManager(Options{MaxEvents: 20})
	continueRun := make(chan struct{})
	firstReported := make(chan struct{})
	started, err := manager.Start(Request{
		Type:        "stream",
		WorkspaceID: "w1",
		Run: func(_ context.Context, reporter Reporter) error {
			reporter.Event("info", "phase.one", "first")
			close(firstReported)
			<-continueRun
			reporter.Event("info", "phase.two", "second")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-firstReported
	waitForEventCode(t, manager, started.ID, "phase.one")

	events, unsubscribe, err := manager.Subscribe(started.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if event := receiveEvent(t, events); event.Code != "job.started" {
		t.Fatalf("first replay event = %#v", event)
	}
	if event := receiveUntilCode(t, events, "phase.one"); event.Text != "first" {
		t.Fatalf("phase one event = %#v", event)
	}

	close(continueRun)
	if event := receiveUntilCode(t, events, "phase.two"); event.Text != "second" {
		t.Fatalf("phase two event = %#v", event)
	}
	waitForStatus(t, manager, started.ID, Succeeded)
}

func TestUnsubscribeDoesNotCancelRunningJob(t *testing.T) {
	manager := newTestManager(Options{})
	release := make(chan struct{})
	started, err := manager.Start(Request{Type: "tab-independent", WorkspaceID: "w1", Run: func(context.Context, Reporter) error {
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, unsubscribe, err := manager.Subscribe(started.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	unsubscribe()
	snapshot, err := manager.Get(started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != Running && snapshot.Status != Queued {
		t.Fatalf("status after tab disconnect = %q", snapshot.Status)
	}
	close(release)
	waitForStatus(t, manager, started.ID, Succeeded)
}

func TestSecretHandoffIsSessionScopedAndConsumedOnce(t *testing.T) {
	manager := newTestManager(Options{})
	started, err := manager.Start(Request{
		Type:        "couchdb.install",
		WorkspaceID: "w1",
		SessionID:   "browser-a",
		Run: func(_ context.Context, reporter Reporter) error {
			reporter.Secret("generated-password")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForStatus(t, manager, started.ID, Succeeded)
	encoded, _ := json.Marshal(completed)
	if strings.Contains(string(encoded), "generated-password") {
		t.Fatalf("snapshot contains secret: %s", encoded)
	}

	if _, err := manager.ConsumeSecret(started.ID, "browser-b"); !errors.Is(err, ErrSecretForbidden) {
		t.Fatalf("foreign session error = %v", err)
	}
	secret, err := manager.ConsumeSecret(started.ID, "browser-a")
	if err != nil || secret != "generated-password" {
		t.Fatalf("ConsumeSecret() = %q, %v", secret, err)
	}
	if _, err := manager.ConsumeSecret(started.ID, "browser-a"); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("second consume error = %v", err)
	}
}

func TestShutdownCancelsJobsAndRejectsNewWork(t *testing.T) {
	manager := newTestManager(Options{})
	running := make(chan struct{})
	started, err := manager.Start(Request{
		Type:        "long",
		WorkspaceID: "w1",
		Run: func(ctx context.Context, _ Reporter) error {
			close(running)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-running

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	waitForStatus(t, manager, started.ID, Cancelled)
	if _, err := manager.Start(Request{Type: "late", WorkspaceID: "w2", Run: func(context.Context, Reporter) error { return nil }}); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("late Start() error = %v", err)
	}
}

func newTestManager(options Options) *Manager {
	counter := 0
	options.NewID = func() (string, error) {
		counter++
		return fmt.Sprintf("job-%d", counter), nil
	}
	options.Now = func() time.Time { return time.Now().UTC() }
	return NewManager(options)
}

func waitForStatus(t *testing.T, manager *Manager, id string, want Status) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Get(id)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
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

func waitForEventCode(t *testing.T, manager *Manager, id, code string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if hasEventCode(snapshot.Events, code) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("event code %q not reported", code)
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
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func receiveUntilCode(t *testing.T, events <-chan Event, code string) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
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
