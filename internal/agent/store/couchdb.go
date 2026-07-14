package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	basecouchdb "github.com/nuxt-apps/couchfusion/internal/couchdb"
)

const (
	DatabaseName         = "couchfusion-control"
	DocumentType         = "agent-conversation"
	CurrentSchemaVersion = 1
	defaultMaxResponse   = int64(2 << 20)
)

var (
	ErrNotFound    = errors.New("agent conversation was not found")
	ErrConflict    = errors.New("agent conversation revision conflicts with stored state")
	ErrInvalid     = errors.New("agent conversation document is invalid")
	ErrNewerSchema = errors.New("agent conversation uses a newer schema")
)

type Status string

const (
	StatusReady       Status = "ready"
	StatusActive      Status = "active"
	StatusWaiting     Status = "waiting"
	StatusInterrupted Status = "interrupted"
)

type BaselineEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	SHA256 string `json:"sha256"`
}

type ChangedFile struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	BaselineDirty bool   `json:"baselineDirty,omitempty"`
}

type LastTurn struct {
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type Document struct {
	ID                   string          `json:"_id"`
	Revision             string          `json:"_rev,omitempty"`
	Type                 string          `json:"type"`
	SchemaVersion        int             `json:"schemaVersion"`
	ConversationID       string          `json:"conversationId"`
	CodexThreadID        string          `json:"codexThreadId"`
	WorkspaceID          string          `json:"workspaceId"`
	SiteID               string          `json:"siteId"`
	SiteRootFingerprint  string          `json:"siteRootFingerprint"`
	Title                string          `json:"title"`
	Status               Status          `json:"status"`
	GeneratedComponentID string          `json:"generatedComponentId,omitempty"`
	Baseline             []BaselineEntry `json:"baseline,omitempty"`
	ChangedFiles         []ChangedFile   `json:"changedFiles,omitempty"`
	LastTurn             *LastTurn       `json:"lastTurn,omitempty"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
	ReadOnly             bool            `json:"-"`
}

type Repository interface {
	Ensure(context.Context) error
	Create(context.Context, Document) (Document, error)
	Get(context.Context, string) (Document, error)
	ListSite(context.Context, string, string) ([]Document, error)
	Update(context.Context, Document) (Document, error)
	MarkActiveInterrupted(context.Context) error
}

type CouchDBOptions struct {
	Credentials      basecouchdb.Store
	Client           *http.Client
	MaxResponseBytes int64
}

type CouchDBRepository struct {
	mu               sync.Mutex
	credentials      basecouchdb.Store
	client           *http.Client
	maxResponseBytes int64
	ensured          bool
	baseURL          string
	username         string
	password         string
}

func NewCouchDBRepository(options CouchDBOptions) *CouchDBRepository {
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	maxResponse := options.MaxResponseBytes
	if maxResponse <= 0 {
		maxResponse = defaultMaxResponse
	}
	return &CouchDBRepository{credentials: options.Credentials, client: client, maxResponseBytes: maxResponse}
}

func (r *CouchDBRepository) Ensure(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ensured {
		return nil
	}
	credentials, ok, err := r.credentials.Load()
	if err != nil {
		return fmt.Errorf("load CouchDB credentials: %w", err)
	}
	if !ok || strings.TrimSpace(credentials.AdminAuth) == "" {
		return errors.New("CouchDB administrative credentials are unavailable")
	}
	username, password, err := basecouchdb.DecodeAdminAuth(credentials.AdminAuth)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(strings.TrimRight(credentials.URL, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("CouchDB URL is invalid")
	}
	r.baseURL, r.username, r.password = parsed.String(), username, password
	status, _, err := r.do(ctx, http.MethodPut, "/"+DatabaseName, nil)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusAccepted && status != http.StatusPreconditionFailed {
		return fmt.Errorf("create CouchDB control database: unexpected status %d", status)
	}
	r.ensured = true
	return nil
}

func (r *CouchDBRepository) Create(ctx context.Context, document Document) (Document, error) {
	if document.Type == "" {
		document.Type = DocumentType
	}
	if document.SchemaVersion == 0 {
		document.SchemaVersion = CurrentSchemaVersion
	}
	if document.ID == "" {
		document.ID = documentID(document.ConversationID)
	}
	now := time.Now().UTC()
	if document.CreatedAt.IsZero() {
		document.CreatedAt = now
	}
	document.UpdatedAt = now
	document.Revision = ""
	if err := validateCurrent(document); err != nil {
		return Document{}, err
	}
	return r.put(ctx, document)
}

func (r *CouchDBRepository) Update(ctx context.Context, document Document) (Document, error) {
	if document.Revision == "" {
		return Document{}, fmt.Errorf("%w: revision is required", ErrInvalid)
	}
	if err := validateCurrent(document); err != nil {
		return Document{}, err
	}
	document.UpdatedAt = time.Now().UTC()
	return r.put(ctx, document)
}

func (r *CouchDBRepository) Get(ctx context.Context, conversationID string) (Document, error) {
	if err := r.Ensure(ctx); err != nil {
		return Document{}, err
	}
	status, body, err := r.do(ctx, http.MethodGet, "/"+DatabaseName+"/"+url.PathEscape(documentID(conversationID)), nil)
	if err != nil {
		return Document{}, err
	}
	if status == http.StatusNotFound {
		return Document{}, ErrNotFound
	}
	if status != http.StatusOK {
		return Document{}, fmt.Errorf("read agent conversation: unexpected status %d", status)
	}
	var document Document
	if err := json.Unmarshal(body, &document); err != nil {
		return Document{}, fmt.Errorf("decode agent conversation: %w", err)
	}
	if document.SchemaVersion > CurrentSchemaVersion {
		document.ReadOnly = true
		return document, ErrNewerSchema
	}
	if err := validateCurrent(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (r *CouchDBRepository) ListSite(ctx context.Context, workspaceID, siteID string) ([]Document, error) {
	documents, err := r.listAll(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Document, 0, len(documents))
	for _, document := range documents {
		if document.WorkspaceID == workspaceID && document.SiteID == siteID {
			result = append(result, document)
		}
	}
	return result, nil
}

func (r *CouchDBRepository) MarkActiveInterrupted(ctx context.Context) error {
	documents, err := r.listAll(ctx)
	if err != nil {
		return err
	}
	for _, document := range documents {
		if document.ReadOnly || !recoverableActiveStatus(document.Status) {
			continue
		}
		document.Status = StatusInterrupted
		if _, err := r.Update(ctx, document); err != nil {
			return err
		}
	}
	return nil
}

func (r *CouchDBRepository) put(ctx context.Context, document Document) (Document, error) {
	if err := r.Ensure(ctx); err != nil {
		return Document{}, err
	}
	payload, err := json.Marshal(document)
	if err != nil {
		return Document{}, err
	}
	status, body, err := r.do(ctx, http.MethodPut, "/"+DatabaseName+"/"+url.PathEscape(document.ID), bytes.NewReader(payload))
	if err != nil {
		return Document{}, err
	}
	if status == http.StatusConflict {
		return Document{}, ErrConflict
	}
	if status != http.StatusCreated && status != http.StatusAccepted && status != http.StatusOK {
		return Document{}, fmt.Errorf("write agent conversation: unexpected status %d", status)
	}
	var response struct {
		Revision string `json:"rev"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Document{}, fmt.Errorf("decode CouchDB write response: %w", err)
	}
	document.Revision = response.Revision
	return document, nil
}

