package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/gitutil"
	"github.com/nuxt-apps/couchfusion/internal/ui"
)

type initStep int

const (
	initStepPath initStep = iota
	initStepBranch
	initStepSummary
	initStepRunning
	initStepDone
	initStepError
)

type initResultMsg struct {
	err error
}

type initModel struct {
	ctx  context.Context
	cfg  *config.Config
	logs *ui.LogBuffer

	pathInput   textinput.Model
	branchInput textinput.Model
	force       bool

	spinner spinner.Model
	step    initStep
	err     error
	aborted bool
	done    bool
}

func newInitModel(ctx context.Context, cfg *config.Config, pathHint, branchHint string, force bool, logs *ui.LogBuffer) *initModel {
	path := textinput.New()
	path.Placeholder = "./"
	path.CharLimit = 256
	path.Prompt = ""
	if pathHint != "" {
		path.SetValue(pathHint)
	} else {
		path.SetValue(".")
	}
	path.Focus()

	branch := textinput.New()
	branch.Placeholder = "(config default)"
	branch.CharLimit = 128
	branch.Prompt = ""
	branch.SetValue(branchHint)

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(ui.PrimaryLight)

	return &initModel{
		ctx:         ctx,
		cfg:         cfg,
		logs:        logs,
		pathInput:   path,
		branchInput: branch,
		force:       force,
		spinner:     spin,
		step:        initStepPath,
	}
}

func (m *initModel) Init() tea.Cmd {
	return nil
}

func (m *initModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case initStepPath:
			return m.updatePath(msg)
		case initStepBranch:
			return m.updateBranch(msg)
		case initStepSummary:
			return m.updateSummary(msg)
		case initStepRunning:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				m.aborted = true
				return m, tea.Quit
			}
		case initStepDone, initStepError:
			if msg.String() == "enter" || msg.String() == "q" {
				return m, tea.Quit
			}
		}
	case spinner.TickMsg:
		if m.step == initStepRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case initResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.logs.Errorf("Initialization failed: %v", msg.err)
			m.step = initStepError
			return m, nil
		}
		m.step = initStepDone
		m.done = true
		m.logs.Successf("Workspace ready at %s", filepath.Clean(m.pathInput.Value()))
		return m, nil
	}
	return m, nil
}

func (m *initModel) updatePath(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "enter":
		value := strings.TrimSpace(m.pathInput.Value())
		if value == "" {
			value = "."
			m.pathInput.SetValue(value)
		}
		m.step = initStepBranch
		m.branchInput.Focus()
		m.pathInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	return m, cmd
}

func (m *initModel) updateBranch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "b":
		m.step = initStepPath
		m.pathInput.Focus()
		m.branchInput.Blur()
		return m, nil
	case "enter":
		m.step = initStepSummary
		m.branchInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.branchInput, cmd = m.branchInput.Update(msg)
	return m, cmd
}

func (m *initModel) updateSummary(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "f":
		m.force = !m.force
		return m, nil
	case "b":
		m.step = initStepBranch
		m.branchInput.Focus()
		return m, nil
	case "enter":
		m.step = initStepRunning
		m.logs.Infof("Initializing workspace...")
		return m, tea.Batch(m.spinner.Tick, m.runInitCmd())
	}
	return m, nil
}

func (m *initModel) runInitCmd() tea.Cmd {
	path := strings.TrimSpace(m.pathInput.Value())
	if path == "" {
		path = "."
	}
	branch := strings.TrimSpace(m.branchInput.Value())
	force := m.force
	cfg := m.cfg
	ctx := m.ctx
	logs := m.logs

	return func() tea.Msg {
		logs.Infof("Target path: %s", filepath.Clean(path))
		if branch != "" {
			logs.Infof("Layers branch override: %s", branch)
		}
		if force {
			logs.Warnf("Force flag enabled; existing directories may be reused if empty.")
		}

		logWriter := logs.Writer(ui.Info)
		err := RunInit(ctx, cfg, path, branch, force, gitutil.WithOutput(logWriter), gitutil.WithLogger(func(format string, args ...any) {
			logs.Infof(format, args...)
		}))
		if err != nil {
			return initResultMsg{err: err}
		}
		return initResultMsg{err: nil}
	}
}

func (m *initModel) View() string {
	switch m.step {
	case initStepPath:
		return ui.Content.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Workspace path"),
			ui.Subtitle.Render("Enter the directory to initialize (defaults to current)."),
			"",
			ui.Content.Render(m.pathInput.View()),
		))
	case initStepBranch:
		return ui.Content.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Layers branch"),
			ui.Subtitle.Render("Optionally override the layers repository branch."),
			"",
			ui.Content.Render(m.branchInput.View()),
		))
	case initStepSummary:
		lines := []string{
			fmt.Sprintf("Path   : %s", filepath.Clean(m.pathInput.Value())),
		}
		branch := strings.TrimSpace(m.branchInput.Value())
		if branch == "" {
			branch = "config default"
		}
		lines = append(lines,
			fmt.Sprintf("Branch : %s", branch),
			fmt.Sprintf("Force  : %v", m.force),
			"",
			ui.Hint.Render("Press 'f' to toggle force."),
		)
		content := lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Review initialization"),
			ui.Subtitle.Render("Confirm settings before cloning."),
			"",
			ui.Content.Render(strings.Join(lines, "\n")),
		)
		return content
	case initStepRunning:
		return ui.Content.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Initializing"),
			ui.Subtitle.Render("Creating directories and cloning layers repository."),
			"",
			ui.Content.Render(fmt.Sprintf("%s  %s", m.spinner.View(), "Working...")),
		))
	case initStepDone:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Workspace initialized"),
			ui.Subtitle.Render("Press Enter to exit."),
		)
	case initStepError:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			ui.Title.Render("Initialization failed"),
			ui.LogError.Render(fmt.Sprintf("%v", m.err)),
			ui.Hint.Render("Press Enter to exit."),
		)
	default:
		return ""
	}
}

func (m *initModel) Hints() []string {
	switch m.step {
	case initStepPath:
		return []string{"Enter to continue", "Ctrl+C cancel"}
	case initStepBranch:
		return []string{"Enter next", "b back", "Ctrl+C cancel"}
	case initStepSummary:
		return []string{"Enter confirm", "f toggle force", "b back", "Ctrl+C cancel"}
	case initStepRunning:
		return []string{"Ctrl+C abort (best effort)"}
	case initStepDone:
		return []string{"Enter finish"}
	case initStepError:
		return []string{"Enter exit"}
	default:
		return nil
	}
}

func (m *initModel) Result() (string, error) {
	if m.aborted && m.err == nil {
		return "", ErrAborted
	}
	return filepath.Clean(m.pathInput.Value()), m.err
}

func RunInitTUI(ctx context.Context, cfg *config.Config, pathHint, branchHint string, force bool) (string, error) {
	logs := ui.NewLogBuffer(128)
	model := newInitModel(ctx, cfg, pathHint, branchHint, force, logs)
	root := ui.NewRootModel("Initialize Workspace", "Prepare /apps and /layers with starter content.", model, logs, nil)
	final, err := ui.Run(root, tea.WithAltScreen())
	if err != nil {
		return "", err
	}
	rootResult, ok := final.(*ui.RootModel)
	if !ok {
		return "", errors.New("unexpected root model result")
	}
	child, ok := rootResult.Child.(*initModel)
	if !ok {
		return "", errors.New("unexpected child model result")
	}
	return child.Result()
}
