package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"sync"
)

const sessionCookieName = "couchfusion_session"

type sessionContextKey struct{}

type sessionManager struct {
	mu         sync.Mutex
	random     io.Reader
	launchHash [sha256.Size]byte
	launchUsed bool
	sessions   map[[sha256.Size]byte]string
}

func newSessionManager(random io.Reader, launchToken string) *sessionManager {
	return &sessionManager{
		random:     random,
		launchHash: sha256.Sum256([]byte(launchToken)),
		sessions:   map[[sha256.Size]byte]string{},
	}
}

func (s *sessionManager) exchange(token string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := sha256.Sum256([]byte(token))
	if s.launchUsed || subtle.ConstantTimeCompare(candidate[:], s.launchHash[:]) != 1 {
		return "", "", errors.New("launch token is invalid or already consumed")
	}
	sessionToken, err := randomToken(s.random)
	if err != nil {
		return "", "", err
	}
	sessionHash := sha256.Sum256([]byte(sessionToken))
	sessionID := stringHash(sessionHash)
	s.sessions[sessionHash] = sessionID
	s.launchUsed = true
	return sessionToken, sessionID, nil
}

func (s *sessionManager) authenticate(request *http.Request) (string, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionID, exists := s.sessions[hash]
	return sessionID, exists
}

func withSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

func SessionID(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(sessionContextKey{}).(string)
	return value, ok && value != ""
}

func stringHash(value [sha256.Size]byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2] = alphabet[item>>4]
		result[index*2+1] = alphabet[item&0x0f]
	}
	return string(result)
}
