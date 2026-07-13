package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrDuplicate        = errors.New("workspace is already registered")
	ErrInvalidWorkspace = errors.New("invalid CouchFusion workspace")
	ErrNotFound         = errors.New("workspace is not registered")
)

type Entry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Root         string    `json:"root"`
	CreatedAt    time.Time `json:"createdAt"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
	Available    bool      `json:"-"`
	Problem      string    `json:"-"`
}

type Store struct {
	Path string
}

type registryFile struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

type Service struct {
	Store Store
	Now   func() time.Time
	NewID func() (string, error)

	mu sync.Mutex
}

func DefaultStore() (Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Store{}, fmt.Errorf("determine home directory: %w", err)
	}
	return Store{Path: filepath.Join(home, ".couchfusion", "workspaces.json")}, nil
}

func (s *Service) List(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.Store.load()
	if err != nil {
		return nil, err
	}
	for index := range entries {
		entries[index].Available, entries[index].Problem = workspaceAvailability(entries[index].Root)
	}
	return entries, nil
}

func (s *Service) Register(ctx context.Context, root, name string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	canonical, err := canonicalWorkspace(root)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.Store.load()
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if samePath(entry.Root, canonical) {
			return Entry{}, ErrDuplicate
		}
	}

	id, err := s.newID()()
	if err != nil {
		return Entry{}, fmt.Errorf("create workspace id: %w", err)
	}
	for _, entry := range entries {
		if entry.ID == id {
			return Entry{}, fmt.Errorf("create workspace id: duplicate id %q", id)
		}
	}
	now := s.now()()
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(canonical)
	}
	entry := Entry{
		ID:           id,
		Name:         name,
		Root:         canonical,
		CreatedAt:    now,
		LastOpenedAt: now,
		Available:    true,
	}
	if err := s.Store.save(append(entries, entry)); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Service) Forget(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.Store.load()
	if err != nil {
		return err
	}
	updated := make([]Entry, 0, len(entries))
	found := false
	for _, entry := range entries {
		if entry.ID == id {
			found = true
			continue
		}
		updated = append(updated, entry)
	}
	if !found {
		return ErrNotFound
	}
	return s.Store.save(updated)
}

func (s *Service) Relink(ctx context.Context, id, root string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	canonical, err := canonicalWorkspace(root)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.Store.load()
	if err != nil {
		return Entry{}, err
	}
	index := -1
	for i, entry := range entries {
		if entry.ID == id {
			index = i
			continue
		}
		if samePath(entry.Root, canonical) {
			return Entry{}, ErrDuplicate
		}
	}
	if index < 0 {
		return Entry{}, ErrNotFound
	}
	entries[index].Root = canonical
	entries[index].LastOpenedAt = s.now()()
	entries[index].Available = true
	entries[index].Problem = ""
	if err := s.Store.save(entries); err != nil {
		return Entry{}, err
	}
	return entries[index], nil
}

func (s *Service) Touch(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.Store.load()
	if err != nil {
		return err
	}
	found := false
	for index := range entries {
		if entries[index].ID == id {
			entries[index].LastOpenedAt = s.now()()
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}
	return s.Store.save(entries)
}

func (s *Service) now() func() time.Time {
	if s.Now != nil {
		return s.Now
	}
	return func() time.Time { return time.Now().UTC() }
}

func (s *Service) newID() func() (string, error) {
	if s.NewID != nil {
		return s.NewID
	}
	return randomID
}

func (s Store) load() ([]Entry, error) {
	if strings.TrimSpace(s.Path) == "" {
		return nil, errors.New("workspace registry path is empty")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("read workspace registry: %w", err)
	}
	var document registryFile
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse workspace registry: %w", err)
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("unsupported workspace registry version %d", document.Version)
	}
	return append([]Entry{}, document.Entries...), nil
}

func (s Store) save(entries []Entry) error {
	if strings.TrimSpace(s.Path) == "" {
		return errors.New("workspace registry path is empty")
	}
	parent := filepath.Dir(s.Path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create workspace registry directory: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("secure workspace registry directory: %w", err)
	}
	data, err := json.MarshalIndent(registryFile{Version: 1, Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace registry: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(parent, ".workspaces-*.json")
	if err != nil {
		return fmt.Errorf("create temporary workspace registry: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary workspace registry: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary workspace registry: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary workspace registry: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary workspace registry: %w", err)
	}
	if err := os.Rename(temporaryPath, s.Path); err != nil {
		return fmt.Errorf("replace workspace registry: %w", err)
	}
	return os.Chmod(s.Path, 0o600)
}

func canonicalWorkspace(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%w: root is required", ErrInvalidWorkspace)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve root: %v", ErrInvalidWorkspace, err)
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(absolute))
	if err != nil {
		return "", fmt.Errorf("%w: resolve root: %v", ErrInvalidWorkspace, err)
	}
	if available, problem := workspaceAvailability(canonical); !available {
		return "", fmt.Errorf("%w: %s", ErrInvalidWorkspace, problem)
	}
	return canonical, nil
}

func workspaceAvailability(root string) (bool, string) {
	info, err := os.Stat(root)
	if err != nil {
		return false, "workspace root is unavailable"
	}
	if !info.IsDir() {
		return false, "workspace root is not a directory"
	}
	for _, name := range []string{"apps", "layers"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || !info.IsDir() {
			return false, fmt.Sprintf("workspace is missing %s directory", name)
		}
	}
	return true, ""
}

func samePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
