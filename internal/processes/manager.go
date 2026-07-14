package processes

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/project"
)

var (
	ErrNotFound       = errors.New("managed process was not found")
	ErrAlreadyRunning = errors.New("managed process is already running")
	ErrShuttingDown   = errors.New("process manager is shutting down")
)

type Status string

const (
	Running  Status = "running"
	Stopping Status = "stopping"
	Exited   Status = "exited"
	Stopped  Status = "stopped"
	Failed   Status = "failed"
)

type Event struct {
	ID     uint64    `json:"id"`
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"`
	Code   string    `json:"code"`
	Text   string    `json:"text"`
}

type SafeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Snapshot struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	SiteID      string     `json:"siteId"`
	TaskID      string     `json:"taskId"`
	Status      Status     `json:"status"`
	PID         int        `json:"pid,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt time.Time  `json:"completedAt,omitempty"`
	ExitCode    *int       `json:"exitCode,omitempty"`
	URLs        []string   `json:"urls"`
	Events      []Event    `json:"events"`
	Error       *SafeError `json:"error,omitempty"`
}

type StartRequest struct {
	WorkspaceID  string
	SiteID       string
	TaskID       string
	Command      project.Command
	SecretValues []string
}

type Options struct {
	MaxEvents  int
	StopGrace  time.Duration
	SecretKeys []string
	Now        func() time.Time
	NewID      func() (string, error)
}

type ConflictError struct {
	WorkspaceID     string
	SiteID          string
	TaskID          string
	ActiveProcessID string
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("site task already has active process %q", e.ActiveProcessID)
}

func (e ConflictError) Unwrap() error { return ErrAlreadyRunning }

type Manager struct {
	mu           sync.Mutex
	processes    map[string]*managedProcess
	owners       map[string]string
	options      Options
	redactor     redactor
	shuttingDown bool
	wg           sync.WaitGroup
}

type managedProcess struct {
	snapshot       Snapshot
	request        StartRequest
	command        *exec.Cmd
	cancel         context.CancelFunc
	done           chan struct{}
	stopRequested  bool
	nextEventID    uint64
	subscribers    map[uint64]chan Event
	nextSubscriber uint64
	redactor       redactor
}

func NewManager(options Options) *Manager {
	if options.MaxEvents <= 0 {
		options.MaxEvents = 500
	}
	if options.StopGrace <= 0 {
		options.StopGrace = 2 * time.Second
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.NewID == nil {
		options.NewID = randomID
	}
	return &Manager{
		processes: map[string]*managedProcess{},
		owners:    map[string]string{},
		options:   options,
		redactor:  newRedactor(options.SecretKeys),
	}
}

func (m *Manager) Start(ctx context.Context, request StartRequest) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if strings.TrimSpace(request.WorkspaceID) == "" || strings.TrimSpace(request.SiteID) == "" || strings.TrimSpace(request.TaskID) == "" {
		return Snapshot{}, errors.New("workspace, site, and task IDs are required")
	}
	if strings.TrimSpace(request.Command.Executable) == "" || strings.TrimSpace(request.Command.Dir) == "" {
		return Snapshot{}, errors.New("resolved executable and working directory are required")
	}

	owner := ownerKey(request)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shuttingDown {
		return Snapshot{}, ErrShuttingDown
	}
	if activeID, exists := m.owners[owner]; exists {
		return Snapshot{}, ConflictError{
			WorkspaceID:     request.WorkspaceID,
			SiteID:          request.SiteID,
			TaskID:          request.TaskID,
			ActiveProcessID: activeID,
		}
	}
	id, err := m.options.NewID()
	if err != nil {
		return Snapshot{}, fmt.Errorf("create process id: %w", err)
	}
	if _, exists := m.processes[id]; exists {
		return Snapshot{}, fmt.Errorf("create process id: duplicate id %q", id)
	}

	processContext, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(processContext, request.Command.Executable, request.Command.Args...)
	command.Dir = request.Command.Dir
	if len(request.Command.Environment) > 0 {
		command.Env = mergeEnvironment(os.Environ(), request.Command.Environment)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return Snapshot{}, fmt.Errorf("capture process stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return Snapshot{}, fmt.Errorf("capture process stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		cancel()
		return Snapshot{}, fmt.Errorf("start managed process: %w", err)
	}

	state := &managedProcess{
		snapshot: Snapshot{
			ID:          id,
			WorkspaceID: request.WorkspaceID,
			SiteID:      request.SiteID,
			TaskID:      request.TaskID,
			Status:      Running,
			PID:         command.Process.Pid,
			StartedAt:   m.options.Now(),
			URLs:        []string{},
			Events:      []Event{},
		},
		request:     cloneRequest(request),
		command:     command,
		cancel:      cancel,
		done:        make(chan struct{}),
		subscribers: map[uint64]chan Event{},
		redactor:    m.redactor.withValues(request.SecretValues),
	}
	m.processes[id] = state
	m.owners[owner] = id
	m.addEventLocked(state, "lifecycle", "process.started", "Process started")
	snapshot := cloneSnapshot(state.snapshot)

	var scanners sync.WaitGroup
	scanners.Add(2)
	go m.capture(id, "stdout", stdout, &scanners)
	go m.capture(id, "stderr", stderr, &scanners)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		waitErr := command.Wait()
		scanners.Wait()
		m.finish(id, waitErr)
	}()
	return snapshot, nil
}

func (m *Manager) Get(id string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, exists := m.processes[id]
	if !exists {
		return Snapshot{}, ErrNotFound
	}
	return cloneSnapshot(state.snapshot), nil
}

func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	state, exists := m.processes[id]
	if !exists {
		m.mu.Unlock()
		return ErrNotFound
	}
	if terminal(state.snapshot.Status) {
		m.mu.Unlock()
		return nil
	}
	done := state.done
	process := state.command.Process
	firstRequest := !state.stopRequested
	if firstRequest {
		state.stopRequested = true
		state.snapshot.Status = Stopping
		m.addEventLocked(state, "lifecycle", "process.interrupt", "Stopping process")
	}
	m.mu.Unlock()

	if firstRequest {
		if err := process.Signal(os.Interrupt); err != nil && !processFinished(done) {
			m.recordEvent(id, "lifecycle", "process.interrupt_failed", "Process did not accept interrupt")
		}
	}

	timer := time.NewTimer(m.options.StopGrace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		m.forceKill(id, process)
	case <-ctx.Done():
		m.forceKill(id, process)
		return ctx.Err()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Restart(ctx context.Context, id string) (Snapshot, error) {
	m.mu.Lock()
	state, exists := m.processes[id]
	if !exists {
		m.mu.Unlock()
		return Snapshot{}, ErrNotFound
	}
	request := cloneRequest(state.request)
	m.mu.Unlock()

	if err := m.Stop(ctx, id); err != nil {
		return Snapshot{}, err
	}
	return m.Start(ctx, request)
}

func (m *Manager) Subscribe(id string, afterEventID uint64) (<-chan Event, func(), error) {
	m.mu.Lock()
	state, exists := m.processes[id]
	if !exists {
		m.mu.Unlock()
		return nil, nil, ErrNotFound
	}
	capacity := m.options.MaxEvents + 4
	if capacity < 16 {
		capacity = 16
	}
	stream := make(chan Event, capacity)
	for _, event := range state.snapshot.Events {
		if event.ID > afterEventID {
			stream <- event
		}
	}
	if terminal(state.snapshot.Status) {
		close(stream)
		m.mu.Unlock()
		return stream, func() {}, nil
	}
	state.nextSubscriber++
	subscriberID := state.nextSubscriber
	state.subscribers[subscriberID] = stream
	m.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			state, exists := m.processes[id]
			if !exists {
				return
			}
			if subscriber, exists := state.subscribers[subscriberID]; exists {
				delete(state.subscribers, subscriberID)
				close(subscriber)
			}
		})
	}
	return stream, unsubscribe, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.shuttingDown = true
	ids := make([]string, 0, len(m.processes))
	for id, state := range m.processes {
		if !terminal(state.snapshot.Status) {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()

	var stops sync.WaitGroup
	stops.Add(len(ids))
	for _, id := range ids {
		go func() {
			defer stops.Done()
			_ = m.Stop(ctx, id)
		}()
	}
	stopped := make(chan struct{})
	go func() {
		stops.Wait()
		m.wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) capture(id, stream string, reader io.Reader, wait *sync.WaitGroup) {
	defer wait.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		m.mu.Lock()
		state, exists := m.processes[id]
		if exists {
			m.addEventLocked(state, stream, "process.output", line)
			m.captureURLsLocked(state, line)
		}
		m.mu.Unlock()
	}
	if scanner.Err() != nil {
		m.recordEvent(id, "lifecycle", "process.output_error", "Process output could not be read completely")
	}
}

func (m *Manager) finish(id string, waitErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, exists := m.processes[id]
	if !exists || terminal(state.snapshot.Status) {
		return
	}
	if processState := state.command.ProcessState; processState != nil {
		exitCode := processState.ExitCode()
		state.snapshot.ExitCode = &exitCode
	}
	state.snapshot.CompletedAt = m.options.Now()
	switch {
	case state.stopRequested:
		state.snapshot.Status = Stopped
		m.addEventLocked(state, "lifecycle", "process.stopped", "Process stopped")
	case waitErr != nil:
		state.snapshot.Status = Failed
		state.snapshot.Error = &SafeError{Code: "process_failed", Message: "Process exited unexpectedly"}
		m.addEventLocked(state, "lifecycle", "process.failed", "Process exited unexpectedly")
	default:
		state.snapshot.Status = Exited
		m.addEventLocked(state, "lifecycle", "process.exited", "Process exited")
	}
	if m.owners[ownerKey(state.request)] == id {
		delete(m.owners, ownerKey(state.request))
	}
	state.cancel()
	close(state.done)
	for subscriberID, subscriber := range state.subscribers {
		close(subscriber)
		delete(state.subscribers, subscriberID)
	}
}

func (m *Manager) forceKill(id string, process *os.Process) {
	m.recordEvent(id, "lifecycle", "process.kill", "Process did not stop in time; forcing termination")
	_ = process.Kill()
}

func (m *Manager) recordEvent(id, stream, code, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, exists := m.processes[id]
	if exists && !terminal(state.snapshot.Status) {
		m.addEventLocked(state, stream, code, text)
	}
}

func (m *Manager) addEventLocked(state *managedProcess, stream, code, text string) {
	state.nextEventID++
	event := Event{
		ID:     state.nextEventID,
		Time:   m.options.Now(),
		Stream: stream,
		Code:   code,
		Text:   state.redactor.Text(text),
	}
	state.snapshot.Events = append(state.snapshot.Events, event)
	if excess := len(state.snapshot.Events) - m.options.MaxEvents; excess > 0 {
		state.snapshot.Events = append([]Event{}, state.snapshot.Events[excess:]...)
	}
	for _, subscriber := range state.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (m *Manager) captureURLsLocked(state *managedProcess, line string) {
	for _, raw := range urlPattern.FindAllString(line, -1) {
		raw = strings.TrimRight(raw, `.,;!?)\"]}`)
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host != "localhost" && host != "127.0.0.1" {
			continue
		}
		value := parsed.String()
		found := false
		for _, existing := range state.snapshot.URLs {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			state.snapshot.URLs = append(state.snapshot.URLs, value)
		}
	}
}

