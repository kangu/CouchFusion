package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreReturnsDefaultStateWhenMissing(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state", "onboarding.json")}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != CurrentVersion || state.Status != StatusNotStarted {
		t.Fatalf("state = %#v", state)
	}
}

func TestStorePersistsPrivateVersionedStateAtomically(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".couchfusion", "onboarding.json")
	store := &Store{Path: path}
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	state := State{Version: CurrentVersion, Status: StatusActive, WorkspaceID: "workspace-1", UpdatedAt: now}
	if err := store.Save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WorkspaceID != state.WorkspaceID || !loaded.UpdatedAt.Equal(now) {
		t.Fatalf("loaded = %#v", loaded)
	}
	for _, path := range []string{filepath.Dir(path), path} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o700)
		if path == store.Path {
			want = 0o600
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".onboarding-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %#v, error = %v", matches, err)
	}
}

func TestApplyClearsDependentStateAndCompletesOnlyAfterPreview(t *testing.T) {
	start := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	state, err := Apply(State{}, Action{Type: ActionSelectWorkspace, ResourceID: "workspace-1"}, start)
	if err != nil {
		t.Fatal(err)
	}
	state, _ = Apply(state, Action{Type: ActionSelectSite, ResourceID: "site-1"}, start.Add(time.Minute))
	state, _ = Apply(state, Action{Type: ActionSelectComponent, ResourceID: "hero"}, start.Add(2*time.Minute))
	state, _ = Apply(state, Action{Type: ActionTrackJob, ResourceID: "job-1"}, start.Add(3*time.Minute))
	if _, err := Apply(state, Action{Type: ActionConfirmPlacement}, start.Add(4*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("confirm placement error = %v", err)
	}
	state, err = Apply(state, Action{Type: ActionConfirmPreview}, start.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	state, err = Apply(state, Action{Type: ActionConfirmPlacement}, start.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusCompleted || state.CompletedAt == nil || state.PlacementConfirmedAt == nil {
		t.Fatalf("completed state = %#v", state)
	}

	if _, err = Apply(state, Action{Type: ActionSelectWorkspace, ResourceID: "workspace-2"}, start.Add(6*time.Minute)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("completed journey mutation error = %v", err)
	}
}

func TestApplyUpstreamSelectionClearsDependentState(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	state, _ := Apply(State{}, Action{Type: ActionSelectWorkspace, ResourceID: "workspace-1"}, now)
	state, _ = Apply(state, Action{Type: ActionSelectSite, ResourceID: "site-1"}, now)
	state, _ = Apply(state, Action{Type: ActionSelectComponent, ResourceID: "hero"}, now)
	state, _ = Apply(state, Action{Type: ActionConfirmPreview}, now)
	state, _ = Apply(state, Action{Type: ActionSelectWorkspace, ResourceID: "workspace-2"}, now)
	if state.Status != StatusActive || state.SiteID != "" || state.ComponentID != "" || state.ActiveJobID != "" || state.PreviewConfirmedAt != nil {
		t.Fatalf("upstream selection did not clear dependent state: %#v", state)
	}
}

func TestApplyPauseResumeClearAndReset(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	state, _ := Apply(State{}, Action{Type: ActionTrackJob, ResourceID: "job-1"}, now)
	state, _ = Apply(state, Action{Type: ActionPause}, now.Add(time.Minute))
	if state.Status != StatusPaused || state.PausedAt == nil {
		t.Fatalf("paused state = %#v", state)
	}
	state, _ = Apply(state, Action{Type: ActionResume}, now.Add(2*time.Minute))
	if state.Status != StatusActive || state.PausedAt != nil {
		t.Fatalf("resumed state = %#v", state)
	}
	state, _ = Apply(state, Action{Type: ActionClearJob}, now.Add(3*time.Minute))
	if state.ActiveJobID != "" {
		t.Fatalf("job was not cleared: %#v", state)
	}
	state, _ = Apply(state, Action{Type: ActionReset}, now.Add(4*time.Minute))
	if state.Version != CurrentVersion || state.Status != StatusNotStarted || !state.UpdatedAt.Equal(now.Add(4*time.Minute)) {
		t.Fatalf("reset state = %#v", state)
	}
}

func TestStoreRejectsCorruptAndUnsupportedDocuments(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    error
	}{
		{name: "corrupt", content: "{", want: ErrInvalidDocument},
		{name: "unsupported", content: "{\"version\":2,\"status\":\"active\"}", want: ErrUnsupportedVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "onboarding.json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			store := &Store{Path: path}
			if _, err := store.Load(context.Background()); !errors.Is(err, test.want) {
				t.Fatalf("Load() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStateJSONDoesNotContainSensitiveOrMachineSpecificFields(t *testing.T) {
	raw, err := json.Marshal(State{Version: CurrentVersion, Status: StatusActive, WorkspaceID: "w", SiteID: "s", ComponentID: "hero"})
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"path", "prompt", "logs", "processId", "credentials", "command"} {
		if _, found := values[forbidden]; found {
			t.Fatalf("state exposed forbidden field %q: %s", forbidden, raw)
		}
	}
}
