package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
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

var ErrAborted = errors.New("operation cancelled by user")

type createAppStep int

const (
	stepName createAppStep = iota
	stepModules
	stepAuth
	stepSummary
	stepRunning
	stepDone
	stepError
)

type createAppResultMsg struct {
	err error
}

type createAppModel struct {
	ctx    context.Context
	cfg    *config.Config
	branch string
	force  bool

	logs *ui.LogBuffer

	step createAppStep

	nameInput  textinput.Model
	nameError  string
	appName    string
	moduleView moduleSelectModel
	modules    []string
	defaults   []string
	authUserInput textinput.Model
	authPassInput textinput.Model
	authField     int
	authError     string
	authUsername  string
	authPassword  string

	spinner spinner.Model
	status  string

	err     error
	aborted bool
	done    bool
}

func newCreateAppModel(ctx context.Context, cfg *config.Config, nameHint string, moduleHints []string, branch string, force bool, logs *ui.LogBuffer) *createAppModel {
	defaults := cfg.DefaultModuleSelection()
	modulesList := availableModules(cfg)

	initialModules := moduleHints
	if len(initialModules) == 0 {
		initialModules = defaults
	}

	nameInput := textinput.New()
	nameInput.Placeholder = "app-name"
	nameInput.CharLimit = 64
	nameInput.Prompt = ""
	nameInput.Focus()

	authUser := textinput.New()
	authUser.Placeholder = "CouchDB admin username"
	authUser.CharLimit = 128
	authUser.Prompt = ""

	authPass := textinput.New()
	authPass.Placeholder = "CouchDB admin password"
	authPass.CharLimit = 256
	authPass.Prompt = ""
	authPass.EchoMode = textinput.EchoPassword
	authPass.EchoCharacter = '•'

	sanitizedName := sanitizeName(nameHint)
	if sanitizedName != "" {
		nameInput.SetValue(sanitizedName)
		nameInput.CursorEnd()
	}

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(ui.PrimaryLight)

	model := &createAppModel{
		ctx:        ctx,
		cfg:        cfg,
		branch:     branch,
		force:      force,
		logs:       logs,
		nameInput:  nameInput,
		moduleView: newModuleSelectModel(modulesList, initialModules),
		defaults:   defaults,
		authUserInput: authUser,
		authPassInput: authPass,
		spinner:    spin,
	}

	if sanitizedName != "" {
		model.appName = sanitizedName
		model.step = stepModules
	} else {
		model.step = stepName
	}

	return model
}

func (m *createAppModel) Init() tea.Cmd {
	if m.step == stepRunning {
		return tea.Batch(m.spinner.Tick, m.runCreateCmd())
	}
	return nil
}

func (m *createAppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case stepName:
			return m.updateNameStep(msg)
		case stepModules:
			return m.updateModuleStep(msg)
		case stepAuth:
			return m.updateAuthStep(msg)
		case stepSummary:
			return m.updateSummaryStep(msg)
		case stepRunning:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				m.aborted = true
				return m, tea.Quit
			}
		case stepDone, stepError:
			if msg.String() == "enter" || msg.String() == "q" {
				return m, tea.Quit
			}
		}
	case spinner.TickMsg:
		if m.step == stepRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case createAppResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.logs.Errorf("Create app failed: %v", msg.err)
			m.step = stepError
			return m, nil
		}
		m.step = stepDone
		m.done = true
		m.logs.Successf("App '%s' created successfully.", m.appName)
		return m, nil
	}
	return m, nil
}

func (m *createAppModel) updateNameStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "enter":
		value := sanitizeName(m.nameInput.Value())
		if value == "" {
			m.nameError = "Name cannot be empty."
			return m, nil
		}
		m.appName = value
		m.nameError = ""
		m.step = stepModules
		return m, nil
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m *createAppModel) updateModuleStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "b":
		m.step = stepName
		return m, nil
	case "enter":
		selected := m.moduleView.SelectedNames()
		if len(selected) == 0 {
			selected = append([]string{}, m.defaults...)
		}
		m.modules = selected
		if containsModule(selected, "auth") {
			m.enterAuthStep()
		} else {
			m.step = stepSummary
		}
		return m, nil
	}

	m.moduleView.HandleKey(msg.String())
	return m, nil
}

