package devui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const missingDependenciesMessage = "Frontend dependencies are missing. Run: cd web && bun install"

var (
	lookPath = exec.LookPath
	command  = exec.Command
	listen   = net.Listen
)

type Options struct {
	Root           string
	Out            io.Writer
	Err            io.Writer
	StartupTimeout time.Duration
	StopGrace      time.Duration
}

type Process struct {
	url       *url.URL
	command   *exec.Cmd
	done      chan error
	exited    chan struct{}
	stopGrace time.Duration

	shutdownMu sync.Mutex
	mu         sync.Mutex
	stopping   bool
	exitErr    error
}

func Start(ctx context.Context, options Options) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Root == "" {
		root, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve CouchFusion source checkout: %w", err)
		}
		options.Root = root
	}
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.Err == nil {
		options.Err = io.Discard
	}
	if options.StartupTimeout <= 0 {
		options.StartupTimeout = 15 * time.Second
	}
	if options.StopGrace <= 0 {
		options.StopGrace = 2 * time.Second
	}

	webRoot := filepath.Join(options.Root, "web")
	if info, err := os.Stat(filepath.Join(webRoot, "package.json")); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("development mode must run from the CouchFusion source checkout containing web/package.json")
	}
	if info, err := os.Stat(filepath.Join(webRoot, "node_modules")); err != nil || !info.IsDir() {
		return nil, errors.New(missingDependenciesMessage)
	}
	bunPath, err := lookPath("bun")
	if err != nil {
		return nil, errors.New("Bun is required for UI development mode")
	}

	reservation, err := listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("reserve Nuxt development port: %w", err)
	}
	address, ok := reservation.Addr().(*net.TCPAddr)
	if !ok || address.Port == 0 {
		_ = reservation.Close()
		return nil, errors.New("reserve Nuxt development port: listener did not provide a TCP port")
	}
	port := address.Port
	if err := reservation.Close(); err != nil {
		return nil, fmt.Errorf("release Nuxt development port reservation: %w", err)
	}

	cmd := command(bunPath, "run", "dev", "--", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Dir = webRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("capture Nuxt stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("capture Nuxt stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Nuxt development server: %w", err)
	}
	target, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("create Nuxt development URL: %w", err)
	}
	process := &Process{
		url:       target,
		command:   cmd,
		done:      make(chan error, 1),
		exited:    make(chan struct{}),
		stopGrace: options.StopGrace,
	}

	var capture sync.WaitGroup
	capture.Add(2)
	go captureOutput(options.Out, stdout, &capture)
	go captureOutput(options.Err, stderr, &capture)
	go process.wait(&capture)

	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.NewTimer(options.StartupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		if nuxtReady(client, target.String()) {
			return process, nil
		}
		select {
		case <-ctx.Done():
			process.stopAfterFailedStartup()
			return nil, ctx.Err()
		case <-deadline.C:
			process.stopAfterFailedStartup()
			return nil, errors.New("Nuxt development server did not become ready before the startup timeout")
		case <-process.exited:
			if exitErr := process.completionError(); exitErr != nil {
				return nil, fmt.Errorf("start Nuxt development server: %w", exitErr)
			}
			return nil, errors.New("Nuxt development server exited before becoming ready")
		case <-ticker.C:
		}
	}
}

func (p *Process) URL() *url.URL { return p.url }

func (p *Process) Done() <-chan error { return p.done }

func (p *Process) Shutdown(ctx context.Context) error {
	p.shutdownMu.Lock()
	defer p.shutdownMu.Unlock()
	select {
	case <-p.exited:
		return nil
	default:
	}
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
	if err := p.command.Process.Signal(os.Interrupt); err != nil {
		select {
		case <-p.exited:
			return nil
		default:
			_ = p.command.Process.Kill()
		}
	}
	timer := time.NewTimer(p.stopGrace)
	defer timer.Stop()
	select {
	case <-p.exited:
		return nil
	case <-timer.C:
		_ = p.command.Process.Kill()
	case <-ctx.Done():
		_ = p.command.Process.Kill()
		select {
		case <-p.exited:
		case <-time.After(time.Second):
		}
		return ctx.Err()
	}
	select {
	case <-p.exited:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Process) wait(capture *sync.WaitGroup) {
	waitErr := p.command.Wait()
	capture.Wait()
	p.mu.Lock()
	if !p.stopping {
		if waitErr != nil {
			p.exitErr = fmt.Errorf("Nuxt development server exited unexpectedly: %w", waitErr)
		} else {
			p.exitErr = errors.New("Nuxt development server exited unexpectedly")
		}
	}
	completion := p.exitErr
	p.mu.Unlock()
	p.done <- completion
	close(p.done)
	close(p.exited)
}

func (p *Process) completionError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

func (p *Process) stopAfterFailedStartup() {
	ctx, cancel := context.WithTimeout(context.Background(), p.stopGrace+time.Second)
	defer cancel()
	_ = p.Shutdown(ctx)
}

func captureOutput(destination io.Writer, source io.Reader, wait *sync.WaitGroup) {
	defer wait.Done()
	scanner := bufio.NewScanner(source)
	for scanner.Scan() {
		_, _ = fmt.Fprintf(destination, "[nuxt] %s\n", scanner.Text())
	}
}

func nuxtReady(client *http.Client, address string) bool {
	response, err := client.Get(address)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return true
}
