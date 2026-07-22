// Package auth implements the bounded, single-administrator browser session.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

const CookieName = "agentshark_session"

var ErrInvalidCredentials = errors.New("invalid credentials")

type Options struct {
	CookieSecure bool
	TTL          time.Duration
}

type Session struct {
	csrf      string
	expiresAt time.Time
}

type Manager struct {
	mu           sync.RWMutex
	adminHash    [32]byte
	sessions     map[string]Session
	cookieSecure bool
	ttl          time.Duration
}

func New(adminToken string, options Options) *Manager {
	if options.TTL <= 0 {
		options.TTL = 8 * time.Hour
	}
	return &Manager{
		adminHash:    sha256.Sum256([]byte(adminToken)),
		sessions:     make(map[string]Session, 1),
		cookieSecure: options.CookieSecure,
		ttl:          options.TTL,
	}
}

func (manager *Manager) Login(writer http.ResponseWriter, provided string) (string, error) {
	providedHash := sha256.Sum256([]byte(provided))
	if subtle.ConstantTimeCompare(manager.adminHash[:], providedHash[:]) != 1 {
		return "", ErrInvalidCredentials
	}
	sessionID, err := randomToken()
	if err != nil {
		return "", errors.New("could not create session")
	}
	csrf, err := randomToken()
	if err != nil {
		return "", errors.New("could not create session")
	}
	expiresAt := time.Now().Add(manager.ttl)
	manager.mu.Lock()
	clear(manager.sessions)
	manager.sessions[sessionID] = Session{csrf: csrf, expiresAt: expiresAt}
	manager.mu.Unlock()

	http.SetCookie(writer, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(manager.ttl.Seconds()),
		HttpOnly: true,
		Secure:   manager.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	return csrf, nil
}

func (manager *Manager) Authenticate(request *http.Request) (Session, bool) {
	cookie, err := request.Cookie(CookieName)
	if err != nil {
		return Session{}, false
	}
	manager.mu.RLock()
	session, ok := manager.sessions[cookie.Value]
	manager.mu.RUnlock()
	if !ok || time.Now().After(session.expiresAt) {
		if ok {
			manager.mu.Lock()
			delete(manager.sessions, cookie.Value)
			manager.mu.Unlock()
		}
		return Session{}, false
	}
	return session, true
}

func (*Manager) ValidCSRF(session Session, provided string) bool {
	expectedHash := sha256.Sum256([]byte(session.csrf))
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
