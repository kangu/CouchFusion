package mutationlease

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var ErrInvalidOwner = errors.New("mutation lease owner metadata is required")

type Owner struct {
	ID   string
	Type string
}

type ConflictError struct {
	Scope           string
	ActiveOwnerID   string
	ActiveOwnerType string
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("mutation scope %q is owned by %s %q", e.Scope, e.ActiveOwnerType, e.ActiveOwnerID)
}

type Manager struct {
	mu     sync.Mutex
	owners map[string]Owner
}

func NewManager() *Manager {
	return &Manager{owners: map[string]Owner{}}
}

func (m *Manager) Acquire(scope, ownerID, ownerType string) (func(), error) {
	scope = strings.TrimSpace(scope)
	ownerID = strings.TrimSpace(ownerID)
	ownerType = strings.TrimSpace(ownerType)
	if ownerID == "" || ownerType == "" {
		return nil, ErrInvalidOwner
	}
	if scope == "" {
		return func() {}, nil
	}

	m.mu.Lock()
	if active, exists := m.owners[scope]; exists {
		m.mu.Unlock()
		return nil, ConflictError{Scope: scope, ActiveOwnerID: active.ID, ActiveOwnerType: active.Type}
	}
	owner := Owner{ID: ownerID, Type: ownerType}
	m.owners[scope] = owner
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if active, exists := m.owners[scope]; exists && active == owner {
				delete(m.owners, scope)
			}
		})
	}, nil
}
