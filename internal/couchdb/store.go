package couchdb

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultURL      = "http://127.0.0.1:5984"
	defaultAdmin    = "admin"
	envFileName     = "couchdb.env"
	envAdminAuthKey = "COUCHDB_ADMIN_AUTH"
)

type Credentials struct {
	URL             string
	AdminUser       string
	AdminAuth       string
	AcceptedVersion string
	AcceptedAt      time.Time
}

type Store struct {
	Path string
}

func DefaultStore() (Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Store{}, fmt.Errorf("unable to determine home directory: %w", err)
	}
	return Store{Path: filepath.Join(home, ".couchfusion", envFileName)}, nil
}

func (s Store) Load() (Credentials, bool, error) {
	if strings.TrimSpace(s.Path) == "" {
		return Credentials{}, false, errors.New("couchdb credential store path is empty")
	}

	f, err := os.Open(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, false, nil
		}
		return Credentials{}, false, fmt.Errorf("failed to open %s: %w", s.Path, err)
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return Credentials{}, false, fmt.Errorf("failed to read %s: %w", s.Path, err)
	}

	creds := Credentials{
		URL:             values["COUCHDB_URL"],
		AdminUser:       values["COUCHDB_ADMIN_USER"],
		AdminAuth:       values[envAdminAuthKey],
		AcceptedVersion: values["COUCHDB_ACCEPTED_VERSION"],
	}
	if creds.URL == "" {
		creds.URL = DefaultURL
	}
	if creds.AdminUser == "" {
		creds.AdminUser = defaultAdmin
	}
	if raw := values["COUCHDB_ACCEPTED_AT"]; raw != "" {
		acceptedAt, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Credentials{}, false, fmt.Errorf("invalid COUCHDB_ACCEPTED_AT in %s: %w", s.Path, err)
		}
		creds.AcceptedAt = acceptedAt
	}

	return creds, true, nil
}

func (s Store) Save(creds Credentials) error {
	if strings.TrimSpace(s.Path) == "" {
		return errors.New("couchdb credential store path is empty")
	}
	if creds.URL == "" {
		creds.URL = DefaultURL
	}
	if creds.AdminUser == "" {
		creds.AdminUser = defaultAdmin
	}
	if creds.AcceptedAt.IsZero() {
		creds.AcceptedAt = time.Now().UTC()
	}

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(s.Path), err)
	}

	content := strings.Join([]string{
		"COUCHDB_URL=" + creds.URL,
		"COUCHDB_ADMIN_USER=" + creds.AdminUser,
		envAdminAuthKey + "=" + creds.AdminAuth,
		"COUCHDB_ACCEPTED_VERSION=" + creds.AcceptedVersion,
		"COUCHDB_ACCEPTED_AT=" + creds.AcceptedAt.UTC().Format(time.RFC3339),
		"",
	}, "\n")

	if err := os.WriteFile(s.Path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", s.Path, err)
	}
	return os.Chmod(s.Path, 0o600)
}

func LoadAdminAuth(store Store) (string, bool, error) {
	if value := strings.TrimSpace(os.Getenv(envAdminAuthKey)); value != "" {
		return value, true, nil
	}

	creds, ok, err := store.Load()
	if err != nil || !ok || strings.TrimSpace(creds.AdminAuth) == "" {
		return "", false, err
	}
	return creds.AdminAuth, true, nil
}

func DecodeAdminAuth(auth string) (string, string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth))
	if err != nil {
		return "", "", fmt.Errorf("failed to decode COUCHDB_ADMIN_AUTH: %w", err)
	}
	username, password, ok := strings.Cut(string(raw), ":")
	if !ok || username == "" || password == "" {
		return "", "", errors.New("COUCHDB_ADMIN_AUTH must decode to username:password")
	}
	return username, password, nil
}

func EncodeAdminAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
