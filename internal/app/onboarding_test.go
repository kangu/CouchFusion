package app

import (
	"context"
	"testing"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/onboarding"
	"github.com/nuxt-apps/couchfusion/internal/preview"
	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
	"github.com/nuxt-apps/couchfusion/internal/registry"
)

func TestUpdateOnboardingValidatesAndPersistsJourneySelections(t *testing.T) {
	store := &onboardingStoreFake{state: onboarding.DefaultState()}
	agent := &agentFake{conversations: []model.Conversation{{
		ID: "conversation-1", WorkspaceID: "w1", SiteID: "shop", Status: model.ConversationReady,
		GeneratedComponentID: "hero", Registry: model.RegistryResult{State: model.RegistrySucceeded, GeneratedComponentID: "hero"},
		CreatedAt: time.Now().Add(time.Hour),
	}}}
	service := newServiceForTest(t, Dependencies{
		Onboarding: store,
		Registry:   &registryFake{entries: []registry.Entry{{ID: "w1", Root: "/workspace", Available: true}}},
		Projects:   &projectFake{site: project.Site{ID: "shop", Root: "/workspace/apps/shop", Modules: []string{"content", "auth"}}},
		Tasks: &taskFake{tasks: []project.Task{
			{ID: "script:dev", LongRunning: true}, {ID: "script:registry:build", LongRunning: false},
		}},
		Components:         &componentServiceFake{registry: project.ComponentRegistry{Status: project.ComponentRegistryReady, Components: []project.ComponentDefinition{{ID: "hero"}}}},
		PreviewHosts:       &previewHostFake{view: preview.View{Status: preview.Ready, URL: "http://127.0.0.1:3001/"}},
		AgentConversations: agent,
	})

	for _, action := range []onboarding.Action{
		{Type: onboarding.ActionSelectWorkspace, ResourceID: "w1"},
		{Type: onboarding.ActionSelectSite, ResourceID: "shop"},
		{Type: onboarding.ActionSelectComponent, ResourceID: "hero"},
		{Type: onboarding.ActionConfirmPreview},
		{Type: onboarding.ActionConfirmPlacement},
	} {
		if _, err := service.UpdateOnboarding(context.Background(), action); err != nil {
			t.Fatalf("UpdateOnboarding(%s) error = %v", action.Type, err)
		}
	}
	if store.state.Status != onboarding.StatusCompleted || store.saves != 5 {
		t.Fatalf("persisted state = %#v, saves = %d", store.state, store.saves)
	}
}

