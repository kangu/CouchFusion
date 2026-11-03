package main

import (
	"context"
	"errors"
	"time"

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

func shutdownUIRuntime(runtime uiRuntime) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return runtime.Shutdown(ctx)
}
