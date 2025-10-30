package ui

import "github.com/charmbracelet/lipgloss"

var (
	Primary      = lipgloss.Color("63")
	PrimaryLight = lipgloss.Color("147")
	Accent       = lipgloss.Color("213")
	SuccessColor = lipgloss.Color("84")
	Warning      = lipgloss.Color("214")
	ErrorColor   = lipgloss.Color("203")
	Muted        = lipgloss.Color("244")
	Background   = lipgloss.Color("235")

	AppFrame = lipgloss.NewStyle().
			Padding(1, 3).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Primary)

	Title = lipgloss.NewStyle().
		Foreground(Accent).
		Bold(true).
		Underline(true)

	Subtitle = lipgloss.NewStyle().
			Foreground(Muted)

	Content = lipgloss.NewStyle().
		PaddingLeft(1).
		Foreground(lipgloss.Color("250"))

	LogInfo = lipgloss.NewStyle().
		Foreground(PrimaryLight)

	LogWarn = lipgloss.NewStyle().
		Foreground(Warning).
		Bold(true)

	LogSuccess = lipgloss.NewStyle().
			Foreground(SuccessColor).
			Bold(true)

	LogError = lipgloss.NewStyle().
			Foreground(ErrorColor).
			Bold(true)

	Hint = lipgloss.NewStyle().
		Foreground(Muted).
		Italic(true)
)
