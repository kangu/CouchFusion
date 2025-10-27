package gitutil

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// Clone clones the provided repository into targetDir.
func Clone(ctx context.Context, repoURL, branch, targetDir string, protocol string, authPrompt bool) error {
	cloneURL := repoURL

	if protocol == "https" && authPrompt {
		var err error
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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
