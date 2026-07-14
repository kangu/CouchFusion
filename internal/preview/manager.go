package preview

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/processes"
	"github.com/nuxt-apps/couchfusion/internal/project"
)

const TaskID = "component-preview"

type Status string

const (
	Stopped  Status = "stopped"
	Starting Status = "starting"
	Ready    Status = "ready"
	Failed   Status = "failed"
)

type View struct {
	Status    Status     `json:"status"`
	ProcessID string     `json:"processId,omitempty"`
	URL       string     `json:"url,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	Problem   string     `json:"problem,omitempty"`
}

type ProcessManager interface {
	Start(context.Context, processes.StartRequest) (processes.Snapshot, error)
	Get(string) (processes.Snapshot, error)
	Stop(context.Context, string) error
}

type Options struct {
	Processes    ProcessManager
	AllocatePort func() (int, error)
	NewToken     func() (string, error)
	Now          func() time.Time
	ReadyTimeout time.Duration
	PollInterval time.Duration
}

type Manager struct {
	mu      sync.Mutex
	records map[string]*record
	options Options
}

type record struct {
	status    Status
	processID string
	origin    string
	token     string
	port      int
	startedAt time.Time
	problem   string
	cancel    context.CancelFunc
}

func NewManager(options Options) *Manager {
	if options.AllocatePort == nil {
		options.AllocatePort = allocateLoopbackPort
	}
	if options.NewToken == nil {
		options.NewToken = randomToken
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.ReadyTimeout <= 0 {
		options.ReadyTimeout = 60 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	return &Manager{records: map[string]*record{}, options: options}
}

func (m *Manager) Start(ctx context.Context, workspaceID, siteID string, command project.Command) (View, error) {
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(siteID) == "" {
		return View{}, errors.New("workspace and site IDs are required")
	}
	if m.options.Processes == nil {
		return View{}, errors.New("preview process manager is required")
	}

	key := recordKey(workspaceID, siteID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.records[key]; existing != nil && (existing.status == Starting || existing.status == Ready) {
		return viewOf(existing), nil
	}

	port, err := m.options.AllocatePort()
	if err != nil {
		return View{}, fmt.Errorf("allocate preview port: %w", err)
	}
	token, err := m.options.NewToken()
	if err != nil {
		return View{}, fmt.Errorf("create preview token: %w", err)
	}
	command.Environment = copyEnvironment(command.Environment)
	command.Environment["COUCHFUSION_PREVIEW"] = "1"
	command.Environment["COUCHFUSION_PREVIEW_TOKEN"] = token
	command.Environment["NUXT_HOST"] = "127.0.0.1"
	command.Environment["NUXT_PORT"] = strconv.Itoa(port)

	snapshot, err := m.options.Processes.Start(ctx, processes.StartRequest{
		WorkspaceID:  workspaceID,
		SiteID:       siteID,
		TaskID:       TaskID,
		Command:      command,
		SecretValues: []string{token},
	})
	if err != nil {
		return View{}, err
	}
	watchContext, cancel := context.WithCancel(context.Background())
	state := &record{
		status: Starting, processID: snapshot.ID, token: token, port: port,
		startedAt: snapshot.StartedAt, cancel: cancel,
	}
	m.records[key] = state
	go m.awaitReadiness(watchContext, key, snapshot.ID)
	return viewOf(state), nil
}

func (m *Manager) Status(ctx context.Context, workspaceID, siteID string) (View, error) {
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.records[recordKey(workspaceID, siteID)]
	if state == nil {
		return View{Status: Stopped}, nil
	}
	return viewOf(state), nil
}

func (m *Manager) Stop(ctx context.Context, workspaceID, siteID string) (View, error) {
	key := recordKey(workspaceID, siteID)
	m.mu.Lock()
	state := m.records[key]
	if state == nil {
		m.mu.Unlock()
		return View{Status: Stopped}, nil
	}
	state.cancel()
	delete(m.records, key)
	processID := state.processID
	m.mu.Unlock()

	if processID != "" {
		if err := m.options.Processes.Stop(ctx, processID); err != nil && !errors.Is(err, processes.ErrNotFound) {
			return View{}, err
		}
	}
	return View{Status: Stopped}, nil
}

func (m *Manager) awaitReadiness(ctx context.Context, key, processID string) {
	ticker := time.NewTicker(m.options.PollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(m.options.ReadyTimeout)
	defer timer.Stop()
	timeout := timer.C
	ready := false

	for {
		done, observedReady := m.inspect(key, processID)
		if done {
			return
		}
		if observedReady && !ready {
			ready = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timeout = nil
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-timeout:
			stopContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = m.options.Processes.Stop(stopContext, processID)
			cancel()
			m.setFailed(key, processID, "Preview host did not report a usable URL within 60 seconds.")
			return
		}
	}
}

func (m *Manager) inspect(key, processID string) (done, ready bool) {
	snapshot, err := m.options.Processes.Get(processID)
	if err != nil {
		m.setFailed(key, processID, "Preview host process is unavailable.")
		return true, false
	}
	if terminal(snapshot.Status) {
		problem := "Preview host exited before it became ready."
		if snapshot.Error != nil && snapshot.Error.Message != "" {
			problem = snapshot.Error.Message
		}
		m.setFailed(key, processID, problem)
		return true, false
	}

	m.mu.Lock()
	state := m.records[key]
	if state == nil || state.processID != processID {
		m.mu.Unlock()
		return true, false
	}
	port := state.port
	m.mu.Unlock()

	for _, candidate := range snapshot.URLs {
		origin, ok := verifiedOrigin(candidate, port)
		if !ok {
			continue
		}
		m.mu.Lock()
		state = m.records[key]
		if state != nil && state.processID == processID {
			state.status = Ready
			state.origin = origin
			state.problem = ""
		}
		m.mu.Unlock()
		return false, true
	}
	return false, false
}

func (m *Manager) setFailed(key, processID, problem string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.records[key]
	if state == nil || state.processID != processID {
		return
	}
	state.status = Failed
	state.origin = ""
	state.token = ""
	state.problem = problem
}

func viewOf(state *record) View {
	startedAt := state.startedAt
	view := View{
		Status: state.status, ProcessID: state.processID,
		StartedAt: &startedAt, Problem: state.problem,
	}
	if state.status == Ready && state.origin != "" && state.token != "" {
		previewURL, _ := url.Parse(state.origin)
		previewURL.Path = "/__couchfusion/components/preview"
		query := previewURL.Query()
		query.Set("token", state.token)
		previewURL.RawQuery = query.Encode()
		view.URL = previewURL.String()
	}
	return view
}

func verifiedOrigin(candidate string, port int) (string, bool) {
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname != "127.0.0.1" && hostname != "localhost" {
		return "", false
	}
	if parsed.Port() != strconv.Itoa(port) {
		return "", false
	}
	return (&url.URL{Scheme: "http", Host: parsed.Host}).String(), true
}

func terminal(status processes.Status) bool {
	return status == processes.Exited || status == processes.Stopped || status == processes.Failed
}

func copyEnvironment(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source)+4)
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func recordKey(workspaceID, siteID string) string {
	return workspaceID + "\x00" + siteID
}

func allocateLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port <= 0 {
		return 0, errors.New("allocated listener has no TCP port")
	}
	return address.Port, nil
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