func cloneRequest(request StartRequest) StartRequest {
	copy := request
	copy.Command.Args = append([]string{}, request.Command.Args...)
	copy.Command.Environment = cloneEnvironment(request.Command.Environment)
	copy.SecretValues = append([]string{}, request.SecretValues...)
	return copy
}

func cloneEnvironment(environment map[string]string) map[string]string {
	if environment == nil {
		return nil
	}
	copy := make(map[string]string, len(environment))
	for key, value := range environment {
		copy[key] = value
	}
	return copy
}

func mergeEnvironment(inherited []string, overrides map[string]string) []string {
	values := make(map[string]string, len(inherited)+len(overrides))
	for _, entry := range inherited {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	merged := make([]string, 0, len(keys))
	for _, key := range keys {
		merged = append(merged, key+"="+values[key])
	}
	return merged
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	copy := snapshot
	copy.URLs = append([]string{}, snapshot.URLs...)
	copy.Events = append([]Event{}, snapshot.Events...)
	if snapshot.ExitCode != nil {
		exitCode := *snapshot.ExitCode
		copy.ExitCode = &exitCode
	}
	if snapshot.Error != nil {
		errorCopy := *snapshot.Error
		copy.Error = &errorCopy
	}
	return copy
}

func ownerKey(request StartRequest) string {
	return request.WorkspaceID + "\x00" + request.SiteID + "\x00" + request.TaskID
}

func terminal(status Status) bool {
	return status == Exited || status == Stopped || status == Failed
}

func processFinished(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

var urlPattern = regexp.MustCompile(`(?i)https?://[^\s<>]+`)

type redactor struct {
	userinfo        *regexp.Regexp
	sensitiveHeader []*regexp.Regexp
	secrets         []*regexp.Regexp
	exactValues     []*regexp.Regexp
}

func (r redactor) withValues(values []string) redactor {
	copy := r
	copy.exactValues = append([]*regexp.Regexp{}, r.exactValues...)
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		copy.exactValues = append(
			copy.exactValues,
			regexp.MustCompile(regexp.QuoteMeta(value)),
		)
	}
	return copy
}

func newRedactor(extraKeys []string) redactor {
	keys := append([]string{"password", "token", "authorization", "cookie_secret", "admin_auth"}, extraKeys...)
	patterns := make([]*regexp.Regexp, 0, len(keys))
	seen := map[string]struct{}{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		lower := strings.ToLower(key)
		if key == "" {
			continue
		}
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		patterns = append(patterns, regexp.MustCompile(`(?i)(\b`+regexp.QuoteMeta(key)+`\s*[:=]\s*)([^\s,;]+)`))
	}
	return redactor{
		userinfo: regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)([^\s/@]+(?::[^\s/@]*)?@)`),
		sensitiveHeader: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(\bauthorization\s*:\s*)[^\r\n]+`),
			regexp.MustCompile(`(?i)(\b(?:cookie|set-cookie)\s*:\s*)[^\r\n]+`),
		},
		secrets: patterns,
	}
}

func (r redactor) Text(value string) string {
	value = r.userinfo.ReplaceAllString(value, `${1}[redacted]@`)
	for _, pattern := range r.sensitiveHeader {
		value = pattern.ReplaceAllString(value, `${1}[redacted]`)
	}
	for _, pattern := range r.secrets {
		value = pattern.ReplaceAllString(value, `${1}[redacted]`)
	}
	for _, pattern := range r.exactValues {
		value = pattern.ReplaceAllString(value, `[redacted]`)
	}
	return value
}