func (m *createAppModel) updateAuthStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "b":
		m.step = stepModules
		return m, nil
	case "tab", "down", "right":
		m.authField = (m.authField + 1) % 2
		m.focusAuthField()
		return m, nil
	case "shift+tab", "up", "left":
		if m.authField == 0 {
			m.authField = 1
		} else {
			m.authField = 0
		}
		m.focusAuthField()
		return m, nil
	case "enter":
		if m.authField == 0 {
			m.authField = 1
			m.focusAuthField()
			return m, nil
		}
		username := strings.TrimSpace(m.authUserInput.Value())
		password := strings.TrimSpace(m.authPassInput.Value())
		if username == "" || password == "" {
			m.authError = "Username and password are required."
			return m, nil
		}
		m.authUsername = username
		m.authPassword = password
		m.authError = ""
		m.step = stepSummary
		return m, nil
	}

	var cmd tea.Cmd
	if m.authField == 0 {
		m.authUserInput, cmd = m.authUserInput.Update(msg)
	} else {
		m.authPassInput, cmd = m.authPassInput.Update(msg)
	}
	return m, cmd
}

func (m *createAppModel) updateSummaryStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "m":
		m.step = stepModules
		return m, nil
	case "n":
		m.step = stepName
		return m, nil
	case "enter":
		if len(m.modules) == 0 {
			m.modules = append([]string{}, m.defaults...)
		}
		m.step = stepRunning
		m.logs.Infof("Scaffolding app '%s'...", m.appName)
		return m, tea.Batch(m.spinner.Tick, m.runCreateCmd())
	}
	return m, nil
}

func (m *createAppModel) runCreateCmd() tea.Cmd {
	name := m.appName
	modules := append([]string{}, m.modules...)
	branch := m.branch
	force := m.force
	cfg := m.cfg
	ctx := m.ctx
	logs := m.logs

	root, _ := os.Getwd()
	targetDir := filepath.Join(root, "apps", name)

	return func() tea.Msg {
		logs.Infof("Target directory: %s", targetDir)
		logs.Infof("Selected modules: %s", strings.Join(modules, ", "))

		cmdCtx := ctx
		if containsModule(modules, "auth") {
			if m.authUsername != "" && m.authPassword != "" {
				logs.Infof("Using provided CouchDB admin user '%s'", m.authUsername)
				cmdCtx = WithAuthCredentials(cmdCtx, m.authUsername, m.authPassword)
			} else {
				logs.Warnf("Auth module selected; CouchDB credentials will be requested during setup.")
			}
		}

		logWriter := logs.Writer(ui.Info)
		logs.Infof("Cloning starter repository...")
		err := RunCreateApp(cmdCtx, cfg, name, modules, branch, force, gitutil.WithOutput(logWriter), gitutil.WithLogger(func(format string, args ...any) {
			logs.Infof(format, args...)
		}))
		if err != nil {
			return createAppResultMsg{err: err}
		}
		return createAppResultMsg{err: nil}
	}
}

func (m *createAppModel) enterAuthStep() {
	if m.authUsername != "" {
		m.authUserInput.SetValue(m.authUsername)
	} else {
		m.authUserInput.SetValue("")
	}
	if m.authPassword != "" {
		m.authPassInput.SetValue(m.authPassword)
	} else {
		m.authPassInput.SetValue("")
	}
	m.authField = 0
	m.focusAuthField()
	m.authError = ""
	m.step = stepAuth
}

func (m *createAppModel) focusAuthField() {
	m.authUserInput.Blur()
	m.authPassInput.Blur()
	if m.authField == 0 {
		m.authUserInput.Focus()
	} else {
		m.authPassInput.Focus()
	}
}

func (m *createAppModel) View() string {
	switch m.step {
	case stepName:
		return m.viewNameStep()
	case stepModules:
		return m.viewModuleStep()
	case stepAuth:
		return m.viewAuthStep()
	case stepSummary:
		return m.viewSummaryStep()
	case stepRunning:
		return m.viewRunningStep()
	case stepDone:
		return m.viewDoneStep()
	case stepError:
		return m.viewErrorStep()
	default:
		return ""
	}
}

func (m *createAppModel) viewNameStep() string {
	errorLine := ""
	if m.nameError != "" {
		errorLine = ui.LogError.Render(m.nameError)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		ui.Title.Render("Name your application"),
		ui.Subtitle.Render("Provide a URL-friendly name. We'll clean up spaces and uppercase letters automatically."),
		"",
		ui.Content.Render("> "+m.nameInput.View()),
		"",
		errorLine,
	)
	return ui.Content.Render(content)
}

func (m *createAppModel) viewModuleStep() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		ui.Title.Render("Select layers"),
		ui.Subtitle.Render("Use space to toggle layers. Leave empty to accept defaults."),
		"",
		m.moduleView.ViewList(),
		"",
		ui.Content.Render(renderSummary(m.moduleView.selectedNames())),
	)
	return ui.Content.Render(content)
}

