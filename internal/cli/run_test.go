package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer

	err := Run(context.Background(), []string{"version"}, IO{
		In:  strings.NewReader(""),
		Out: &out,
		Err: io.Discard,
	}, Commands{})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != Version {
		t.Fatalf("version = %q, want %q", got, Version)
	}
}

func TestRunVersionAliases(t *testing.T) {
	for _, alias := range []string{"--version", "-v"} {
		t.Run(alias, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(context.Background(), []string{alias}, IO{Out: &out}, Commands{}); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := strings.TrimSpace(out.String()); got != Version {
				t.Fatalf("version = %q, want %q", got, Version)
			}
		})
	}
}

func TestRunRejectsMissingCommand(t *testing.T) {
	err := Run(context.Background(), nil, IO{Out: io.Discard, Err: io.Discard}, Commands{})

	assertExitError(t, err, 2, "command required")
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := Run(context.Background(), []string{"unknown"}, IO{Out: io.Discard, Err: io.Discard}, Commands{})

	assertExitError(t, err, 2, "unknown command: unknown")
}

func TestRunDispatchesCommandArguments(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextKey("request"), "expected")
	var gotCtx context.Context
	var gotArgs []string
	commands := Commands{
		New: func(commandCtx context.Context, args []string) error {
			gotCtx = commandCtx
			gotArgs = append([]string{}, args...)
			return nil
		},
	}

	err := Run(ctx, []string{"new", "site-name", "--modules", "auth"}, IO{Out: io.Discard}, commands)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotCtx.Value(contextKey("request")) != "expected" {
		t.Fatalf("command context did not preserve caller values")
	}
	wantArgs := []string{"site-name", "--modules", "auth"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestRunReturnsCommandError(t *testing.T) {
	want := errors.New("create failed")
	commands := Commands{
		CreateLayer: func(context.Context, []string) error { return want },
	}

	err := Run(context.Background(), []string{"create_layer"}, IO{Out: io.Discard}, commands)

	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	var exitErr ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("Run() error = %#v, want ExitError code 1", err)
	}
}

func TestRunRejectsUnavailableCommand(t *testing.T) {
	err := Run(context.Background(), []string{"init"}, IO{Out: io.Discard}, Commands{})

	assertExitError(t, err, 1, "init command is unavailable")
}

func TestIOWithNilStreamsUsesSafeDefaults(t *testing.T) {
	commands := Commands{
		Init: func(context.Context, []string) error { return nil },
	}

	if err := Run(context.Background(), []string{"init"}, IO{}, commands); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

type contextKey string

func assertExitError(t *testing.T, err error, wantCode int, wantMessage string) {
	t.Helper()
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %#v, want ExitError", err)
	}
	if exitErr.Code != wantCode {
		t.Fatalf("exit code = %d, want %d", exitErr.Code, wantCode)
	}
	if exitErr.Error() != wantMessage {
		t.Fatalf("error = %q, want %q", exitErr.Error(), wantMessage)
	}
}