func (r *CouchDBRepository) listAll(ctx context.Context) ([]Document, error) {
	if err := r.Ensure(ctx); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("include_docs", "true")
	query.Set("startkey", `"agent-conversation:"`)
	query.Set("endkey", `"agent-conversation:\ufff0"`)
	status, body, err := r.do(ctx, http.MethodGet, "/"+DatabaseName+"/_all_docs?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("list agent conversations: unexpected status %d", status)
	}
	var response struct {
		Rows []struct {
			Document json.RawMessage `json:"doc"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode agent conversation list: %w", err)
	}
	documents := make([]Document, 0, len(response.Rows))
	for _, row := range response.Rows {
		var document Document
		if json.Unmarshal(row.Document, &document) != nil || document.Type != DocumentType || document.ConversationID == "" {
			continue
		}
		if document.SchemaVersion > CurrentSchemaVersion {
			document.ReadOnly = true
			documents = append(documents, document)
			continue
		}
		if validateCurrent(document) == nil {
			documents = append(documents, document)
		}
	}
	return documents, nil
}

func (r *CouchDBRepository) do(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	request.SetBasicAuth(r.username, r.password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := r.client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("CouchDB request failed: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, r.maxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return 0, nil, err
	}
	if int64(len(payload)) > r.maxResponseBytes {
		return 0, nil, errors.New("CouchDB response is too large")
	}
	return response.StatusCode, payload, nil
}

func validateCurrent(document Document) error {
	if document.Type != DocumentType || document.SchemaVersion != CurrentSchemaVersion || document.ConversationID == "" ||
		document.ID != documentID(document.ConversationID) || document.CodexThreadID == "" || document.WorkspaceID == "" ||
		document.SiteID == "" || document.SiteRootFingerprint == "" || document.Title == "" || document.Status == "" {
		return ErrInvalid
	}
	return nil
}

func documentID(conversationID string) string { return "agent-conversation:" + conversationID }

func recoverableActiveStatus(status Status) bool {
	switch status {
	case StatusActive, StatusWaiting, "starting", "interrupting", "rebuilding_registry":
		return true
	default:
		return false
	}
}
