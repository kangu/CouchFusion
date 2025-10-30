package gitutil

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

type cloneConfig struct {
	stdout io.Writer
	stderr io.Writer
	logf   func(string, ...any)
}

// CloneOption customizes git clone execution.
type CloneOption func(*cloneConfig)

// WithOutput directs clone stdout and stderr to the given writer.
func WithOutput(w io.Writer) CloneOption {
	return func(cfg *cloneConfig) {
		cfg.stdout = w
		cfg.stderr = w
	}
}

// WithStdout directs clone stdout to the given writer.
func WithStdout(w io.Writer) CloneOption {
	return func(cfg *cloneConfig) {
		cfg.stdout = w
	}
}

// WithStderr directs clone stderr to the given writer.
func WithStderr(w io.Writer) CloneOption {
	return func(cfg *cloneConfig) {
		cfg.stderr = w
	}
}

// WithLogger overrides the logging function used for clone status messages.
func WithLogger(logf func(string, ...any)) CloneOption {
	return func(cfg *cloneConfig) {
		cfg.logf = logf
	}
}

func buildCloneConfig(opts []CloneOption) cloneConfig {
	cfg := cloneConfig{
		stdout: os.Stdout,
		stderr: os.Stderr,
		logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.stdout == nil {
		cfg.stdout = io.Discard
	}
	if cfg.stderr == nil {
		cfg.stderr = io.Discard
	}
	if cfg.logf == nil {
		cfg.logf = func(string, ...any) {}
	}
	return cfg
}

// Clone clones the provided repository into targetDir.
func Clone(ctx context.Context, repoURL, branch, targetDir string, protocol string, authPrompt bool, opts ...CloneOption) error {
	cfg := buildCloneConfig(opts)

	cloneURL := repoURL

	cfg.logf("Preparing git clone: repo=%s branch=%s target=%s", repoURL, branch, targetDir)

	if protocol == "https" && authPrompt {
		var err error
		// Inject credentials using stdin; this still prompts in the terminal.
		cloneURL, err = injectCredentials(repoURL)
		if err != nil {
			return err
		}
	}

	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, cloneURL, targetDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout = cfg.stdout
	cmd.Stderr = cfg.stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	return nil
}

func injectCredentials(repoURL string) (string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter HTTPS username: ")
	usernameRaw, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read username: %w", err)
	}
	username := strings.TrimSpace(usernameRaw)

	fmt.Print("Enter HTTPS password/token: ")
	bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	password := strings.TrimSpace(string(bytePassword))

	if username == "" || password == "" {
		return "", fmt.Errorf("credentials cannot be empty")
	}

	schemeSplit := strings.SplitN(repoURL, "//", 2)
	if len(schemeSplit) != 2 {
		return "", fmt.Errorf("invalid https repo url")
	}
	return fmt.Sprintf("%s//%s:%s@%s", schemeSplit[0], urlEncode(username), urlEncode(password), schemeSplit[1]), nil
}

func urlEncode(input string) string {
	replacer := strings.NewReplacer(
		"@", "%40",
		":", "%3A",
		"/", "%2F",
	)
	return replacer.Replace(input)
}
