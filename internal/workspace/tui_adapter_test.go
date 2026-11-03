package workspace

import (
	"context"
	"testing"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/ui"
)

func TestInitTUIRunsInjectedApplicationOperation(t *testing.T) {
	called := false
	model := newInitModelWithOperation(context.Background(), &config.Config{}, "/workspace", "preview", true, ui.NewLogBuffer(20), func(_ context.Context, path, branch string, force bool, _ *ui.LogBuffer) error {
		called = path == "/workspace" && branch == "preview" && force
		return nil
	})
	message := model.runInitCmd()()
	if result, ok := message.(initResultMsg); !ok || result.err != nil || !called {
		t.Fatalf("message = %#v, called = %v", message, called)
	}
}

func TestNewSiteTUIRunsInjectedApplicationOperation(t *testing.T) {
	called := false
	cfg := &config.Config{Modules: map[string]config.ModuleConfig{}, Prompts: config.PromptConfig{}}
	model := newAppModelWithOperation(context.Background(), cfg, "shop", []string{"content"}, "ac", "preview", true, ui.NewLogBuffer(20), func(_ context.Context, name string, modules []string, branch string, force bool, prefix, _, _ string, _ *ui.LogBuffer) error {
		called = name == "shop" && len(modules) == 1 && modules[0] == "content" && prefix == "ac" && branch == "preview" && force
		return nil
	})
	model.appName = "shop"
	model.modules = []string{"content"}
	message := model.runCreateCmd()()
	if result, ok := message.(newAppResultMsg); !ok || result.err != nil || !called {
		t.Fatalf("message = %#v, called = %v", message, called)
	}
}

func TestCreateLayerTUIRunsInjectedApplicationOperation(t *testing.T) {
	called := false
	model := newCreateLayerModelWithOperation(context.Background(), &config.Config{}, "content", "preview", true, ui.NewLogBuffer(20), func(_ context.Context, name, branch string, force bool, _ *ui.LogBuffer) error {
		called = name == "content" && branch == "preview" && force
		return nil
	})
	model.layer = "content"
	message := model.runCreateLayerCmd()()
	if result, ok := message.(layerResultMsg); !ok || result.err != nil || !called {
		t.Fatalf("message = %#v, called = %v", message, called)
	}
}
