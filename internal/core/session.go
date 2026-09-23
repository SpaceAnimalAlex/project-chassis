package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrSessionNotFound     = errors.New("session not found or expired")
)

// Session represents one authenticated device/browser for a user.
// A user can hold many concurrent Sessions — one per device — by design:
// there is no artificial single-session-per-user restriction anywhere in
// this contract, so logging in from a desktop, a phone, and a tablet at the
// same time all just works.
type Session struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	DeviceLabel string    `json:"device_label"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	IsCurrent   bool      `json:"is_current,omitempty"` // set only when listing sessions for the requesting device
}

// AuthService defines the login/session contract. It follows the same
// pattern as WorkItemService: the interface lives in core, the concrete
// implementation lives in db.Repository — the HTTP layer never sees SQL.
type AuthService interface {
	// Authenticate verifies email/password and returns the matching active user.
	Authenticate(ctx context.Context, email, password string) (*User, error)
	// CreateSession issues a new session for userID and returns the raw
	// bearer token (only the token's hash is persisted).
	CreateSession(ctx context.Context, userID int64, deviceLabel string, ttl time.Duration) (token string, session *Session, err error)
	// ValidateSession resolves a raw token to its user and session, and
	// slides the session's expiry forward (bounded by the session's original
	// absolute lifetime — see Repository.maxSessionLifetime).
	ValidateSession(ctx context.Context, token string) (*User, *Session, error)
	// RevokeSession deletes a single session by its raw token (logout).
	RevokeSession(ctx context.Context, token string) error
	// RevokeOtherSessions deletes every session for userID except the one
	// matching keepToken — "log out of all other devices."
	RevokeOtherSessions(ctx context.Context, userID int64, keepToken string) error
	// ListSessions returns every active session for a user, for a
	// "your devices" view.
	ListSessions(ctx context.Context, userID int64, currentToken string) ([]Session, error)
	// PruneExpiredSessions deletes sessions past their expiry; intended to be
	// called periodically from a background sweep, not per-request.
	PruneExpiredSessions(ctx context.Context) (int64, error)
}

// userContextKey is unexported so only ContextWithUser/UserFromContext in
// this package can set or read it — no other package can forge an actor.
type userContextKey struct{}

// ContextWithUser attaches the authenticated user to ctx. Called once, by
// the web layer's Auth middleware, after a session token has been validated.
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userContextKey{}, u)
}

// UserFromContext retrieves the authenticated user attached by the Auth
// middleware. Handlers use this instead of trusting any client-supplied
// header for identity.
func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userContextKey{}).(*User)
	return u, ok
}
