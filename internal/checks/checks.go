package checks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Run executes prerequisite checks and returns warnings encountered.
func Run(ctx context.Context) []string {
	warnings := []string{}

	if err := checkBun(ctx); err != nil {
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
