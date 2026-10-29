package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	basecouchdb "github.com/nuxt-apps/couchfusion/internal/couchdb"
)

func TestCouchDBRepositoryEnsuresDedicatedAuthenticatedDatabase(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.Method != http.MethodPut || request.URL.Path != "/couchfusion-control" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		username, password, ok := request.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			t.Fatalf("basic auth = %q/%q, %v", username, password, ok)
		}
		writer.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer server.Close()

	repository := newRepositoryForTest(t, server.URL, server.Client())
	if err := repository.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("database calls = %d, want 1", calls)
	}
}

func TestCouchDBRepositoryRecoversSiteCredentialsAndPersistsPrimaryStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "admin" || password != "site-secret" {
			t.Fatalf("basic auth = %q/%q, %v", username, password, ok)
		}
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	directory := t.TempDir()
	primary := basecouchdb.Store{Path: filepath.Join(directory, "global", "couchdb.env")}
	site := basecouchdb.Store{Path: filepath.Join(directory, "workspace", "apps", "shop", ".env")}
	if err := site.Save(basecouchdb.Credentials{
		URL: server.URL, AdminUser: "admin", AdminAuth: basecouchdb.EncodeAdminAuth("admin", "site-secret"),
	}); err != nil {
		t.Fatal(err)
	}

	repository := NewCouchDBRepository(CouchDBOptions{
		Credentials: primary, CredentialFallbacks: []basecouchdb.Store{site}, Client: server.Client(),
	})
	if err := repository.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, ok, err := primary.Load()
	if err != nil || !ok {
		t.Fatalf("primary.Load() = %#v, %v, %v", recovered, ok, err)
	}
	if recovered.URL != server.URL || recovered.AdminAuth != basecouchdb.EncodeAdminAuth("admin", "site-secret") {
		t.Fatalf("recovered credentials = %#v", recovered)
	}
}

func TestCouchDBRepositoryUsesProcessAdminAuthWithStoredURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "admin" || password != "process-secret" {
			t.Fatalf("basic auth = %q/%q, %v", username, password, ok)
		}
		writer.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer server.Close()

	primary := basecouchdb.Store{Path: filepath.Join(t.TempDir(), "couchdb.env")}
	if err := primary.Save(basecouchdb.Credentials{URL: server.URL, AdminUser: "admin"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COUCHDB_ADMIN_AUTH", basecouchdb.EncodeAdminAuth("admin", "process-secret"))

	repository := NewCouchDBRepository(CouchDBOptions{Credentials: primary, Client: server.Client()})
	if err := repository.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCouchDBRepositoryCreatesAndUpdatesRevisionedDocuments(t *testing.T) {
	var mu sync.Mutex
	var stored Document
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/couchfusion-control" {
			writer.WriteHeader(http.StatusCreated)
			return
		}
		if request.Method != http.MethodPut || request.URL.Path != "/couchfusion-control/agent-conversation:c1" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var document Document
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		stored = document
		mu.Unlock()
		revision := "1-created"
		if document.Revision != "" {
			revision = "2-updated"
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "id": document.ID, "rev": revision})
	}))
	defer server.Close()
	repository := newRepositoryForTest(t, server.URL, server.Client())

	created, err := repository.Create(context.Background(), Document{
		ConversationID: "c1", CodexThreadID: "thread-1", WorkspaceID: "w1", SiteID: "shop",
		SiteRootFingerprint: "fingerprint", Title: "Hero", Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "agent-conversation:c1" || created.Revision != "1-created" {
		t.Fatalf("created = %#v", created)
	}
	created.Status = StatusReady
	updated, err := repository.Update(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != "2-updated" {
		t.Fatalf("updated revision = %q", updated.Revision)
	}
	mu.Lock()
	defer mu.Unlock()
	if stored.Type != DocumentType || stored.SchemaVersion != CurrentSchemaVersion || stored.Revision != "1-created" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestCouchDBRepositoryMapsConflicts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/couchfusion-control" {
			writer.WriteHeader(http.StatusCreated)
			return
		}
		writer.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	repository := newRepositoryForTest(t, server.URL, server.Client())
	document := validDocument("c1", "w1", "shop")
	document.ID = "agent-conversation:c1"
	document.Revision = "1-old"
	if _, err := repository.Update(context.Background(), document); !errors.Is(err, ErrConflict) {
		t.Fatalf("Update() error = %v, want ErrConflict", err)
	}
}

func TestCouchDBRepositoryListsOnlySiteDocumentsAndMarksNewerSchemasReadOnly(t *testing.T) {
	matching := validDocument("c1", "w1", "shop")
	matching.ID = "agent-conversation:c1"
	newer := validDocument("c2", "w1", "shop")
	newer.ID = "agent-conversation:c2"
	newer.SchemaVersion = CurrentSchemaVersion + 1
	other := validDocument("c3", "w1", "other")
	other.ID = "agent-conversation:c3"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/couchfusion-control" {
			writer.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if request.URL.Path != "/couchfusion-control/_all_docs" || request.URL.Query().Get("include_docs") != "true" {
			t.Fatalf("request = %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"rows": []any{
			map[string]any{"doc": matching}, map[string]any{"doc": newer}, map[string]any{"doc": other},
			map[string]any{"doc": map[string]any{"_id": "other:1", "type": "other"}},
		}})
	}))
	defer server.Close()
	repository := newRepositoryForTest(t, server.URL, server.Client())
	documents, err := repository.ListSite(context.Background(), "w1", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 2 || documents[0].ConversationID != "c1" || documents[1].ConversationID != "c2" || !documents[1].ReadOnly {
		t.Fatalf("documents = %#v", documents)
	}
}

