package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	webserver "github.com/nuxt-apps/couchfusion/internal/web"
)

type UIServer interface {
	Start(context.Context) (webserver.LaunchInfo, error)
	Shutdown(context.Context) error
}

type UIApplication interface {
	Shutdown(context.Context) error
}

type UIOptions struct {
	Listen      func(network, address string) (net.Listener, error)
	NewServer   func(net.Listener, string) (UIServer, UIApplication, error)
	OpenBrowser func(string) error
}

func RunUI(ctx context.Context, args []string, streams IO, options UIOptions) error {
	streams = streams.withDefaults()
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	port := flags.Int("port", 0, "Loopback port (0 selects an available port)")
	noOpen := flags.Bool("no-open", false, "Do not open the browser")
	configPath := flags.String("config", "", "Path to the global configuration file")
	if err := flags.Parse(args); err != nil {
		return ExitError{Code: 2, Err: err}
	}
	if flags.NArg() != 0 {
		return ExitError{Code: 2, Err: errors.New("ui does not accept positional arguments")}
	}
	if *port < 0 || *port > 65535 {
		return ExitError{Code: 2, Err: errors.New("port must be between 0 and 65535")}
	}
	if options.NewServer == nil {
		return errors.New("UI server factory is unavailable")
	}
	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)))
	if err != nil {
		return fmt.Errorf("start UI listener: %w", err)
	}
	server, application, err := options.NewServer(listener, *configPath)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("create UI server: %w", err)
	}
	launch, err := server.Start(ctx)
	if err != nil {
		_ = listener.Close()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = application.Shutdown(shutdownContext)
		return fmt.Errorf("start UI server: %w", err)
	}
	_, _ = fmt.Fprintf(streams.Out, "CouchFusion UI: %s\n", launch.URL)
	if !*noOpen {
		open := options.OpenBrowser
		if open == nil {
			open = openBrowser
		}
		if err := open(launch.URL); err != nil {
			_, _ = fmt.Fprintf(streams.Err, "Could not open a browser: %v\nOpen %s manually.\n", err, launch.URL)
		}
	}
	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(server.Shutdown(shutdownContext), application.Shutdown(shutdownContext))
}

func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}
