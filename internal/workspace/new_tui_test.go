package workspace

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/ui"
)

func TestNewAppRequiresAndPropagatesDatabasePrefix(t *testing.T) {
	cfg := &config.Config{Modules: map[string]config.ModuleConfig{}}
	var receivedPrefix string
	model := newAppModelWithOperation(context.Background(), cfg, "", nil, "", "main", false, ui.NewLogBuffer(20), func(_ context.Context, _ string, _ []string, _ string, _ bool, prefix, _, _ string, _ *ui.LogBuffer) error {
		receivedPrefix = prefix
		return nil
	})

	if model.prefixInput.Value() != "" {
		t.Fatalf("initial prefix = %q, want blank", model.prefixInput.Value())
	}
	if model.prefixInput.CharLimit != 2 {
		t.Fatalf("prefix character limit = %d, want 2", model.prefixInput.CharLimit)
	}
	model.nameField = 1
	model.focusNameField()
	model.updateNameStep(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if model.aborted || model.prefixInput.Value() != "q" {
		t.Fatalf("typing q aborted=%v prefix=%q, want ordinary text input", model.aborted, model.prefixInput.Value())
	}

	model.nameInput.SetValue("catalog")
	model.prefixInput.SetValue("A1")
	model.updateNameStep(tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != stepName {
		t.Fatalf("step = %v, want name step after invalid prefix", model.step)
	}

	model.prefixInput.SetValue("ac")
	model.updateNameStep(tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != stepModules {
		t.Fatalf("step = %v, want modules step", model.step)
	}

	model.modules = []string{}
	model.step = stepSummary
	if summary := model.viewSummaryStep(); !strings.Contains(summary, "Database") || !strings.Contains(summary, "ac-") {
		t.Fatalf("summary does not show database prefix: %q", summary)
	}
	message := model.runCreateCmd()()
	if result, ok := message.(newAppResultMsg); !ok || result.err != nil {
		t.Fatalf("message = %#v", message)
	}
	if receivedPrefix != "ac" {
		t.Fatalf("operation prefix = %q, want ac", receivedPrefix)
	}
}
