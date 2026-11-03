package cli

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	webserver "github.com/nuxt-apps/couchfusion/internal/web"
)

func TestUIUsesRandomLoopbackPortAndOpensTokenizedURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := &uiServerFake{launch: webserver.LaunchInfo{Address: "127.0.0.1:43123", URL: "http://127.0.0.1:43123/?token=secret"}, started: cancel}
	application := &uiApplicationFake{}
	var address, opened, configPath string
	err := RunUI(ctx, []string{"--config", "/tmp/cf.yaml"}, IO{}, UIOptions{
		Listen: func(_, requested string) (net.Listener, error) { address = requested; return loopbackListener(t), nil },
		NewServer: func(_ net.Listener, config string, _ bool) (UIServer, UIApplication, error) {
			configPath = config
			return server, application, nil
		},
		OpenBrowser: func(url string) error { opened = url; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "127.0.0.1:0" {
		t.Fatalf("listen address = %q", address)
	}
	if opened != server.launch.URL {
		t.Fatalf("opened = %q", opened)
	}
	if configPath != "/tmp/cf.yaml" {
		t.Fatalf("config = %q", configPath)
	}
	if server.shutdownCalls != 1 || application.shutdownCalls != 1 {
		t.Fatalf("shutdown calls = server %d, app %d", server.shutdownCalls, application.shutdownCalls)
	}
}

func TestUIHonorsExplicitPortAndNoOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := &uiServerFake{launch: webserver.LaunchInfo{URL: "http://127.0.0.1:4400/?token=x"}, started: cancel}
	var address string
	opened := false
	err := RunUI(ctx, []string{"--port", "4400", "--no-open"}, IO{}, UIOptions{
		Listen: func(_, requested string) (net.Listener, error) { address = requested; return loopbackListener(t), nil },
		NewServer: func(net.Listener, string, bool) (UIServer, UIApplication, error) {
			return server, &uiApplicationFake{}, nil
		},
		OpenBrowser: func(string) error { opened = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "127.0.0.1:4400" || opened {
		t.Fatalf("address = %q, opened = %v", address, opened)
	}
}

func TestUIRejectsInvalidPort(t *testing.T) {
	err := RunUI(context.Background(), []string{"--port", "70000"}, IO{}, UIOptions{})
	assertExitError(t, err, 2, "port must be between 0 and 65535")
}

func TestUIBrowserFailureIsWarningNotServerFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := &uiServerFake{launch: webserver.LaunchInfo{URL: "http://127.0.0.1:1/?token=x"}, started: cancel}
	var stderr bytes.Buffer
	err := RunUI(ctx, nil, IO{Err: &stderr}, UIOptions{
		Listen: func(_, _ string) (net.Listener, error) { return loopbackListener(t), nil },
		NewServer: func(net.Listener, string, bool) (UIServer, UIApplication, error) {
			return server, &uiApplicationFake{}, nil
		},
		OpenBrowser: func(string) error { return errors.New("no browser") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "http://127.0.0.1:1/?token=x") {
		t.Fatalf("warning = %q", stderr.String())
	}
}

func TestRunDispatchesUI(t *testing.T) {
	called := false
	err := Run(context.Background(), []string{"ui", "--no-open"}, IO{}, Commands{UI: func(_ context.Context, args []string) error {
		called = len(args) == 1 && args[0] == "--no-open"
		return nil
	}})
	if err != nil || !called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestUIPropagatesDevelopmentMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := &uiServerFake{launch: webserver.LaunchInfo{URL: "http://127.0.0.1:4400/?token=x"}, started: cancel}
	development := false
	err := RunUI(ctx, []string{"--dev", "--no-open"}, IO{}, UIOptions{
		Listen: func(_, _ string) (net.Listener, error) { return loopbackListener(t), nil },
		NewServer: func(_ net.Listener, _ string, dev bool) (UIServer, UIApplication, error) {
			development = dev
			return server, &uiApplicationFake{}, nil
		},
	})
	if err != nil || !development {
		t.Fatalf("err = %v, development = %v", err, development)
	}
}

func TestUIReturnsUnexpectedRuntimeFailure(t *testing.T) {
	runtimeDone := make(chan error, 1)
	runtimeDone <- errors.New("Nuxt development server exited unexpectedly")
	close(runtimeDone)
	application := &uiApplicationFake{done: runtimeDone}
	server := &uiServerFake{launch: webserver.LaunchInfo{URL: "http://127.0.0.1:4400/?token=x"}}
	err := RunUI(context.Background(), []string{"--dev", "--no-open"}, IO{}, UIOptions{
		Listen: func(_, _ string) (net.Listener, error) { return loopbackListener(t), nil },
		NewServer: func(net.Listener, string, bool) (UIServer, UIApplication, error) {
			return server, application, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Nuxt development server exited unexpectedly") {
		t.Fatalf("error = %v", err)
	}
	if server.shutdownCalls != 1 || application.shutdownCalls != 1 {
		t.Fatalf("shutdown = server %d application %d", server.shutdownCalls, application.shutdownCalls)
	}
}

type uiServerFake struct {
	launch        webserver.LaunchInfo
	started       context.CancelFunc
	shutdownCalls int
}

func (s *uiServerFake) Start(context.Context) (webserver.LaunchInfo, error) {
	if s.started != nil {
		s.started()
	}
	return s.launch, nil
}
func (s *uiServerFake) Shutdown(context.Context) error { s.shutdownCalls++; return nil }

type uiApplicationFake struct {
	shutdownCalls int
	done          <-chan error
}

func (a *uiApplicationFake) Shutdown(context.Context) error { a.shutdownCalls++; return nil }
func (a *uiApplicationFake) Done() <-chan error             { return a.done }
func loopbackListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener
}
