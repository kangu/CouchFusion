package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/gitutil"
	"github.com/nuxt-apps/couchfusion/internal/ui"
)

type layerStep int

const (
	layerStepName layerStep = iota
	layerStepBranch
	layerStepSummary
	layerStepRunning
	layerStepDone
	layerStepError
)

type layerResultMsg struct {
	err error
}

type createLayerModel struct {
	ctx  context.Context
	cfg  *config.Config
	logs *ui.LogBuffer

	nameInput   textinput.Model
	branchInput textinput.Model
	force       bool

	spinner spinner.Model
	step    layerStep
	err     error
	aborted bool
	done    bool
	layer   string
}

func newCreateLayerModel(ctx context.Context, cfg *config.Config, nameHint, branchHint string, force bool, logs *ui.LogBuffer) *createLayerModel {
	name := textinput.New()
	name.Placeholder = "layer-name"
	name.CharLimit = 64
	name.Prompt = ""
	name.SetValue(nameHint)
	name.Focus()

	branch := textinput.New()
	branch.Placeholder = "(config default)"
	branch.CharLimit = 128
	branch.Prompt = ""
	branch.SetValue(branchHint)

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(ui.PrimaryLight)

	return &createLayerModel{
		ctx:         ctx,
		cfg:         cfg,
		logs:        logs,
		nameInput:   name,
		branchInput: branch,
		force:       force,
		spinner:     spin,
		step:        layerStepName,
	}
}

func (m *createLayerModel) Init() tea.Cmd {
	return nil
}

func (m *createLayerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case layerStepName:
			return m.updateName(msg)
		case layerStepBranch:
			return m.updateBranch(msg)
		case layerStepSummary:
			return m.updateSummary(msg)
		case layerStepRunning:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				m.aborted = true
				return m, tea.Quit
			}
		case layerStepDone, layerStepError:
			if msg.String() == "enter" || msg.String() == "q" {
				return m, tea.Quit
			}
		}
	case spinner.TickMsg:
		if m.step == layerStepRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case layerResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.logs.Errorf("Layer creation failed: %v", msg.err)
			m.step = layerStepError
			return m, nil
		}
		m.done = true
		m.step = layerStepDone
		m.logs.Successf("Layer '%s' created successfully", m.layer)
		return m, nil
	}
	return m, nil
}

func (m *createLayerModel) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "enter":
		value := sanitizeName(m.nameInput.Value())
		if value == "" {
			m.logs.Warnf("Layer name cannot be empty.")
			return m, nil
		}
		m.layer = value
		m.step = layerStepBranch
		m.branchInput.Focus()
		m.nameInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m *createLayerModel) updateBranch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "b":
		m.step = layerStepName
		m.nameInput.Focus()
		m.branchInput.Blur()
		return m, nil
	case "enter":
		m.step = layerStepSummary
		m.branchInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.branchInput, cmd = m.branchInput.Update(msg)
	return m, cmd
}

func (m *createLayerModel) updateSummary(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "f":
		m.force = !m.force
		return m, nil
	case "b":
		m.step = layerStepBranch
		m.branchInput.Focus()
		return m, nil
	case "enter":
		if m.layer == "" {
			name := sanitizeName(m.nameInput.Value())
			if name == "" {
				m.logs.Warnf("Layer name cannot be empty.")
				m.step = layerStepName
				m.nameInput.Focus()
				return m, nil
			}
			m.layer = name
		}
		m.step = layerStepRunning
		m.logs.Infof("Creating layer '%s'...", m.layer)
		return m, tea.Batch(m.spinner.Tick, m.runCreateLayerCmd())
	}
	return m, nil
}

