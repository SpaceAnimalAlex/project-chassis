package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/project-chassis/chassis/internal/core"
)

// maxSessionLifetime is the absolute cap on a session's life, regardless of
// how often it's renewed by ValidateSession's sliding window. A lost or
// stolen device's session dies on its own within this window even if no one
// ever explicitly revokes it.
const maxSessionLifetime = 30 * 24 * time.Hour

var _ core.AuthService = (*Repository)(nil)

// Authenticate verifies email + password against the stored bcrypt hash.
// Returns core.ErrInvalidCredentials for any failure mode (unknown email,
// inactive account, wrong password) so callers can't distinguish "no such
// user" from "wrong password" by timing or error message — that
// distinction is exactly what account enumeration attacks rely on.
func (r *Repository) Authenticate(ctx context.Context, email, password string) (*core.User, error) {
	var u core.User
	var passwordHash string
	var createdAtStr string
	var isActive int

	err := r.db.QueryRowContext(ctx, `
		SELECT id, uuid, email, full_name, role, is_active, password_hash, created_at
		FROM users WHERE email = ?;
	`, email).Scan(&u.ID, &u.UUID, &u.Email, &u.FullName, &u.Role, &isActive, &passwordHash, &createdAtStr)

	if errors.Is(err, sql.ErrNoRows) {
		// Still run a bcrypt comparison against a dummy hash so a lookup on a
		// nonexistent email takes roughly the same time as a wrong-password
		// lookup on a real one.
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return nil, core.ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	u.IsActive = isActive == 1
	u.CreatedAt = parseTime(createdAtStr)

	if !u.IsActive || passwordHash == "" {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return nil, core.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, core.ErrInvalidCredentials
	}

	return &u, nil
}

// dummyHash is a valid bcrypt hash of a random value, used only to burn
// roughly the same CPU time as a real comparison when short-circuiting.
const dummyHash = "$2a$10$C6UzMDM.H6dfI/f/IKcEeO0ZrRmT1v3Fs7fN6pKF.jXKk0e6z1x8u"

// SetPassword hashes and stores a new password for a user. Used by the
// bootstrap-admin flow and any future "change password" handler.
func (r *Repository) SetPassword(ctx context.Context, userID int64, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	_, err = r.db.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", string(hash), userID)
	return err
}

// CreateSession issues a new session and returns the raw bearer token.
// Only SHA-256(token) is ever persisted; the raw token exists solely in the
// response cookie, so a leaked database dump can't be replayed as sessions.
func (r *Repository) CreateSession(ctx context.Context, userID int64, deviceLabel string, ttl time.Duration) (string, *core.Session, error) {
	token, err := generateToken()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	if ttl <= 0 || ttl > maxSessionLifetime {
		ttl = maxSessionLifetime
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	res, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, device_label, created_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`, hashToken(token), userID, deviceLabel, now, now, expiresAt)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", nil, err
	}

	return token, &core.Session{
		ID:          id,
		UserID:      userID,
		DeviceLabel: deviceLabel,
		CreatedAt:   now,
		LastSeenAt:  now,
		ExpiresAt:   expiresAt,
	}, nil
}

// ValidateSession resolves a raw bearer token to its user and session,
// sliding the expiry forward on every use (bounded by maxSessionLifetime
// from the session's original creation, so renewal can't extend a session
// forever).
func (r *Repository) ValidateSession(ctx context.Context, token string) (*core.User, *core.Session, error) {
	if token == "" {
		return nil, nil, core.ErrSessionNotFound
	}
	tokenHash := hashToken(token)

	var s core.Session
	var u core.User
	var isActive int
	var createdAtStr, lastSeenStr, expiresStr, userCreatedStr string

	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.device_label, s.created_at, s.last_seen_at, s.expires_at,
		       u.id, u.uuid, u.email, u.full_name, u.role, u.is_active, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?;
	`, tokenHash).Scan(
		&s.ID, &s.UserID, &s.DeviceLabel, &createdAtStr, &lastSeenStr, &expiresStr,
		&u.ID, &u.UUID, &u.Email, &u.FullName, &u.Role, &isActive, &userCreatedStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, core.ErrSessionNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query session: %w", err)
	}

	s.CreatedAt = parseTime(createdAtStr)
	s.LastSeenAt = parseTime(lastSeenStr)
	s.ExpiresAt = parseTime(expiresStr)
	u.CreatedAt = parseTime(userCreatedStr)
	u.IsActive = isActive == 1

	now := time.Now().UTC()
	if now.After(s.ExpiresAt) || !u.IsActive {
		// Expired or the account was deactivated after the session was issued.
		_, _ = r.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", s.ID)
		return nil, nil, core.ErrSessionNotFound
	}

	// Slide the window, capped at the session's absolute lifetime.
	absoluteCeiling := s.CreatedAt.Add(maxSessionLifetime)
	newExpiry := now.Add(12 * time.Hour)
	if newExpiry.After(absoluteCeiling) {
		newExpiry = absoluteCeiling
	}
	if newExpiry.After(s.ExpiresAt) {
		if _, err := r.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?", now, newExpiry, s.ID); err == nil {
			s.ExpiresAt = newExpiry
		}
	} else {
		_, _ = r.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE id = ?", now, s.ID)
	}
	s.LastSeenAt = now

	return &u, &s, nil
}

// RevokeSession deletes a single session by its raw token (logout on this device).
func (r *Repository) RevokeSession(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashToken(token))
	return err
}

// RevokeOtherSessions deletes every session for userID except the one
// matching keepToken — "log out of all other devices," the panic button for
// a lost or stolen device.
func (r *Repository) RevokeOtherSessions(ctx context.Context, userID int64, keepToken string) error {
	_, err := r.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id = ? AND token_hash != ?",
		userID, hashToken(keepToken),
	)
	return err
}

// ListSessions returns every active session for a user (for a "your
// devices" view), marking which one is the caller's current device.
func (r *Repository) ListSessions(ctx context.Context, userID int64, currentToken string) ([]core.Session, error) {
	currentHash := hashToken(currentToken)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, device_label, created_at, last_seen_at, expires_at, token_hash
		FROM sessions WHERE user_id = ? ORDER BY last_seen_at DESC;
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []core.Session
	for rows.Next() {
		var s core.Session
		var createdAtStr, lastSeenStr, expiresStr, tokenHash string
		if err := rows.Scan(&s.ID, &s.UserID, &s.DeviceLabel, &createdAtStr, &lastSeenStr, &expiresStr, &tokenHash); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		s.CreatedAt = parseTime(createdAtStr)
		s.LastSeenAt = parseTime(lastSeenStr)
		s.ExpiresAt = parseTime(expiresStr)
		s.IsCurrent = tokenHash == currentHash
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// PruneExpiredSessions deletes every session past its expiry. Intended to be
// called periodically from a background sweep (see cmd/chassis/main.go),
// not per-request — ValidateSession already self-cleans on the read path.
func (r *Repository) PruneExpiredSessions(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