func (m *createAppModel) viewAuthStep() string {
	lines := []string{
		ui.Title.Render("CouchDB admin credentials"),
		ui.Subtitle.Render("These values seed COUCHDB_ADMIN_AUTH and COUCHDB_COOKIE_SECRET."),
		"",
		ui.Content.Render("Username: " + m.authUserInput.View()),
		ui.Content.Render("Password: " + m.authPassInput.View()),
	}
	if m.authError != "" {
		lines = append(lines, "", ui.LogError.Render(m.authError))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *createAppModel) viewSummaryStep() string {
	modList := m.modules
	if len(modList) == 0 {
		modList = m.defaults
	}
	lines := []string{
		fmt.Sprintf("App Name   : %s", ui.Content.Render(m.appName)),
		fmt.Sprintf("Modules    : %s", ui.Content.Render(strings.Join(modList, ", "))),
		fmt.Sprintf("Branch     : %s", branchLabel(m.branch)),
		fmt.Sprintf("Force      : %v", m.force),
	}
	if containsModule(modList, "auth") {
		credStatus := "will prompt during scaffolding"
		if m.authUsername != "" {
			credStatus = fmt.Sprintf("username %s", m.authUsername)
		}
		lines = append(lines, fmt.Sprintf("Auth       : %s", credStatus))
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		ui.Title.Render("Review & Confirm"),
		ui.Subtitle.Render("Press Enter to scaffold the app, or navigate back to adjust details."),
		"",
		ui.Content.Render(strings.Join(lines, "\n")),
	)
	return content
}

func (m *createAppModel) viewRunningStep() string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		ui.Title.Render("Scaffolding app"),
		ui.Subtitle.Render("Hang tight while we clone the template and write configuration files."),
		"",
		ui.Content.Render(fmt.Sprintf("%s  %s", m.spinner.View(), "Working...")),
	)
	return content
}

func (m *createAppModel) viewDoneStep() string {
	modList := m.modules
	if len(modList) == 0 {
		modList = m.defaults
	}
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		ui.Title.Render("All set!"),
		ui.Subtitle.Render("Your app has been created with the following layers:"),
		"",
		ui.Content.Render(strings.Join(modList, ", ")),
		"",
		ui.LogSuccess.Render("Press Enter to exit."),
	)
	return content
}

func (m *createAppModel) viewErrorStep() string {
	msg := ui.LogError.Render(fmt.Sprintf("Error: %v", m.err))
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		ui.Title.Render("Something went wrong"),
		ui.Subtitle.Render("Review the log output for details."),
		"",
		msg,
		ui.Hint.Render("Press Enter to exit."),
	)
	return content
}

func branchLabel(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return "default"
	}
	return branch
}

func (m *createAppModel) Hints() []string {
	switch m.step {
	case stepName:
		return []string{"Type to edit name", "Enter to continue", "Ctrl+C to cancel"}
	case stepModules:
		return []string{"↑/↓ move", "Space toggle", "Enter accept", "b back", "Ctrl+C cancel"}
	case stepAuth:
		return []string{"Tab switch field", "Enter next/confirm", "b back", "Ctrl+C cancel"}
	case stepSummary:
		return []string{"Enter confirm", "m modules", "n rename", "Ctrl+C cancel"}
	case stepRunning:
		return []string{"Ctrl+C abort (best effort)"}
	case stepDone:
		return []string{"Enter to finish"}
	case stepError:
		return []string{"Enter to exit"}
	default:
		return nil
	}
}

func (m *createAppModel) Result() (string, []string, error) {
	if m.aborted && m.err == nil {
		return "", nil, ErrAborted
	}
	if m.err != nil {
		return "", nil, m.err
	}
	mods := m.modules
	if len(mods) == 0 {
		mods = append([]string{}, m.defaults...)
	}
	return m.appName, mods, nil
}

// RunCreateAppTUI runs the interactive Bubble Tea experience for scaffolding a new app.
func RunCreateAppTUI(ctx context.Context, cfg *config.Config, nameHint string, modulesHint string, branch string, force bool) (string, []string, error) {
	logBuffer := ui.NewLogBuffer(128)

	initialModules := parseModules(modulesHint)
	model := newCreateAppModel(ctx, cfg, nameHint, initialModules, branch, force, logBuffer)

	root := ui.NewRootModel("Create Nuxt App", "Scaffold a new Nuxt application with CouchFusion layers.", model, logBuffer, nil)
	finalModel, err := ui.Run(root, tea.WithAltScreen())
	if err != nil {
		return "", nil, err
	}

	rootResult, ok := finalModel.(*ui.RootModel)
	if !ok {
		return "", nil, errors.New("unexpected root model result")
	}

	child, ok := rootResult.Child.(*createAppModel)
	if !ok {
		return "", nil, errors.New("unexpected child model result")
	}

	return child.Result()
}

func containsModule(mods []string, target string) bool {
	for _, m := range mods {
		if m == target {
			return true
		}
	}
	return false
}
