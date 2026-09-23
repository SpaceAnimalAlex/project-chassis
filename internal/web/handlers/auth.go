package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/web/session"
)

// sessionIdleTTL is the sliding-window idle timeout; Repository.ValidateSession
// enforces the absolute 30-day ceiling on top of this.
const sessionIdleTTL = 12 * time.Hour

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login handles POST /api/auth/login. On success it issues a new session —
// additive to whatever other sessions the account already holds elsewhere,
// so signing in on a phone never signs the desktop out.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.Auth.Authenticate(r.Context(), req.Email, req.Password)
	if errors.Is(err, core.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		h.Logger.Error("authenticate failed", "error", err)
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	deviceLabel := session.DescribeDevice(r.UserAgent())
	token, sess, err := h.Auth.CreateSession(r.Context(), user.ID, deviceLabel, sessionIdleTTL)
	if err != nil {
		h.Logger.Error("create session failed", "error", err)
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	session.SetCookie(w, r, token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":        user.ID,
			"email":     user.Email,
			"full_name": user.FullName,
			"role":      user.Role,
		},
		"device_label": deviceLabel,
	})
}

// Logout handles POST /api/auth/logout — ends only the calling device's session.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(session.CookieName); err == nil {
		_ = h.Auth.RevokeSession(r.Context(), cookie.Value)
	}
	session.ClearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// LogoutOthers handles POST /api/auth/logout-others — the panic button for a
// lost or stolen device: ends every session for this account except the one
// making the request.
func (h *Handlers) LogoutOthers(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(session.CookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.Auth.RevokeOtherSessions(r.Context(), actor(r).ID, cookie.Value); err != nil {
		h.Logger.Error("revoke other sessions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to sign out other devices")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Sessions handles GET /api/auth/sessions — the "your devices" list, so an
// operator can see (and then kill) every device currently signed in.
func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	var currentToken string
	if cookie, err := r.Cookie(session.CookieName); err == nil {
		currentToken = cookie.Value
	}

	sessions, err := h.Auth.ListSessions(r.Context(), actor(r).ID, currentToken)
	if err != nil {
		h.Logger.Error("list sessions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

// CurrentUser handles GET /api/auth/session — "who am I," used by app.js on
// every page load to decide whether to show the login screen.
func (h *Handlers) CurrentUser(w http.ResponseWriter, r *http.Request) {
	u, ok := core.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "email": u.Email, "full_name": u.FullName, "role": u.Role,
	})
}

// RenderLogin handles GET /login.
func (h *Handlers) RenderLogin(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "login.html", nil); err != nil {
		h.Logger.Error("render login failed", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}
