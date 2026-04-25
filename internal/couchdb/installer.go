package couchdb

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) error
	Output(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

type SystemInstaller struct {
	Runner CommandRunner
	OS     string
}

func (i SystemInstaller) Install(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	runner := i.Runner
	if runner == nil {
		runner = execRunner{}
	}
	goos := i.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	adminUser := opts.AdminUser
	if adminUser == "" {
		adminUser = defaultAdmin
	}
	password, err := generatePassword()
	if err != nil {
		return InstallResult{}, err
	}
	auth := EncodeAdminAuth(adminUser, password)

	switch goos {
	case "darwin":
		if err := installDarwin(ctx, runner, adminUser, password); err != nil {
			return InstallResult{}, err
		}
	case "linux":
		if err := installLinux(ctx, runner, adminUser, password); err != nil {
			return InstallResult{}, err
		}
	default:
		return InstallResult{}, fmt.Errorf("managed CouchDB installation is supported on macOS and Debian/Ubuntu only (detected %s)", goos)
	}

	status, err := waitForCouchDB(ctx, DefaultURL)
	if err != nil {
		return InstallResult{}, err
	}
	if err := ensureSystemDatabases(ctx, DefaultURL, adminUser, password); err != nil {
		return InstallResult{}, err
	}
	if err := installPhoton(ctx, DefaultURL, adminUser, password); err != nil {
		return InstallResult{}, err
	}

	return InstallResult{
		URL:       DefaultURL,
		AdminUser: adminUser,
		AdminAuth: auth,
		Version:   status.Version,
	}, nil
}

func installDarwin(ctx context.Context, runner CommandRunner, adminUser, password string) error {
	if err := runner.Run(ctx, "bash", "-lc", buildDarwinInstallScript()); err != nil {
		return fmt.Errorf("failed to install CouchDB with Homebrew: %w", err)
	}

	if err := runner.Run(ctx, "bash", "-lc", buildDarwinConfigScript(adminUser, password)); err != nil {
		return fmt.Errorf("failed to configure CouchDB on macOS: %w", err)
	}
	return nil
}

func buildDarwinInstallScript() string {
	return `
set -euo pipefail
BREW_BIN="$(command -v brew || true)"
if [ -z "$BREW_BIN" ]; then
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  if [ -x /opt/homebrew/bin/brew ]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
  elif [ -x /usr/local/bin/brew ]; then
    eval "$(/usr/local/bin/brew shellenv)"
  fi
else
  if [ -x /opt/homebrew/bin/brew ]; then
    eval "$(/opt/homebrew/bin/brew shellenv)"
  elif [ -x /usr/local/bin/brew ]; then
    eval "$(/usr/local/bin/brew shellenv)"
  fi
fi
brew update
brew install couchdb
BREW_PREFIX="$(brew --prefix)"
CONF_FILE="$BREW_PREFIX/etc/local.ini"
LOCAL_D="$BREW_PREFIX/etc/local.d"
mkdir -p "$LOCAL_D"
touch "$CONF_FILE"
`
}

func buildDarwinConfigScript(adminUser, password string) string {
	return fmt.Sprintf(`
set -euo pipefail
if [ -x /opt/homebrew/bin/brew ]; then
  eval "$(/opt/homebrew/bin/brew shellenv)"
elif [ -x /usr/local/bin/brew ]; then
  eval "$(/usr/local/bin/brew shellenv)"
fi
BREW_PREFIX="$(brew --prefix)"
CONF_FILE="$BREW_PREFIX/etc/local.ini"
LOCAL_D="$BREW_PREFIX/etc/local.d"
mkdir -p "$LOCAL_D"
touch "$CONF_FILE"
if ! grep -q '^\[admins\]' "$CONF_FILE"; then printf "\n[admins]\n" >> "$CONF_FILE"; fi
if grep -q '^%[1]s *= *' "$CONF_FILE"; then
  sed -i '' 's|^%[1]s *= *.*|%[1]s = %[2]s|g' "$CONF_FILE"
else
  printf "%%s = %%s\n" "%[1]s" "%[2]s" >> "$CONF_FILE"
fi
if ! grep -q '^\[couchdb\]' "$CONF_FILE"; then printf "\n[couchdb]\n" >> "$CONF_FILE"; fi
if grep -q '^single_node *= *' "$CONF_FILE"; then
  sed -i '' 's|^single_node *= *.*|single_node = true|g' "$CONF_FILE"
else
  printf "single_node = true\n" >> "$CONF_FILE"
fi
cat > "$LOCAL_D/10-single-node.ini" <<'EOF'
[chttpd]
bind_address = 127.0.0.1
port = 5984

[cluster]
n = 1
EOF
chmod 0644 "$CONF_FILE" "$LOCAL_D/10-single-node.ini"
brew services stop couchdb >/dev/null 2>&1 || true
brew services start couchdb
`, shellEscape(adminUser), shellEscape(password))
}

func installLinux(ctx context.Context, runner CommandRunner, adminUser, password string) error {
	cookie, err := randomHex(16)
	if err != nil {
		return err
	}

	if err := runner.Run(ctx, "bash", "-lc", buildLinuxInstallScript(adminUser, password, cookie)); err != nil {
		return fmt.Errorf("failed to install CouchDB on Debian/Ubuntu: %w", err)
	}
	return nil
}

func buildLinuxInstallScript(adminUser, password, cookie string) string {
	return fmt.Sprintf(`
set -euo pipefail
. /etc/os-release
if [ -z "${VERSION_CODENAME:-}" ]; then
  echo "Unsupported Linux distribution: missing VERSION_CODENAME" >&2
  exit 1
fi
sudo apt update -y
sudo apt install -y curl apt-transport-https gnupg debconf-utils openssl
curl -fsSL https://couchdb.apache.org/repo/keys.asc | gpg --dearmor | sudo tee /usr/share/keyrings/couchdb-archive-keyring.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/couchdb-archive-keyring.gpg] https://apache.jfrog.io/artifactory/couchdb-deb/ ${VERSION_CODENAME} main" | sudo tee /etc/apt/sources.list.d/couchdb.list >/dev/null
sudo apt update -y
echo "couchdb couchdb/mode select standalone" | sudo debconf-set-selections
echo "couchdb couchdb/bindaddress string 127.0.0.1" | sudo debconf-set-selections
echo "couchdb couchdb/cookie string %[3]s" | sudo debconf-set-selections
echo "couchdb couchdb/adminpass password %[2]s" | sudo debconf-set-selections
echo "couchdb couchdb/adminpass_again password %[2]s" | sudo debconf-set-selections
sudo DEBIAN_FRONTEND=noninteractive apt install -y couchdb
sudo systemctl restart couchdb
`, shellEscape(adminUser), shellEscape(password), shellEscape(cookie))
}

func waitForCouchDB(ctx context.Context, baseURL string) (Status, error) {
	deadline := time.Now().Add(45 * time.Second)
	var last Status
	for time.Now().Before(deadline) {
		status, err := Probe(ctx, baseURL, nil)
		if err != nil {
			return Status{}, err
		}
		last = status
		if status.Running {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return last, errors.New("CouchDB did not become reachable at http://127.0.0.1:5984")
}

func ensureSystemDatabases(ctx context.Context, baseURL, username, password string) error {
	for _, db := range []string{"_users", "_replicator", "_global_changes"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(baseURL, "/")+"/"+db, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(username, password)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to ensure CouchDB database %s: %w", db, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == http.StatusPreconditionFailed {
			continue
		}
		return fmt.Errorf("failed to ensure CouchDB database %s: HTTP %s", db, resp.Status)
	}
	return nil
}

func installPhoton(ctx context.Context, baseURL, username, password string) error {
	client := http.DefaultClient
	couch := strings.TrimRight(baseURL, "/")
	if err := putJSON(ctx, client, couch+"/photon", username, password, nil); err != nil && !strings.Contains(err.Error(), "412") {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://raw.githubusercontent.com/ermouth/couch-photon/master/photon.json", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download Photon design document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("failed to download Photon design document: HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := putJSON(ctx, client, couch+"/photon/_design/photon", username, password, body); err != nil {
		return err
	}
	if err := putJSON(ctx, client, couch+"/photon/_security", username, password, []byte(`{}`)); err != nil {
		return err
	}
	if err := putJSON(ctx, client, couch+"/_node/_local/_config/csp/attachments_enable", username, password, []byte(`"false"`)); err != nil {
		return err
	}
	if err := putJSON(ctx, client, couch+"/_node/_local/_config/chttpd_auth/same_site", username, password, []byte(`"lax"`)); err != nil {
		return err
	}
	return nil
}

func putJSON(ctx context.Context, client *http.Client, url, username, password string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, password)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to PUT %s: %w", url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("failed to PUT %s: HTTP %s %s", url, resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func generatePassword() (string, error) {
	return randomHex(24)
}

func randomHex(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func shellEscape(value string) string {
	return strings.ReplaceAll(value, `'`, `'\''`)
}
