package appserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
	"github.com/nuxt-apps/couchfusion/internal/agent/protocol"
)

var (
	ErrUnavailable = errors.New("Codex app-server is unavailable")
	ErrUnsupported = errors.New("Codex app-server version is unsupported")
)

var supportedVersion = regexp.MustCompile(`^codex-cli (0\.144\.[0-9]+)(?:\s|$)`)

type Options struct {
	Command        func(context.Context) *exec.Cmd
	VersionCommand func(context.Context) *exec.Cmd
	SkillRoot      string
	Version        string
	Backoff        time.Duration
}

type Supervisor struct {
	mu          sync.Mutex
	options     Options
	rootCtx     context.Context
	rootCancel  context.CancelFunc
	client      *protocol.Client
	command     *exec.Cmd
	processDone <-chan error
	generation  uint64
	readiness   model.AgentReadiness
	shutting    bool
	events      chan protocol.Notification
	requests    chan protocol.ServerRequest
}

func NewSupervisor(options Options) *Supervisor {
	if options.Command == nil {
		options.Command = func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, "codex", "app-server", "--stdio")
		}
	}
	if options.VersionCommand == nil {
		options.VersionCommand = func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, "codex", "--version")
		}
	}
	if options.Backoff <= 0 {
		options.Backoff = 250 * time.Millisecond
	}
	rootCtx, rootCancel := context.WithCancel(context.Background())
	return &Supervisor{
		options: options, rootCtx: rootCtx, rootCancel: rootCancel,
		events: make(chan protocol.Notification, 256), requests: make(chan protocol.ServerRequest, 64),
	}
}

func (s *Supervisor) EnsureReady(ctx context.Context) (model.AgentReadiness, error) {
	if err := ctx.Err(); err != nil {
		return model.AgentReadiness{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutting {
		return s.readiness, ErrUnavailable
	}
	if s.client != nil {
		select {
		case <-s.client.Done():
			s.client = nil
			s.command = nil
		default:
			return s.readiness, nil
		}
	}

	versionOutput, err := s.options.VersionCommand(ctx).CombinedOutput()
	if err != nil {
		status := model.AgentLaunchFailed
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			status = model.AgentMissing
		}
		s.readiness = model.AgentReadiness{Status: status, Problem: readinessProblem(status)}
		return s.readiness, fmt.Errorf("%w: version probe failed", ErrUnavailable)
	}
	match := supportedVersion.FindStringSubmatch(strings.TrimSpace(string(versionOutput)))
	if len(match) != 2 {
		s.readiness = model.AgentReadiness{Status: model.AgentUnsupported, Problem: "Installed Codex is not compatible with this CouchFusion build"}
		return s.readiness, ErrUnsupported
	}

	readiness, err := s.startLocked(ctx, match[1])
	s.readiness = readiness
	return readiness, err
}

func (s *Supervisor) Client(ctx context.Context) (*protocol.Client, error) {
	if _, err := s.EnsureReady(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, ErrUnavailable
	}
	return s.client, nil
}

func (s *Supervisor) Events() <-chan protocol.Notification { return s.events }

func (s *Supervisor) Requests() <-chan protocol.ServerRequest { return s.requests }

func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.shutting {
		s.mu.Unlock()
		return nil
	}
	s.shutting = true
	s.rootCancel()
	client := s.client
	command := s.command
	processDone := s.processDone
	s.client = nil
	s.command = nil
	s.processDone = nil
	s.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	if command == nil || command.Process == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-processDone:
		return nil
	}
}

