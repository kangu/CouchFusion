package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"time"
)

// Run executes prerequisite checks and returns warnings encountered.
func Run(ctx context.Context) []string {
	warnings := []string{}

	if err := checkBun(ctx); err != nil {
		warnings = append(warnings, err.Error())
	}

	if err := checkCouchDB(ctx); err != nil {
		warnings = append(warnings, err.Error())
	}

	return warnings
}

func checkBun(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "bun", "--version")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("bun check failed: %v", exitErr)
		}
		return fmt.Errorf("bun is not available in PATH")
	}
	return nil
}

func checkCouchDB(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:5984/_up", nil)
	if err != nil {
		return fmt.Errorf("couchdb check failed to build request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("unable to reach CouchDB at http://localhost:5984: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("CouchDB responded with status %s", resp.Status)
}
