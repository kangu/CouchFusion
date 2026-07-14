package main

import (
	"context"
	"errors"

	"github.com/nuxt-apps/couchfusion/internal/cli"
)

type developmentRuntime interface {
	Done() <-chan error
	Shutdown(context.Context) error
}

type uiRuntime struct {
	application cli.UIApplication
	development developmentRuntime
}

func (r uiRuntime) Done() <-chan error {
	if r.development == nil {
		return nil
	}
	return r.development.Done()
}

func (r uiRuntime) Shutdown(ctx context.Context) error {
	var developmentErr error
	if r.development != nil {
		developmentErr = r.development.Shutdown(ctx)
	}
	return errors.Join(developmentErr, r.application.Shutdown(ctx))
}
