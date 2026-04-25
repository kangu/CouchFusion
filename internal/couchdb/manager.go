package couchdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrConfirmationRequired = errors.New("couchdb install or acknowledgement requires interactive confirmation or --yes")

type Installer interface {
	Install(context.Context, InstallOptions) (InstallResult, error)
}

type InstallOptions struct {
	URL       string
	AdminUser string
}

type InstallResult struct {
	URL       string
	AdminUser string
	AdminAuth string
	Version   string
}

type Result struct {
	Installed bool
	Status    Status
	StorePath string
}

type Manager struct {
	Store       Store
	Probe       Prober
	Installer   Installer
	Interactive bool
	AutoApprove bool

	PromptProceed func(Status) (bool, error)
	PromptInstall func() (bool, error)
}

func (m Manager) Ensure(ctx context.Context) (Result, error) {
	store := m.Store
	if strings.TrimSpace(store.Path) == "" {
		var err error
		store, err = DefaultStore()
		if err != nil {
			return Result{}, err
		}
	}

	prober := m.Probe
	if prober == nil {
		prober = HTTPProber{BaseURL: DefaultURL}
	}

	status, err := prober.Probe(ctx)
	if err != nil {
		return Result{}, err
	}
	if status.URL == "" {
		status.URL = DefaultURL
	}

	if status.Running {
		if err := m.ensureAccepted(store, status); err != nil {
			return Result{}, err
		}
		return Result{Installed: false, Status: status, StorePath: store.Path}, nil
	}

	if !m.AutoApprove {
		if !m.Interactive {
			return Result{}, ErrConfirmationRequired
		}
		confirmed, err := promptOrDefault(m.PromptInstall)
		if err != nil {
			return Result{}, err
		}
		if !confirmed {
			return Result{}, errors.New("couchdb installation cancelled")
		}
	}

	installer := m.Installer
	if installer == nil {
		installer = SystemInstaller{}
	}
	installResult, err := installer.Install(ctx, InstallOptions{
		URL:       status.URL,
		AdminUser: defaultAdmin,
	})
	if err != nil {
		return Result{}, err
	}
	if installResult.URL == "" {
		installResult.URL = DefaultURL
	}
	if installResult.AdminUser == "" {
		installResult.AdminUser = defaultAdmin
	}

	acceptedAt := time.Now().UTC()
	if err := store.Save(Credentials{
		URL:             installResult.URL,
		AdminUser:       installResult.AdminUser,
		AdminAuth:       installResult.AdminAuth,
		AcceptedVersion: installResult.Version,
		AcceptedAt:      acceptedAt,
	}); err != nil {
		return Result{}, err
	}

	return Result{
		Installed: true,
		Status: Status{
			Running: true,
			URL:     installResult.URL,
			Version: installResult.Version,
		},
		StorePath: store.Path,
	}, nil
}

func (m Manager) ensureAccepted(store Store, status Status) error {
	creds, ok, err := store.Load()
	if err != nil {
		return err
	}
	if ok && strings.TrimSpace(creds.AcceptedVersion) != "" {
		return nil
	}

	if !m.AutoApprove {
		if !m.Interactive {
			return ErrConfirmationRequired
		}
		confirmed, err := promptOrDefaultStatus(m.PromptProceed, status)
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("couchdb acknowledgement cancelled")
		}
	}

	return store.Save(Credentials{
		URL:             status.URL,
		AdminUser:       defaultAdmin,
		AdminAuth:       creds.AdminAuth,
		AcceptedVersion: status.Version,
		AcceptedAt:      time.Now().UTC(),
	})
}

func promptOrDefault(fn func() (bool, error)) (bool, error) {
	if fn == nil {
		return false, fmt.Errorf("%w: no prompt function configured", ErrConfirmationRequired)
	}
	return fn()
}

func promptOrDefaultStatus(fn func(Status) (bool, error), status Status) (bool, error) {
	if fn == nil {
		return false, fmt.Errorf("%w: no prompt function configured", ErrConfirmationRequired)
	}
	return fn(status)
}
