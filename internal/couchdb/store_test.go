package couchdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTripUsesEnvFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "couchdb.env")
	store := Store{Path: path}
	acceptedAt := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)

	err := store.Save(Credentials{
		URL:             "http://127.0.0.1:5984",
		AdminUser:       "admin",
		AdminAuth:       "YWRtaW46c2VjcmV0",
		AcceptedVersion: "3.3.3",
		AcceptedAt:      acceptedAt,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("env file mode = %o, want 0600", got)
	}

	creds, ok, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if creds.AdminAuth != "YWRtaW46c2VjcmV0" {
		t.Fatalf("AdminAuth = %q", creds.AdminAuth)
	}
	if creds.AcceptedVersion != "3.3.3" {
		t.Fatalf("AcceptedVersion = %q", creds.AcceptedVersion)
	}
	if !creds.AcceptedAt.Equal(acceptedAt) {
		t.Fatalf("AcceptedAt = %s, want %s", creds.AcceptedAt, acceptedAt)
	}
}

func TestLoadAdminAuthPrefersProcessEnvironment(t *testing.T) {
	t.Setenv("COUCHDB_ADMIN_AUTH", "process-token")
	store := Store{Path: filepath.Join(t.TempDir(), "missing.env")}

	auth, ok, err := LoadAdminAuth(store)
	if err != nil {
		t.Fatalf("LoadAdminAuth() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadAdminAuth() ok = false, want true")
	}
	if auth != "process-token" {
		t.Fatalf("auth = %q, want process-token", auth)
	}
}

func TestDecodeAdminAuth(t *testing.T) {
	user, password, err := DecodeAdminAuth("YWRtaW46c2VjcmV0")
	if err != nil {
		t.Fatalf("DecodeAdminAuth() error = %v", err)
	}
	if user != "admin" || password != "secret" {
		t.Fatalf("decoded = %q/%q, want admin/secret", user, password)
	}
}

func TestStoreLoadIgnoresCommentsAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "couchdb.env")
	if err := os.WriteFile(path, []byte("\n# comment\nCOUCHDB_ADMIN_AUTH=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, ok, err := (Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if strings.TrimSpace(creds.AdminAuth) != "abc" {
		t.Fatalf("AdminAuth = %q", creds.AdminAuth)
	}
}