func TestConversationDocumentPersistsOnlyApprovedMetadata(t *testing.T) {
	document := validDocument("c1", "w1", "shop")
	document.Baseline = []BaselineEntry{{Path: "app/components/content/Hero.vue", Status: "tracked", SHA256: "abc"}}
	document.ChangedFiles = []ChangedFile{{Path: "app/components/content/Hero.vue", Kind: "modified", BaselineDirty: true}}
	document.LastTurn = &LastTurn{Status: "completed", Summary: "bounded summary"}
	document.Registry = &RegistryResult{State: "failed", Problem: "Registry rebuild failed"}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{`"prompt"`, `"transcript"`, `"diff"`, `"output"`, `"requestid"`, `"approvaltoken"`, `"password"`, `"authorization"`} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("document contains forbidden key %s: %s", forbidden, encoded)
		}
	}
}

func TestCouchDBRepositoryMarksRecoveredActiveDocumentsInterrupted(t *testing.T) {
	active := validDocument("c1", "w1", "shop")
	active.ID, active.Revision, active.Status = "agent-conversation:c1", "1-a", StatusActive
	ready := validDocument("c2", "w1", "shop")
	ready.ID, ready.Revision, ready.Status = "agent-conversation:c2", "1-b", StatusReady
	var updated []Document
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/couchfusion-control":
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodGet:
			_ = json.NewEncoder(writer).Encode(map[string]any{"rows": []any{map[string]any{"doc": active}, map[string]any{"doc": ready}}})
		default:
			var document Document
			_ = json.NewDecoder(request.Body).Decode(&document)
			updated = append(updated, document)
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "id": document.ID, "rev": "2-interrupted"})
		}
	}))
	defer server.Close()
	repository := newRepositoryForTest(t, server.URL, server.Client())
	if err := repository.MarkActiveInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ConversationID != "c1" || updated[0].Status != StatusInterrupted {
		t.Fatalf("updated = %#v", updated)
	}
}

func newRepositoryForTest(t *testing.T, url string, client *http.Client) *CouchDBRepository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "couchdb.env")
	credentialStore := basecouchdb.Store{Path: path}
	if err := credentialStore.Save(basecouchdb.Credentials{
		URL: url, AdminUser: "admin", AdminAuth: basecouchdb.EncodeAdminAuth("admin", "secret"), AcceptedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return NewCouchDBRepository(CouchDBOptions{Credentials: credentialStore, Client: client})
}

func validDocument(id, workspaceID, siteID string) Document {
	return Document{
		Type: DocumentType, SchemaVersion: CurrentSchemaVersion, ConversationID: id, CodexThreadID: "thread-" + id,
		WorkspaceID: workspaceID, SiteID: siteID, SiteRootFingerprint: "fingerprint", Title: "Component " + id,
		Status: StatusReady, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}
