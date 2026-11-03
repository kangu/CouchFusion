package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestUIRuntimeWithoutDevelopmentHasNoCompletionChannel(t *testing.T) {
	runtime := uiRuntime{application: &runtimeApplicationFake{}}
	if runtime.Done() != nil {
		t.Fatal("normal UI runtime exposes a completion channel")
	}
}

func TestUIRuntimeStopsDevelopmentBeforeApplicationAndJoinsErrors(t *testing.T) {
	order := []string{}
	done := make(chan error)
	developmentErr := errors.New("development shutdown")
	applicationErr := errors.New("application shutdown")
	runtime := uiRuntime{
		development: &developmentRuntimeFake{done: done, shutdown: func(context.Context) error {
			order = append(order, "development")
			return developmentErr
		}},
		application: &runtimeApplicationFake{shutdown: func(context.Context) error {
			order = append(order, "application")
			return applicationErr
		}},
	}
	if runtime.Done() != done {
		t.Fatal("development completion channel was not exposed")
	}
	err := runtime.Shutdown(context.Background())
	if !errors.Is(err, developmentErr) || !errors.Is(err, applicationErr) {
		t.Fatalf("shutdown error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"development", "application"}) {
		t.Fatalf("shutdown order = %#v", order)
	}
}

func TestShutdownUIRuntimeUsesBoundedContext(t *testing.T) {
	checked := false
	runtime := uiRuntime{application: &runtimeApplicationFake{shutdown: func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("shutdown context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > 10*time.Second {
			t.Fatalf("shutdown deadline remaining = %s", remaining)
		}
		checked = true
		return nil
	}}}
	if err := shutdownUIRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("application shutdown was not called")
	}
}

type runtimeApplicationFake struct {
	shutdown func(context.Context) error
}

func (f *runtimeApplicationFake) Shutdown(ctx context.Context) error {
	if f.shutdown == nil {
		return nil
	}
	return f.shutdown(ctx)
}

type developmentRuntimeFake struct {
	done     <-chan error
	shutdown func(context.Context) error
}

func (f *developmentRuntimeFake) Done() <-chan error { return f.done }
func (f *developmentRuntimeFake) Shutdown(ctx context.Context) error {
	if f.shutdown == nil {
		return nil
	}
	return f.shutdown(ctx)
}