func TestUpdateOnboardingRejectsInvalidResourcesAndPrerequisites(t *testing.T) {
	tests := []struct {
		name   string
		state  onboarding.State
		action onboarding.Action
		deps   Dependencies
	}{
		{
			name: "unavailable workspace", state: onboarding.DefaultState(),
			action: onboarding.Action{Type: onboarding.ActionSelectWorkspace, ResourceID: "w1"},
			deps:   Dependencies{Registry: &registryFake{entries: []registry.Entry{{ID: "w1", Available: false}}}},
		},
		{
			name: "site without auth", state: onboarding.State{Version: 1, Status: onboarding.StatusActive, WorkspaceID: "w1"},
			action: onboarding.Action{Type: onboarding.ActionSelectSite, ResourceID: "shop"},
			deps: Dependencies{
				Registry: &registryFake{entries: []registry.Entry{{ID: "w1", Available: true}}},
				Projects: &projectFake{site: project.Site{ID: "shop", Modules: []string{"content"}}},
				Tasks:    &taskFake{tasks: []project.Task{{ID: "script:dev", LongRunning: true}, {ID: "script:registry:build"}}},
			},
		},
		{
			name: "component not generated", state: onboarding.State{Version: 1, Status: onboarding.StatusActive, WorkspaceID: "w1", SiteID: "shop"},
			action: onboarding.Action{Type: onboarding.ActionSelectComponent, ResourceID: "hero"},
			deps:   validOnboardingDependencies(nil),
		},
		{
			name: "component generated before journey", state: onboarding.State{Version: 1, Status: onboarding.StatusActive, WorkspaceID: "w1", SiteID: "shop", StartedAt: timePointer(time.Now())},
			action: onboarding.Action{Type: onboarding.ActionSelectComponent, ResourceID: "hero"},
			deps:   validOnboardingDependencies([]model.Conversation{{Status: model.ConversationReady, WorkspaceID: "w1", SiteID: "shop", GeneratedComponentID: "hero", Registry: model.RegistryResult{State: model.RegistrySucceeded}, CreatedAt: time.Now().Add(-time.Hour)}}),
		},
		{
			name: "preview not ready", state: onboarding.State{Version: 1, Status: onboarding.StatusActive, WorkspaceID: "w1", SiteID: "shop", ComponentID: "hero"},
			action: onboarding.Action{Type: onboarding.ActionConfirmPreview},
			deps:   validOnboardingDependencies([]model.Conversation{{Status: model.ConversationReady, WorkspaceID: "w1", SiteID: "shop", GeneratedComponentID: "hero", Registry: model.RegistryResult{State: model.RegistrySucceeded}}}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &onboardingStoreFake{state: test.state}
			test.deps.Onboarding = store
			service := newServiceForTest(t, test.deps)
			if _, err := service.UpdateOnboarding(context.Background(), test.action); errorCode(err) != ErrorValidation {
				t.Fatalf("error = %v", err)
			}
			if store.saves != 0 {
				t.Fatalf("invalid state was saved")
			}
		})
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestUpdateOnboardingTracksOnlyExistingJob(t *testing.T) {
	store := &onboardingStoreFake{state: onboarding.DefaultState()}
	service := newServiceForTest(t, Dependencies{Onboarding: store})
	if _, err := service.UpdateOnboarding(context.Background(), onboarding.Action{Type: onboarding.ActionTrackJob, ResourceID: "missing"}); errorCode(err) != ErrorValidation {
		t.Fatalf("error = %v", err)
	}
}

func TestSiteProcessFindsManagedOwner(t *testing.T) {
	manager := &processManagerFake{found: processes.Snapshot{ID: "p1", WorkspaceID: "w1", SiteID: "shop", TaskID: "script:dev", Status: processes.Running}}
	service := newServiceForTest(t, Dependencies{Processes: manager})
	found, err := service.SiteProcess(context.Background(), "w1", "shop", "script:dev")
	if err != nil || found.ID != "p1" || manager.findCalls != 1 {
		t.Fatalf("SiteProcess() = %#v, %v", found, err)
	}
}

func validOnboardingDependencies(conversations []model.Conversation) Dependencies {
	return Dependencies{
		Registry:           &registryFake{entries: []registry.Entry{{ID: "w1", Available: true}}},
		Projects:           &projectFake{site: project.Site{ID: "shop", Modules: []string{"content", "auth"}}},
		Tasks:              &taskFake{tasks: []project.Task{{ID: "script:dev", LongRunning: true}, {ID: "script:registry:build"}}},
		Components:         &componentServiceFake{registry: project.ComponentRegistry{Status: project.ComponentRegistryReady, Components: []project.ComponentDefinition{{ID: "hero"}}}},
		AgentConversations: &agentFake{conversations: conversations},
		PreviewHosts:       &previewHostFake{view: preview.View{Status: preview.Starting}},
	}
}

type onboardingStoreFake struct {
	state onboarding.State
	err   error
	saves int
}

func (f *onboardingStoreFake) Load(context.Context) (onboarding.State, error) { return f.state, f.err }
func (f *onboardingStoreFake) Save(_ context.Context, state onboarding.State) error {
	if f.err != nil {
		return f.err
	}
	f.state = state
	f.saves++
	return nil
}
