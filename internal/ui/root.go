package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type RootModel struct {
	Title    string
	Subtitle string
	Child    tea.Model
	Logs     *LogBuffer
	Hints    []string

	width    int
	height   int
	quitting bool
}

type hintProvider interface {
	Hints() []string
}

func NewRootModel(title, subtitle string, child tea.Model, logs *LogBuffer, hints []string) *RootModel {
	if logs == nil {
		logs = NewLogBuffer(64)
	}
	return &RootModel{
		Title:    title,
		Subtitle: subtitle,
		Child:    child,
		Logs:     logs,
		Hints:    hints,
	}
}

func (m *RootModel) Init() tea.Cmd {
	if m.Child != nil {
		return m.Child.Init()
	}
	return nil
}

func (m *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	if m.Child != nil {
		var cmd tea.Cmd
		var child tea.Model
		child, cmd = m.Child.Update(msg)
		m.Child = child
		return m, cmd
	}

	return m, nil
}

func (m *RootModel) View() string {
	body := ""
	if m.Child != nil {
		body = m.Child.View()
	}

	hintsView := ""
	switch child := m.Child.(type) {
	case hintProvider:
		if hintList := child.Hints(); len(hintList) > 0 {
			hintsView = Hint.Render(strings.Join(hintList, " • "))
		}
	default:
		if len(m.Hints) > 0 {
			hintsView = Hint.Render(strings.Join(m.Hints, " • "))
		}
	}

	width := m.width
	if width <= 0 {
		width = 96
	}
	logs := m.Logs.Render()

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		Title.Render(m.Title),
		Subtitle.Render(m.Subtitle),
		"",
		body,
		"",
		LogInfo.Render("Logs:"),
		logs,
		"",
		hintsView,
	)

	return AppFrame.Width(width).Render(content)
}

func Run(model *RootModel, opts ...tea.ProgramOption) (tea.Model, error) {
	program := tea.NewProgram(model, opts...)
	return program.Run()
}
