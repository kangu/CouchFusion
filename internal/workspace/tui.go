package workspace

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	frameStyle = lipgloss.NewStyle().
			Padding(1, 3).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63"))

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("213")).
			Bold(true).
			Underline(true)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).
			Bold(true)

	bulletSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("84")).
				Bold(true)

	bulletUnselectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("236"))

	rowStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			Foreground(lipgloss.Color("248"))

	rowActiveStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("63")).
			Bold(true)

	rowSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("213")).
				Bold(true)

	summaryStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("147"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true)
)

type moduleSelectModel struct {
	items    []string
	cursor   int
	selected map[int]struct{}
}

func newModuleSelectModel(items []string, preselected []string) moduleSelectModel {
	selected := map[int]struct{}{}
	cursor := 0

	if len(preselected) > 0 {
		indexByName := make(map[string]int, len(items))
		for i, item := range items {
			indexByName[item] = i
		}
		for _, name := range preselected {
			if idx, ok := indexByName[name]; ok {
				selected[idx] = struct{}{}
			}
		}
		if idx, ok := indexByName[preselected[0]]; ok {
			cursor = idx
		}
	}

	return moduleSelectModel{
		items:    items,
		cursor:   cursor,
		selected: selected,
	}
}

func (m *moduleSelectModel) HandleKey(key string) {
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		} else if len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		} else {
			m.cursor = 0
		}
	case " ":
		if len(m.items) == 0 {
			return
		}
		if _, ok := m.selected[m.cursor]; ok {
			delete(m.selected, m.cursor)
		} else {
			m.selected[m.cursor] = struct{}{}
		}
	}
}

func (m moduleSelectModel) ViewList() string {
	if len(m.items) == 0 {
		return hintStyle.Render("No modules available")
	}
	rows := make([]string, 0, len(m.items))
	for i, item := range m.items {
		rows = append(rows, renderRow(m, i, item))
	}
	return strings.Join(rows, "\n")
}

func (m moduleSelectModel) View() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("CouchFusion Modules"),
		subtitleStyle.Render("Choose the layers you want in this app."),
		"",
		m.ViewList(),
		"",
		renderSummary(m.selectedNames()),
		hintStyle.Render("↑/↓ move • Space toggle • Enter to confirm"),
	)
	return frameStyle.Render(content)
}

func (m moduleSelectModel) SelectedNames() []string {
	return m.selectedNames()
}

func renderRow(m moduleSelectModel, index int, name string) string {
	cursor := " "
	if m.cursor == index {
		cursor = cursorStyle.Render("›")
	}

	symbol := bulletUnselectedStyle.Render("○")
	lineStyle := rowStyle
	if _, ok := m.selected[index]; ok {
		symbol = bulletSelectedStyle.Render("●")
		lineStyle = rowSelectedStyle
	}
	if m.cursor == index {
		lineStyle = rowActiveStyle
	}

	label := fmt.Sprintf("%s %s %s", cursor, symbol, name)
	return lineStyle.Render(label)
}

func (m moduleSelectModel) selectedNames() []string {
	names := make([]string, 0, len(m.selected))
	for idx := range m.selected {
		if idx >= 0 && idx < len(m.items) {
			names = append(names, m.items[idx])
		}
	}
	sort.Strings(names)
	return names
}

func renderSummary(names []string) string {
	if len(names) == 0 {
		return summaryStyle.Render("Selected: none (defaults will apply)")
	}
	return summaryStyle.Render("Selected: " + strings.Join(names, ", "))
}
