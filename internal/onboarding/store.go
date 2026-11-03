package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const CurrentVersion = 1

var (
	ErrInvalidDocument    = errors.New("onboarding document is invalid")
	ErrUnsupportedVersion = errors.New("onboarding document version is unsupported")
	ErrInvalidTransition  = errors.New("onboarding transition is invalid")
)

type Status string

const (
	StatusNotStarted Status = "not_started"
	StatusActive     Status = "active"
	StatusPaused     Status = "paused"
	StatusCompleted  Status = "completed"
)

type ActionType string

const (
	ActionSelectWorkspace  ActionType = "select_workspace"
	ActionSelectSite       ActionType = "select_site"
	ActionSelectComponent  ActionType = "select_component"
	ActionTrackJob         ActionType = "track_job"
	ActionClearJob         ActionType = "clear_job"
	ActionConfirmPreview   ActionType = "confirm_preview"
	ActionConfirmPlacement ActionType = "confirm_placement"
	ActionPause            ActionType = "pause"
	ActionResume           ActionType = "resume"
	ActionReset            ActionType = "reset"
)

type Action struct {
	Type       ActionType `json:"action"`
	ResourceID string     `json:"resourceId,omitempty"`
}

type State struct {
	Version              int        `json:"version"`
	Status               Status     `json:"status"`
	WorkspaceID          string     `json:"workspaceId,omitempty"`
	SiteID               string     `json:"siteId,omitempty"`
	ComponentID          string     `json:"componentId,omitempty"`
	ActiveJobID          string     `json:"activeJobId,omitempty"`
	PreviewConfirmedAt   *time.Time `json:"previewConfirmedAt,omitempty"`
	PlacementConfirmedAt *time.Time `json:"placementConfirmedAt,omitempty"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	PausedAt             *time.Time `json:"pausedAt,omitempty"`
	CompletedAt          *time.Time `json:"completedAt,omitempty"`
	UpdatedAt            time.Time  `json:"updatedAt,omitempty"`
}

type Store struct {
	Path string
	mu   sync.Mutex
}

func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("determine home directory: %w", err)
	}
	return &Store{Path: filepath.Join(home, ".couchfusion", "onboarding.json")}, nil
}

func DefaultState() State {
	return State{Version: CurrentVersion, Status: StatusNotStarted}
}

func (s *Store) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (State, error) {
	if strings.TrimSpace(s.Path) == "" {
		return State{}, fmt.Errorf("%w: store path is empty", ErrInvalidDocument)
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read onboarding state: %w", err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if state.Version != CurrentVersion {
		return State{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, state.Version, CurrentVersion)
	}
	if !validStatus(state.Status) {
		return State{}, fmt.Errorf("%w: unknown status %q", ErrInvalidDocument, state.Status)
	}
	return state, nil
}

func (s *Store) Save(ctx context.Context, state State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("%w: store path is empty", ErrInvalidDocument)
	}
	if state.Version == 0 {
		state.Version = CurrentVersion
	}
	if state.Version != CurrentVersion {
		return ErrUnsupportedVersion
	}
	if !validStatus(state.Status) {
		return fmt.Errorf("%w: unknown status %q", ErrInvalidDocument, state.Status)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode onboarding state: %w", err)
	}
	raw = append(raw, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	parent := filepath.Dir(s.Path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create onboarding state directory: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("secure onboarding state directory: %w", err)
	}
	temporary, err := os.CreateTemp(parent, ".onboarding-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary onboarding state: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary onboarding state: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary onboarding state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary onboarding state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary onboarding state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.Path); err != nil {
		return fmt.Errorf("replace onboarding state: %w", err)
	}
	removeTemporary = false
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return fmt.Errorf("secure onboarding state: %w", err)
	}
	return nil
}

func Apply(state State, action Action, now time.Time) (State, error) {
	if state.Version == 0 {
		state = DefaultState()
	}
	if state.Version != CurrentVersion || !validStatus(state.Status) {
		return State{}, ErrInvalidDocument
	}
	now = now.UTC()
	if action.Type == ActionReset {
		state = DefaultState()
		state.UpdatedAt = now
		return state, nil
	}
	if state.Status == StatusCompleted {
		return State{}, ErrInvalidTransition
	}
	requireID := func() (string, error) {
		value := strings.TrimSpace(action.ResourceID)
		if value == "" {
			return "", ErrInvalidTransition
		}
		return value, nil
	}
	activate := func() {
		if state.StartedAt == nil {
			value := now
			state.StartedAt = &value
		}
		state.Status = StatusActive
		state.PausedAt = nil
		state.CompletedAt = nil
		state.PlacementConfirmedAt = nil
	}

	switch action.Type {
	case ActionSelectWorkspace:
		value, err := requireID()
		if err != nil {
			return State{}, err
		}
		activate()
		state.WorkspaceID = value
		state.SiteID = ""
		state.ComponentID = ""
		state.ActiveJobID = ""
		state.PreviewConfirmedAt = nil
	case ActionSelectSite:
		value, err := requireID()
		if err != nil || state.WorkspaceID == "" {
			return State{}, ErrInvalidTransition
		}
		activate()
		state.SiteID = value
		state.ComponentID = ""
		state.ActiveJobID = ""
		state.PreviewConfirmedAt = nil
	case ActionSelectComponent:
		value, err := requireID()
		if err != nil || state.SiteID == "" {
			return State{}, ErrInvalidTransition
		}
		activate()
		state.ComponentID = value
		state.ActiveJobID = ""
		state.PreviewConfirmedAt = nil
	case ActionTrackJob:
		value, err := requireID()
		if err != nil {
			return State{}, err
		}
		activate()
		state.ActiveJobID = value
	case ActionClearJob:
		activate()
		state.ActiveJobID = ""
	case ActionConfirmPreview:
		if state.ComponentID == "" {
			return State{}, ErrInvalidTransition
		}
		activate()
		value := now
		state.PreviewConfirmedAt = &value
	case ActionConfirmPlacement:
		if state.ComponentID == "" || state.PreviewConfirmedAt == nil {
			return State{}, ErrInvalidTransition
		}
		value := now
		state.Status = StatusCompleted
		state.PausedAt = nil
		state.PlacementConfirmedAt = &value
		state.CompletedAt = &value
		state.ActiveJobID = ""
	case ActionPause:
		if state.Status == StatusCompleted {
			return State{}, ErrInvalidTransition
		}
		if state.StartedAt == nil {
			value := now
			state.StartedAt = &value
		}
		value := now
		state.Status = StatusPaused
		state.PausedAt = &value
	case ActionResume:
		if state.Status != StatusPaused {
			return State{}, ErrInvalidTransition
		}
		state.Status = StatusActive
		state.PausedAt = nil
	default:
		return State{}, ErrInvalidTransition
	}
	state.UpdatedAt = now
	return state, nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusNotStarted, StatusActive, StatusPaused, StatusCompleted:
		return true
	default:
		return false
	}
}