func (m *createLayerModel) runCreateLayerCmd() tea.Cmd {
	name := m.layer
	branch := strings.TrimSpace(m.branchInput.Value())
	force := m.force
	cfg := m.cfg
	ctx := m.ctx
	logs := m.logs

	return func() tea.Msg {
		logs.Infof("Target layer: %s", name)
		if branch != "" {
			logs.Infof("Branch override: %s", branch)
		}
		if force {
			logs.Warnf("Force flag enabled; existing directory may be reused if empty.")
		}

		logWriter := logs.Writer(ui.Info)
		err := RunCreateLayer(ctx, cfg, name, branch, force, gitutil.WithOutput(logWriter), gitutil.WithLogger(func(format string, args ...any) {
			logs.Infof(format, args...)
		}))
		if err != nil {
			return layerResultMsg{err: err}
		}
		return layerResultMsg{err: nil}
	}
}

func (m *createLayerModel) View() string {
	switch m.step {
	case layerStepName:
		return ui.Content.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Layer name"),
			ui.Subtitle.Render("Provide a snake/kebab-case identifier."),
			"",
			ui.Content.Render(m.nameInput.View()),
		))
	case layerStepBranch:
		return ui.Content.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Branch override"),
			ui.Subtitle.Render("Optionally override the layer template branch."),
			"",
			ui.Content.Render(m.branchInput.View()),
		))
	case layerStepSummary:
		branch := strings.TrimSpace(m.branchInput.Value())
		if branch == "" {
			branch = "config default"
		}
		lines := []string{
			fmt.Sprintf("Name  : %s", m.layer),
			fmt.Sprintf("Branch: %s", branch),
			fmt.Sprintf("Force : %v", m.force),
			"",
			ui.Hint.Render("Press 'f' to toggle force."),
		}
		return lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Review & confirm"),
			ui.Subtitle.Render("Press Enter to clone the layer template."),
			"",
			ui.Content.Render(strings.Join(lines, "\n")),
		)
	case layerStepRunning:
		return ui.Content.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Creating layer"),
			ui.Subtitle.Render("Cloning template into /layers."),
			"",
			ui.Content.Render(fmt.Sprintf("%s  %s", m.spinner.View(), "Working...")),
		))
	case layerStepDone:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Layer ready"),
			ui.Subtitle.Render("Press Enter to exit."),
		)
	case layerStepError:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Layer creation failed"),
			ui.LogError.Render(fmt.Sprintf("%v", m.err)),
			ui.Hint.Render("Press Enter to exit."),
		)
	default:
		return ""
	}
}

func (m *createLayerModel) Hints() []string {
	switch m.step {
	case layerStepName:
		return []string{"Enter next", "Ctrl+C cancel"}
	case layerStepBranch:
		return []string{"Enter next", "b back", "Ctrl+C cancel"}
	case layerStepSummary:
		return []string{"Enter confirm", "f toggle force", "b back", "Ctrl+C cancel"}
	case layerStepRunning:
		return []string{"Ctrl+C abort (best effort)"}
	case layerStepDone:
		return []string{"Enter finish"}
	case layerStepError:
		return []string{"Enter exit"}
	default:
		return nil
	}
}

func (m *createLayerModel) Result() (string, error) {
	if m.aborted && m.err == nil {
		return "", ErrAborted
	}
	return m.layer, m.err
}

func RunCreateLayerTUI(ctx context.Context, cfg *config.Config, nameHint, branchHint string, force bool) (string, error) {
	logs := ui.NewLogBuffer(128)
	model := newCreateLayerModel(ctx, cfg, nameHint, branchHint, force, logs)
	root := ui.NewRootModel("Create Layer", "Scaffold a new reusable layer inside /layers.", model, logs, nil)
	final, err := ui.Run(root, tea.WithAltScreen())
	if err != nil {
		return "", err
	}
	rootResult, ok := final.(*ui.RootModel)
	if !ok {
		return "", errors.New("unexpected root model result")
	}
	child, ok := rootResult.Child.(*createLayerModel)
	if !ok {
		return "", errors.New("unexpected child model result")
	}
	return child.Result()
}
