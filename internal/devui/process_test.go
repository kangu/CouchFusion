package devui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestStartValidatesFrontendCheckout(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		want    string
	}{
		{
			name:    "missing web package",
			prepare: func(*testing.T, string) {},
			want:    "development mode must run from the CouchFusion source checkout",
		},
		{
			name: "missing dependencies",
			prepare: func(t *testing.T, root string) {
				web := filepath.Join(root, "web")
				if err := os.MkdirAll(web, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"scripts":{"dev":"nuxt dev"}}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "Frontend dependencies are missing. Run: cd web && bun install",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.prepare(t, root)
			_, err := Start(context.Background(), Options{Root: root})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestStartReportsMissingBun(t *testing.T) {
	root := validFrontendRoot(t)
	original := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = original })
	_, err := Start(context.Background(), Options{Root: root})
	if err == nil || !strings.Contains(err.Error(), "Bun is required for UI development mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestProcessBecomesReadyPrefixesOutputAndShutsDown(t *testing.T) {
	var output synchronizedBuffer
	process := startHelperProcess(t, "serve", &output)
	if process.URL().Hostname() != "127.0.0.1" {
		t.Fatalf("URL = %s", process.URL())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := process.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := process.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown = %v", err)
	}
	select {
	case err := <-process.Done():
		if err != nil {
			t.Fatalf("done = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("process did not finish")
	}
	if !strings.Contains(output.String(), "[nuxt] helper ready") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestProcessReportsUnexpectedExit(t *testing.T) {
	process := startHelperProcess(t, "exit-after-ready", nil)
	select {
	case err := <-process.Done():
		if err == nil || !strings.Contains(err.Error(), "Nuxt development server exited unexpectedly") {
			t.Fatalf("done = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process did not report its exit")
	}
}

func TestStartTimeoutStopsChild(t *testing.T) {
	root := validFrontendRoot(t)
	restore := installHelperCommand(t, "never-ready")
	defer restore()
	_, err := Start(context.Background(), Options{Root: root, StartupTimeout: 150 * time.Millisecond, StopGrace: 100 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("error = %v", err)
	}
}

func startHelperProcess(t *testing.T, mode string, output *synchronizedBuffer) *Process {
	t.Helper()
	root := validFrontendRoot(t)
	restore := installHelperCommand(t, mode)
	t.Cleanup(restore)
	options := Options{Root: root, StartupTimeout: 2 * time.Second, StopGrace: 250 * time.Millisecond}
	if output != nil {
		options.Out = output
		options.Err = output
	}
	process, err := Start(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = process.Shutdown(ctx)
	})
	return process
}

func validFrontendRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	web := filepath.Join(root, "web")
	if err := os.MkdirAll(filepath.Join(web, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"scripts":{"dev":"nuxt dev"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func installHelperCommand(t *testing.T, mode string) func() {
	t.Helper()
	originalLookPath := lookPath
	originalCommand := command
	lookPath = func(string) (string, error) { return os.Args[0], nil }
	command = func(_ string, arguments ...string) *exec.Cmd {
		port := arguments[len(arguments)-1]
		cmd := exec.Command(os.Args[0], "-test.run=TestDevUIHelperProcess", "--")
		cmd.Env = append(os.Environ(),
			"GO_WANT_DEVUI_HELPER=1",
			"COUCHFUSION_DEVUI_HELPER_MODE="+mode,
			"COUCHFUSION_DEVUI_HELPER_PORT="+port,
		)
		return cmd
	}
	return func() {
		lookPath = originalLookPath
		command = originalCommand
	}
}

func TestDevUIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_DEVUI_HELPER") != "1" {
		return
	}
	if os.Getenv("COUCHFUSION_DEVUI_HELPER_MODE") == "never-ready" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		<-signals
		os.Exit(0)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+os.Getenv("COUCHFUSION_DEVUI_HELPER_PORT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(listener) }()
	fmt.Println("helper ready")
	if os.Getenv("COUCHFUSION_DEVUI_HELPER_MODE") == "exit-after-ready" {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	_ = server.Close()
	os.Exit(0)
}

type synchronizedBuffer struct {
	mu    sync.Mutex
	value strings.Builder
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.value.Write(value)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.value.String()
}
