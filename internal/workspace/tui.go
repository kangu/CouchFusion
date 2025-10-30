package workspace

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type moduleSelectModel struct {
	items       []string
	cursor      int
	selected    map[int]struct{}
	confirm     bool
	renderReady bool
}

func newModuleSelectModel(items []string, preselected []string) moduleSelectModel {
	selected := map[int]struct{}{}
	if len(preselected) > 0 {
		indexByName := map[string]int{}
		for i, item := range items {
			indexByName[item] = i
		}
		for _, name := range preselected {
			if idx, ok := indexByName[name]; ok {
				selected[idx] = struct{}{}
			}
		}
	}
	return moduleSelectModel{
		items:    items,
		selected: selected,
	}
}

func (m moduleSelectModel) Init() tea.Cmd {
	return nil
}

func (m moduleSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return moduleSelectModel{items: m.items, selected: map[int]struct{}{}, confirm: true}, tea.Quit
		case "enter":
			m.confirm = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ":
			if _, ok := m.selected[m.cursor]; ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
		}
	case tea.WindowSizeMsg:
		m.renderReady = true
	}
	return m, nil
}

func (m moduleSelectModel) View() string {
	if !m.renderReady && len(m.items) == 0 {
		return ""
	}

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true)
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("57"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213"))

	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render("Select modules to include (↑/↓ to navigate, space to toggle, enter to confirm, q to quit)"))

	for i, item := range m.items {
		cursor := " "
		if m.cursor == i {
			cursor = cursorStyle.Render("›")
		}
		check := " "
		if _, ok := m.selected[i]; ok {
			check = selectedStyle.Render("•")
		}
		fmt.Fprintf(&b, "%s [%s] %s\n", cursor, check, item)
	}

	return b.String()
}

func runModuleSelector(options []string, preselected []string) ([]string, error) {
	model := newModuleSelectModel(options, preselected)
	program := tea.NewProgram(model)

	res, err := program.Run()
	if err != nil {
		return nil, err
	}

	final, ok := res.(moduleSelectModel)
	if !ok {
		return nil, fmt.Errorf("unexpected model result type")
	}

	indices := make([]int, 0, len(final.selected))
	for idx := range final.selected {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	selected := make([]string, 0, len(indices))
	for _, idx := range indices {
		selected = append(selected, final.items[idx])
	}

	return selected, nil
}