func (s *Supervisor) startLocked(ctx context.Context, version string) (model.AgentReadiness, error) {
	command := s.options.Command(s.rootCtx)
	if command == nil {
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, ErrUnavailable
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, ErrUnavailable
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, ErrUnavailable
	}
	command.Stderr = &boundedBuffer{limit: 64 << 10}
	if err := command.Start(); err != nil {
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, fmt.Errorf("%w: app-server launch failed", ErrUnavailable)
	}
	client := protocol.NewClient(stdin, stdout, protocol.Options{})
	failed := func() {
		_ = client.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}
	var initialized protocol.InitializeResponse
	if err := client.Call(ctx, "initialize", protocol.InitializeParams{
		ClientInfo:   protocol.ClientInfo{Name: "couchfusion", Title: stringPointer("CouchFusion"), Version: defaultString(s.options.Version, "dev")},
		Capabilities: &protocol.InitializeCapabilities{ExperimentalAPI: true},
	}, &initialized); err != nil {
		failed()
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, fmt.Errorf("%w: initialize failed", ErrUnavailable)
	}
	if err := client.Notify(ctx, "initialized", nil); err != nil {
		failed()
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, ErrUnavailable
	}
	if err := client.Call(ctx, "skills/extraRoots/set", protocol.SkillsExtraRootsSetParams{ExtraRoots: nonEmptyStrings(s.options.SkillRoot)}, nil); err != nil {
		failed()
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, ErrUnavailable
	}
	var account protocol.AccountReadResponse
	if err := client.Call(ctx, "account/read", protocol.AccountReadParams{}, &account); err != nil {
		failed()
		return model.AgentReadiness{Status: model.AgentLaunchFailed, Version: version, Problem: readinessProblem(model.AgentLaunchFailed)}, ErrUnavailable
	}
	if account.RequiresOpenAIAuth && account.Account == nil {
		failed()
		return model.AgentReadiness{Status: model.AgentUnauthenticated, Version: version, Problem: "Authenticate with the local Codex installation"}, ErrUnavailable
	}
	s.generation++
	generation := s.generation
	processDone := make(chan error, 1)
	s.client = client
	s.command = command
	s.processDone = processDone
	go func() {
		err := command.Wait()
		processDone <- err
		close(processDone)
		s.handleExit(generation, client)
	}()
	go s.forwardClient(client)
	return model.AgentReadiness{Status: model.AgentReady, Available: true, Version: version, AccountType: accountType(account.Account)}, nil
}

func (s *Supervisor) forwardClient(client *protocol.Client) {
	for {
		select {
		case <-s.rootCtx.Done():
			return
		case <-client.Done():
			return
		case event := <-client.Notifications():
			select {
			case s.events <- event:
			case <-s.rootCtx.Done():
				return
			}
		case request := <-client.ServerRequests():
			select {
			case s.requests <- request:
			case <-s.rootCtx.Done():
				return
			}
		}
	}
}

func (s *Supervisor) handleExit(generation uint64, client *protocol.Client) {
	s.mu.Lock()
	if s.shutting || generation != s.generation || s.client != client {
		s.mu.Unlock()
		return
	}
	s.client = nil
	s.command = nil
	s.processDone = nil
	s.readiness = model.AgentReadiness{Status: model.AgentRestarting, Version: s.readiness.Version, Problem: "Codex app-server is restarting"}
	backoff := s.options.Backoff
	s.mu.Unlock()
	_ = client.Close()

	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-s.rootCtx.Done():
		return
	case <-timer.C:
		_, _ = s.EnsureReady(s.rootCtx)
	}
}

func readinessProblem(status model.AgentReadinessStatus) string {
	if status == model.AgentMissing {
		return "Codex was not found on PATH"
	}
	return "Codex app-server could not be started"
}

func accountType(account *protocol.Account) string {
	if account == nil {
		return ""
	}
	return account.Type
}

func stringPointer(value string) *string { return &value }

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nonEmptyStrings(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	return []string{value}
}

type boundedBuffer struct {
	mu    sync.Mutex
	limit int
	data  bytes.Buffer
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(value) > b.limit {
		excess := b.data.Len() + len(value) - b.limit
		remaining := append([]byte(nil), b.data.Bytes()[min(excess, b.data.Len()):]...)
		b.data.Reset()
		_, _ = b.data.Write(remaining)
	}
	_, _ = b.data.Write(value[max(0, len(value)-b.limit):])
	return len(value), nil
}

var _ io.Writer = (*boundedBuffer)(nil)
