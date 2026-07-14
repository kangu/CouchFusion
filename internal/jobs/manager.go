package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound          = errors.New("job was not found")
	ErrShuttingDown      = errors.New("job manager is shutting down")
	ErrSecretForbidden   = errors.New("job secret belongs to another session")
	ErrSecretUnavailable = errors.New("job secret is unavailable")
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Event struct {
	ID    uint64    `json:"id"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Code  string    `json:"code"`
	Text  string    `json:"text"`
}

type SafeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Snapshot struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	WorkspaceID string     `json:"workspaceId,omitempty"`
	SiteID      string     `json:"siteId,omitempty"`
	Status      Status     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   time.Time  `json:"startedAt,omitempty"`
	CompletedAt time.Time  `json:"completedAt,omitempty"`
	Events      []Event    `json:"events"`
	Error       *SafeError `json:"error,omitempty"`
}

type Reporter interface {
	Event(level, code, text string)
	Secret(value string)
}

type Request struct {
	Type        string
	WorkspaceID string
	SiteID      string
	SessionID   string
	Run         func(context.Context, Reporter) error
}

type Options struct {
	MaxEvents  int
	MaxJobs    int
	SecretKeys []string
	Now        func() time.Time
	NewID      func() (string, error)
}

type ConflictError struct {
	WorkspaceID string
	ActiveJobID string
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("workspace %q already has active job %q", e.WorkspaceID, e.ActiveJobID)
}

type Manager struct {
	mu             sync.Mutex
	jobs           map[string]*job
	order          []string
	workspaceLocks map[string]string
	options        Options
	redactor       redactor
	rootContext    context.Context
	rootCancel     context.CancelFunc
	shuttingDown   bool
	wg             sync.WaitGroup
}

type job struct {
	snapshot       Snapshot
	cancel         context.CancelFunc
	nextEventID    uint64
	subscribers    map[uint64]chan Event
	nextSubscriber uint64
	secret         string
	secretSession  string
	secretConsumed bool
}

type jobReporter struct {
	manager *Manager
	jobID   string
}

func NewManager(options Options) *Manager {
	if options.MaxEvents <= 0 {
		options.MaxEvents = 200
	}
	if options.MaxJobs <= 0 {
		options.MaxJobs = 100
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.NewID == nil {
		options.NewID = randomID
	}
	rootContext, rootCancel := context.WithCancel(context.Background())
	return &Manager{
		jobs:           map[string]*job{},
		workspaceLocks: map[string]string{},
		options:        options,
		redactor:       newRedactor(options.SecretKeys),
		rootContext:    rootContext,
		rootCancel:     rootCancel,
	}
}

func (m *Manager) Start(request Request) (Snapshot, error) {
	if request.Run == nil {
		return Snapshot{}, errors.New("job run function is required")
	}
	if strings.TrimSpace(request.Type) == "" {
		return Snapshot{}, errors.New("job type is required")
	}

	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return Snapshot{}, ErrShuttingDown
	}
	if request.WorkspaceID != "" {
		if activeID, exists := m.workspaceLocks[request.WorkspaceID]; exists {
			m.mu.Unlock()
			return Snapshot{}, ConflictError{WorkspaceID: request.WorkspaceID, ActiveJobID: activeID}
		}
	}
	m.pruneCompletedLocked()
	id, err := m.options.NewID()
	if err != nil {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("create job id: %w", err)
	}
	if _, exists := m.jobs[id]; exists {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("create job id: duplicate id %q", id)
	}
	ctx, cancel := context.WithCancel(m.rootContext)
	created := m.options.Now()
	state := &job{
		snapshot: Snapshot{
			ID:          id,
			Type:        request.Type,
			WorkspaceID: request.WorkspaceID,
			SiteID:      request.SiteID,
			Status:      Queued,
			CreatedAt:   created,
			Events:      []Event{},
		},
		cancel:        cancel,
		subscribers:   map[uint64]chan Event{},
		secretSession: request.SessionID,
	}
	m.jobs[id] = state
	m.order = append(m.order, id)
	if request.WorkspaceID != "" {
		m.workspaceLocks[request.WorkspaceID] = id
	}
	snapshot := cloneSnapshot(state.snapshot)
	m.wg.Add(1)
	m.mu.Unlock()

	go m.execute(ctx, id, request.Run)
	return snapshot, nil
}

func (m *Manager) Get(id string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, exists := m.jobs[id]
	if !exists {
		return Snapshot{}, ErrNotFound
	}
	return cloneSnapshot(state.snapshot), nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	state, exists := m.jobs[id]
	if !exists {
		m.mu.Unlock()
		return ErrNotFound
	}
	if terminal(state.snapshot.Status) {
		m.mu.Unlock()
		return nil
	}
	cancel := state.cancel
	m.mu.Unlock()
	cancel()
	return nil
}

func (m *Manager) Subscribe(id string, afterEventID uint64) (<-chan Event, func(), error) {
	m.mu.Lock()
	state, exists := m.jobs[id]
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
			state, exists := m.jobs[id]
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

func (m *Manager) ConsumeSecret(id, sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, exists := m.jobs[id]
	if !exists {
		return "", ErrNotFound
	}
	if state.secretSession == "" || state.secretSession != sessionID {
		return "", ErrSecretForbidden
	}
	if state.snapshot.Status != Succeeded || state.secret == "" || state.secretConsumed {
		return "", ErrSecretUnavailable
	}
	secret := state.secret
	state.secret = ""
	state.secretConsumed = true
	return secret, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if !m.shuttingDown {
		m.shuttingDown = true
		m.rootCancel()
	}
	cancels := make([]context.CancelFunc, 0, len(m.jobs))
	for _, state := range m.jobs {
		if !terminal(state.snapshot.Status) {
			cancels = append(cancels, state.cancel)
		}
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		m.mu.Lock()
		for _, state := range m.jobs {
			state.secret = ""
			state.secretConsumed = true
		}
		m.mu.Unlock()
		return nil
	}
}

func (m *Manager) execute(ctx context.Context, id string, run func(context.Context, Reporter) error) {
	defer m.wg.Done()
	var runErr error
	panicked := false
	defer func() {
		if recover() != nil {
			panicked = true
		}
		m.finish(id, ctx.Err(), runErr, panicked)
	}()

	m.mu.Lock()
	state, exists := m.jobs[id]
	if !exists {
		m.mu.Unlock()
		return
	}
	state.snapshot.Status = Running
	state.snapshot.StartedAt = m.options.Now()
	m.addEventLocked(state, "info", "job.started", "Job started")
	m.mu.Unlock()

	runErr = run(ctx, jobReporter{manager: m, jobID: id})
}

func (m *Manager) finish(id string, contextErr, runErr error, panicked bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, exists := m.jobs[id]
	if !exists {
		return
	}
	switch {
	case contextErr != nil:
		state.snapshot.Status = Cancelled
		state.snapshot.Error = &SafeError{Code: "cancelled", Message: "Job was cancelled"}
		m.addEventLocked(state, "warn", "job.cancelled", "Job cancelled")
		state.secret = ""
		state.secretConsumed = true
	case panicked:
		state.snapshot.Status = Failed
		state.snapshot.Error = &SafeError{Code: "internal", Message: "Job failed unexpectedly"}
		m.addEventLocked(state, "error", "job.failed", "Job failed unexpectedly")
		state.secret = ""
		state.secretConsumed = true
	case runErr != nil:
		message := m.redactor.Text(runErr.Error())
		state.snapshot.Status = Failed
		state.snapshot.Error = &SafeError{Code: "job_failed", Message: message}
		m.addEventLocked(state, "error", "job.failed", message)
		state.secret = ""
		state.secretConsumed = true
	default:
		state.snapshot.Status = Succeeded
		m.addEventLocked(state, "success", "job.succeeded", "Job completed")
	}
	state.snapshot.CompletedAt = m.options.Now()
	if workspaceID := state.snapshot.WorkspaceID; workspaceID != "" && m.workspaceLocks[workspaceID] == id {
		delete(m.workspaceLocks, workspaceID)
	}
	for subscriberID, subscriber := range state.subscribers {
		close(subscriber)
		delete(state.subscribers, subscriberID)
	}
}

func (r jobReporter) Event(level, code, text string) {
	r.manager.mu.Lock()
	defer r.manager.mu.Unlock()
	state, exists := r.manager.jobs[r.jobID]
	if !exists || terminal(state.snapshot.Status) {
		return
	}
	r.manager.addEventLocked(state, level, code, text)
}

func (r jobReporter) Secret(value string) {
	r.manager.mu.Lock()
	defer r.manager.mu.Unlock()
	state, exists := r.manager.jobs[r.jobID]
	if !exists || terminal(state.snapshot.Status) || state.secretSession == "" {
		return
	}
	state.secret = value
	state.secretConsumed = false
}

func (m *Manager) addEventLocked(state *job, level, code, text string) {
	state.nextEventID++
	event := Event{
		ID:    state.nextEventID,
		Time:  m.options.Now(),
		Level: strings.TrimSpace(level),
		Code:  strings.TrimSpace(code),
		Text:  m.redactor.Text(text),
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

func (m *Manager) pruneCompletedLocked() {
	for len(m.jobs) >= m.options.MaxJobs {
		removed := false
		for index, id := range m.order {
			state := m.jobs[id]
			if state == nil || !terminal(state.snapshot.Status) {
				continue
			}
			delete(m.jobs, id)
			m.order = append(m.order[:index], m.order[index+1:]...)
			removed = true
			break
		}
		if !removed {
			return
		}
	}
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	copy := snapshot
	copy.Events = append([]Event{}, snapshot.Events...)
	if snapshot.Error != nil {
		errorCopy := *snapshot.Error
		copy.Error = &errorCopy
	}
	return copy
}

func terminal(status Status) bool {
	return status == Succeeded || status == Failed || status == Cancelled
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

type redactor struct {
	userinfo        *regexp.Regexp
	sensitiveHeader []*regexp.Regexp
	secrets         []*regexp.Regexp
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
	return value
}
