package workspace

import "testing"

func TestStoredCouchDBCredentialsUseAdminAuthEnvironment(t *testing.T) {
	t.Setenv("COUCHDB_ADMIN_AUTH", "YWRtaW46c2VjcmV0")

	username, password, ok := storedCouchDBCredentials()
	if !ok {
		t.Fatal("storedCouchDBCredentials() ok = false, want true")
	}
	if username != "admin" || password != "secret" {
		t.Fatalf("credentials = %q/%q, want admin/secret", username, password)
	}
}
