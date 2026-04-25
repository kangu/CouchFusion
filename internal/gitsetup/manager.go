package gitsetup

import (
	"context"
	"errors"
	"fmt"
)

var ErrConfirmationRequired = errors.New("git installation requires interactive confirmation or --yes")

type Result struct {
	Installed bool
	Status    Status
}

type Manager struct {
	Probe       Prober
	Installer   Installer
	Interactive bool
	AutoApprove bool

	PromptInstall func() (bool, error)
}

func (m Manager) Ensure(ctx context.Context) (Result, error) {
	prober := m.Probe
	if prober == nil {
		prober = CommandProber{}
	}
	status, err := prober.Probe(ctx)
	if err != nil {
		return Result{}, err
	}
	if status.Available {
		return Result{Installed: false, Status: status}, nil
	}

	if !m.AutoApprove {
		if !m.Interactive {
			return Result{}, ErrConfirmationRequired
		}
		if m.PromptInstall == nil {
			return Result{}, fmt.Errorf("%w: no prompt function configured", ErrConfirmationRequired)
		}
		confirmed, err := m.PromptInstall()
		if err != nil {
			return Result{}, err
		}
		if !confirmed {
			return Result{}, errors.New("git installation cancelled")
		}
	}

	installer := m.Installer
	if installer == nil {
		installer = SystemInstaller{}
	}
	installResult, err := installer.Install(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Installed: true,
		Status: Status{
			Available: true,
			Version:   installResult.Version,
			Path:      installResult.Path,
		},
	}, nil
}
