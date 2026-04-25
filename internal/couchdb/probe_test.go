package couchdb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestProbeDetectsRunningVersion(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"status":"ok"}`
		if req.URL.Path == "/" {
			body = `{"couchdb":"Welcome","version":"3.4.2"}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Header:     http.Header{},
			Request:    req,
		}, nil
	})}

	status, err := Probe(context.Background(), "http://couchdb.local", client)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !status.Running {
		t.Fatal("Running = false, want true")
	}
	if status.Version != "3.4.2" {
		t.Fatalf("Version = %q, want 3.4.2", status.Version)
	}
}

func TestProbeTreatsUnavailableServerAsNotRunning(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}

	status, err := Probe(context.Background(), "http://couchdb.local", client)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if status.Running {
		t.Fatal("Running = true, want false")
	}
}
