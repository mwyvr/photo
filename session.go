package photo

import (
	"context"
	"time"

	"github.com/mwyvr/kid"
)

// Session represents an authenticated CLI or HTTP session.
// The raw JWT is never stored; only a SHA-256 hash of the token is kept
// so the database cannot be used to forge tokens.
type Session struct {
	ID        kid.ID    `json:"id"`
	UserID    kid.ID    `json:"userId"`
	TokenHash string    `json:"-"` // SHA-256 hex of the raw JWT
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// IsExpired returns true if the session has passed its expiry time.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// SessionService manages authentication sessions.
type SessionService interface {
	// CreateSession persists a new session record.
	CreateSession(ctx context.Context, session *Session) error

	// FindSessionByTokenHash retrieves a session by its token hash.
	// Returns ENOTFOUND if no matching session exists.
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)

	// DeleteSession removes a session (logout).
	// Returns ENOTFOUND if the session does not exist.
	DeleteSession(ctx context.Context, id kid.ID) error

	// DeleteExpiredSessions removes all sessions past their expiry time.
	// Called periodically by the server.
	DeleteExpiredSessions(ctx context.Context) error
}
