package couchdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Status struct {
	Running bool
	URL     string
	Version string
}

type Prober interface {
	Probe(context.Context) (Status, error)
}

type HTTPProber struct {
	BaseURL string
	Client  *http.Client
}

func (p HTTPProber) Probe(ctx context.Context) (Status, error) {
	return Probe(ctx, p.BaseURL, p.Client)
}

func Probe(ctx context.Context, baseURL string, client *http.Client) (Status, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	upReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/_up", nil)
	if err != nil {
		return Status{}, fmt.Errorf("failed to build CouchDB _up request: %w", err)
	}
	upResp, err := client.Do(upReq)
	if err != nil {
		return Status{URL: baseURL, Running: false}, nil
	}
	defer upResp.Body.Close()
	_, _ = io.Copy(io.Discard, upResp.Body)
	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		return Status{URL: baseURL, Running: false}, nil
	}

	rootReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
	if err != nil {
		return Status{}, fmt.Errorf("failed to build CouchDB version request: %w", err)
	}
	rootResp, err := client.Do(rootReq)
	if err != nil {
		return Status{URL: baseURL, Running: true}, nil
	}
	defer rootResp.Body.Close()

	body, err := io.ReadAll(rootResp.Body)
	if err != nil {
		return Status{}, fmt.Errorf("failed to read CouchDB version response: %w", err)
	}
	if rootResp.StatusCode < 200 || rootResp.StatusCode >= 300 {
		return Status{URL: baseURL, Running: true}, nil
	}

	var payload struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(body, &payload)

	return Status{URL: baseURL, Running: true, Version: payload.Version}, nil
}
