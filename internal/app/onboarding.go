package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/jobs"
	"github.com/nuxt-apps/couchfusion/internal/onboarding"
	"github.com/nuxt-apps/couchfusion/internal/preview"
	"github.com/nuxt-apps/couchfusion/internal/project"
)

func (s *Service) Onboarding(ctx context.Context) (onboarding.State, error) {
	state, err := s.dependencies.Onboarding.Load(ctx)
	return state, MapError(err)
}

func (s *Service) UpdateOnboarding(ctx context.Context, action onboarding.Action) (onboarding.State, error) {
	s.onboardingMu.Lock()
	defer s.onboardingMu.Unlock()
	state, err := s.dependencies.Onboarding.Load(ctx)
	if err != nil {
		return onboarding.State{}, MapError(err)
	}
	if err := s.validateOnboardingAction(ctx, state, action); err != nil {
		return onboarding.State{}, err
	}
	next, err := onboarding.Apply(state, action, time.Now().UTC())
	if err != nil {
		return onboarding.State{}, ValidationError("action", "The onboarding action is not valid for the current journey")
	}
	if err := s.dependencies.Onboarding.Save(ctx, next); err != nil {
		return onboarding.State{}, MapError(err)
	}
	return next, nil
}

func (s *Service) validateOnboardingAction(ctx context.Context, state onboarding.State, action onboarding.Action) error {
	switch action.Type {
	case onboarding.ActionSelectWorkspace:
		entry, err := s.workspaceEntry(ctx, action.ResourceID)
		if err != nil {
			return ValidationError("resourceId", "Select a registered workspace")
		}
		if !entry.Available {
			return ValidationError("resourceId", "The selected workspace is unavailable")
		}
	case onboarding.ActionSelectSite:
		if err := s.validateOnboardingSite(ctx, state.WorkspaceID, action.ResourceID); err != nil {
			return err
		}
	case onboarding.ActionSelectComponent:
		if err := s.validateOnboardingComponent(ctx, state.WorkspaceID, state.SiteID, action.ResourceID, state.StartedAt); err != nil {
			return err
		}
	case onboarding.ActionTrackJob:
		if _, err := s.dependencies.Jobs.Get(strings.TrimSpace(action.ResourceID)); err != nil {
			if errors.Is(err, jobs.ErrNotFound) {
				return ValidationError("resourceId", "The tracked job no longer exists")
			}
			return MapError(err)
		}
	case onboarding.ActionConfirmPreview:
		if state.WorkspaceID == "" || state.SiteID == "" || state.ComponentID == "" {
			return ValidationError("action", "Select a generated component before confirming its preview")
		}
		if err := s.validateOnboardingComponent(ctx, state.WorkspaceID, state.SiteID, state.ComponentID, state.StartedAt); err != nil {
			return err
		}
		view, err := s.dependencies.PreviewHosts.Status(ctx, state.WorkspaceID, state.SiteID)
		if err != nil || view.Status != preview.Ready {
			return ValidationError("action", "The component preview host must be ready")
		}
	case onboarding.ActionConfirmPlacement:
		if state.PreviewConfirmedAt == nil {
			return ValidationError("action", "Confirm the component preview before placement")
		}
	case onboarding.ActionClearJob, onboarding.ActionPause, onboarding.ActionResume, onboarding.ActionReset:
		// Pure state transitions are validated by onboarding.Apply.
	default:
		return ValidationError("action", "Unknown onboarding action")
	}
	return nil
}

func (s *Service) validateOnboardingSite(ctx context.Context, workspaceID, siteID string) error {
	entry, err := s.workspaceEntry(ctx, workspaceID)
	if err != nil || !entry.Available {
		return ValidationError("workspaceId", "Select an available workspace first")
	}
	site, err := s.dependencies.Projects.Site(ctx, entry.Root, siteID)
	if err != nil || site.ID != strings.TrimSpace(siteID) {
		return ValidationError("resourceId", "Select a site that belongs to this workspace")
	}
	modules := make(map[string]bool, len(site.Modules))
	for _, module := range site.Modules {
		modules[strings.ToLower(strings.TrimSpace(module))] = true
	}
	if !modules["content"] || !modules["auth"] {
		return ValidationError("resourceId", "Onboarding sites require the Content and Auth modules")
	}
	hasDev, hasRegistry := false, false
	for _, task := range s.dependencies.Tasks.Tasks(site) {
		switch {
		case task.ID == "script:dev" && task.LongRunning:
			hasDev = true
		case task.ID == "script:registry:build" && !task.LongRunning:
			hasRegistry = true
		}
	}
	if !hasDev || !hasRegistry {
		return ValidationError("resourceId", "Onboarding sites require dev and component-registry build scripts")
	}
	return nil
}

func (s *Service) validateOnboardingComponent(ctx context.Context, workspaceID, siteID, componentID string, journeyStartedAt *time.Time) error {
	if err := s.validateOnboardingSite(ctx, workspaceID, siteID); err != nil {
		return err
	}
	site, err := s.Site(ctx, workspaceID, siteID)
	if err != nil {
		return ValidationError("siteId", "The selected site is unavailable")
	}
	registry := s.dependencies.Components.Inspect(ctx, site.Root)
	if registry.Status != project.ComponentRegistryReady {
		return ValidationError("resourceId", "The component registry is not ready")
	}
	found := false
	for _, component := range registry.Components {
		if component.ID == componentID {
			found = true
			break
		}
	}
	if !found {
		return ValidationError("resourceId", "The generated component is not in the registry")
	}
	conversations, err := s.dependencies.AgentConversations.ListSite(ctx, workspaceID, siteID)
	if err != nil {
		return ValidationError("resourceId", "Component generation history is unavailable")
	}
	for _, conversation := range conversations {
		if conversation.WorkspaceID == workspaceID && conversation.SiteID == siteID &&
			conversation.Status == model.ConversationReady && conversation.GeneratedComponentID == componentID &&
			conversation.Registry.State == model.RegistrySucceeded && journeyStartedAt != nil && !conversation.CreatedAt.Before(*journeyStartedAt) {
			return nil
		}
	}
	return ValidationError("resourceId", "Select a component generated successfully for this journey")
}

type memoryOnboardingStore struct {
	mu    sync.Mutex
	state onboarding.State
}

func (s *memoryOnboardingStore) Load(ctx context.Context) (onboarding.State, error) {
	if err := ctx.Err(); err != nil {
		return onboarding.State{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, nil
}

func (s *memoryOnboardingStore) Save(ctx context.Context, state onboarding.State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	return nil
}
