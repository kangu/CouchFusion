package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
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
