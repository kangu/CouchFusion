package bun

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

type Status struct {
	Available bool
	Version   string
	Path      string
}

type Prober interface {
	Probe(context.Context) (Status, error)
}

type CommandProber struct {
	Env []string
}

func (p CommandProber) Probe(ctx context.Context) (Status, error) {
	path, err := exec.LookPath("bun")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Status{Available: false}, nil
		}
		return Status{}, err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	if len(p.Env) > 0 {
		cmd.Env = p.Env
	}
	output, err := cmd.Output()
	if err != nil {
		return Status{Available: false, Path: path}, nil
	}
	return Status{Available: true, Path: path, Version: strings.TrimSpace(string(output))}, nil
}
