package workspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/logging"
	"golang.org/x/term"
)

// applyLayerParameters executes post-clone configuration for selected modules.
func applyLayerParameters(ctx context.Context, targetDir string, modules []string) error {
	seen := map[string]struct{}{}
	for _, module := range modules {
		if _, handled := seen[module]; handled {
			continue
		}
		seen[module] = struct{}{}

		switch module {
		case "auth":
			if err := configureAuthLayer(ctx, targetDir); err != nil {
				return fmt.Errorf("auth layer configuration failed: %w", err)
			}
		}
	}
	return nil
}

func configureAuthLayer(ctx context.Context, targetDir string) error {
	username, err := prompt("Enter CouchDB admin username: ")
	if err != nil {
		return err
	}
	if username == "" {
		return errors.New("couchdb admin username cannot be empty")
	}

	password, err := promptSecret("Enter CouchDB admin password: ")
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("couchdb admin password cannot be empty")
	}

	authHeader := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	secret, err := fetchCouchDBCookieSecret(ctx, username, password)
	if err != nil {
		return err
	}

	envPath := filepath.Join(targetDir, ".env")
	values := map[string]string{
		"COUCHDB_ADMIN_AUTH":    authHeader,
		"COUCHDB_COOKIE_SECRET": secret,
	}

	if err := ensureEnvEntries(envPath, values); err != nil {
		return err
	}

	if err := ensureCouchDBAdminUser(ctx, username, password); err != nil {
		return err
	}

	logging.Infof("Updated %s with COUCHDB_ADMIN_AUTH and COUCHDB_COOKIE_SECRET.", envPath)
	return nil
}

func fetchCouchDBCookieSecret(ctx context.Context, username, password string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:5984/_node/_local/_config/chttpd_auth/secret", nil)
	if err != nil {
		return "", fmt.Errorf("failed to build couchdb config request: %w", err)
	}
	req.SetBasicAuth(username, password)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch couchdb cookie secret: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read couchdb response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("couchdb returned status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	secret := strings.TrimSpace(string(body))
	secret = strings.Trim(secret, "\"")
	if secret == "" {
		return "", errors.New("couchdb cookie secret response was empty")
	}
	return secret, nil
}

func ensureEnvEntries(path string, values map[string]string) error {
	existing := []string{}
	if data, err := os.ReadFile(path); err == nil {
		existing = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	updatedKeys := map[string]bool{}
	result := make([]string, 0, len(existing)+len(values))

	for _, line := range existing {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			result = append(result, line)
			continue
		}

		keyVal := strings.SplitN(line, "=", 2)
		if len(keyVal) != 2 {
			result = append(result, line)
			continue
		}

		key := keyVal[0]
		if val, ok := values[key]; ok {
			result = append(result, fmt.Sprintf("%s=%s", key, val))
			updatedKeys[key] = true
		} else {
			result = append(result, line)
		}
	}

	for key, val := range values {
		if updatedKeys[key] {
			continue
		}
		result = append(result, fmt.Sprintf("%s=%s", key, val))
	}

	content := strings.Join(result, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

func promptSecret(message string) (string, error) {
	fmt.Print(message)
	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes)), nil
}

func ensureCouchDBAdminUser(ctx context.Context, username, password string) error {
	userID := fmt.Sprintf("org.couchdb.user:%s", username)
	url := fmt.Sprintf("http://localhost:5984/_users/%s", userID)

	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to build user lookup request: %w", err)
	}
	req.SetBasicAuth(username, password)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to query couchdb user document: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		logging.Infof("CouchDB admin user '%s' already exists.", username)
		return nil
	case http.StatusNotFound:
		// proceed to creation
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected response checking user '%s': %s %s", username, resp.Status, strings.TrimSpace(string(body)))
	}

	payload := map[string]any{
		"_id":      userID,
		"name":     username,
		"roles":    []string{"admin"},
		"type":     "user",
		"password": password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal couchdb user payload: %w", err)
	}

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build user creation request: %w", err)
	}
	putReq.Header.Set("Content-Type", "application/json")
	putReq.SetBasicAuth(username, password)

	putResp, err := client.Do(putReq)
	if err != nil {
		return fmt.Errorf("failed to create couchdb user: %w", err)
	}
	defer putResp.Body.Close()

	respBody, err := io.ReadAll(putResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read couchdb user creation response: %w", err)
	}

	if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
		return fmt.Errorf("couchdb user creation failed (%s): %s", putResp.Status, strings.TrimSpace(string(respBody)))
	}

	logging.Infof("Created CouchDB admin user '%s'.", username)
	return nil
}
