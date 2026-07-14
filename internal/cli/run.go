package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Version = "0.5.0"

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Command func(context.Context, []string) error

type Commands struct {
	Init        Command
	New         Command
	CreateLayer Command
	UI          Command
}

type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return "command failed"
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error {
	return e.Err
}

func Run(ctx context.Context, args []string, streams IO, commands Commands) error {
	streams = streams.withDefaults()
	if len(args) == 0 {
		return ExitError{Code: 2, Err: errors.New("command required")}
	}

	switch args[0] {
	case "version", "--version", "-v":
		_, err := fmt.Fprintln(streams.Out, Version)
		return err
	case "init":
		return dispatch(ctx, "init", args[1:], commands.Init)
	case "new":
		return dispatch(ctx, "new", args[1:], commands.New)
	case "create_layer":
		return dispatch(ctx, "create_layer", args[1:], commands.CreateLayer)
	case "ui":
		return dispatch(ctx, "ui", args[1:], commands.UI)
	default:
		return ExitError{Code: 2, Err: fmt.Errorf("unknown command: %s", args[0])}
	}
}

func dispatch(ctx context.Context, name string, args []string, command Command) error {
	if command == nil {
		return ExitError{Code: 1, Err: fmt.Errorf("%s command is unavailable", name)}
	}
	err := command(ctx, args)
	if err == nil {
		return nil
	}
	var exitError ExitError
	if errors.As(err, &exitError) {
		return err
	}
	return ExitError{Code: 1, Err: err}
}

func (streams IO) withDefaults() IO {
	if streams.In == nil {
		streams.In = strings.NewReader("")
	}
	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.Err == nil {
		streams.Err = io.Discard
	}
	return streams
}
