package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/web/session"
)

// Middleware wraps an http.Handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in order, so the first one listed runs outermost
// (first on the request, last on the response).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush delegates to the underlying ResponseWriter's Flusher when present.
// Without this, wrapping every response in a statusRecorder would silently
// break Server-Sent Events: http.ResponseWriter doesn't include Flush, so
// it isn't promoted by embedding, and the SSE handler's type assertion to
// http.Flusher would fail on every request that passed through Log.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Log records method, path, status, and duration for every request.
func Log(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// Recover converts a panic in any handler into a 500 instead of taking down
// the whole process — a single malformed ticket must not crash the shop's
// only queue for the day.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "error", rec, "path", r.URL.Path)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// PublicPath reports whether a request path may proceed without a valid
// session: the login page/API, the embedded static bundle, and the health
// probe. Everything else — every ticket, every message, every asset link —
// requires authentication.
func PublicPath(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/login":
		return true
	case p == "/api/auth/login":
		return true
	case p == "/api/health":
		return true
	case p == "/api/webhooks/voice/incoming":
		// Authenticated by its own shared secret (VoiceIncoming checks
		// X-Webhook-Secret), not a session cookie — a PBX/SIP provider has
		// neither an operator session nor any way to obtain one.
		return true
	case strings.HasPrefix(p, "/static/"):
		return true
	default:
		return false
	}
}

// Auth resolves the session cookie to an authenticated user and attaches it
// to the request context via core.ContextWithUser. Requests to a public
// path (per isPublic) pass through unauthenticated; everything else gets a
// 401 (API paths) or a redirect to /login (page paths) if the cookie is
// missing, invalid, or expired.
//
// This is the sole source of caller identity for every handler downstream —
// nothing in the handler layer trusts a client-supplied header for who the
// actor is, which is what actually makes the is_internal boundary and the
// audit log trustworthy.
func Auth(authSvc core.AuthService, isPublic func(*http.Request) bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublic(r) {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				denyUnauthenticated(w, r)
				return
			}

			user, _, err := authSvc.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				session.ClearCookie(w, r)
				denyUnauthenticated(w, r)
				return
			}

			ctx := core.ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func denyUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
		return
	}
	next := url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, "/login?next="+next, http.StatusSeeOther)
}

// CSRFOriginCheck rejects state-changing requests whose Origin header names
// a different host than the one being called. SameSite=Lax on the session
// cookie already blocks it from being attached to most cross-site requests,
// but browsers that predate SameSite defaults (or a misconfigured proxy)
// make this a cheap second layer — no token machinery, standard library
// only, appropriate for a self-hosted LAN tool that isn't embedding
// third-party widgets.
func CSRFOriginCheck() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStateChanging(r.Method) {
				if origin := r.Header.Get("Origin"); origin != "" {
					originURL, err := url.Parse(origin)
					if err != nil || !strings.EqualFold(originURL.Host, r.Host) {
						http.Error(w, "cross-origin request rejected", http.StatusForbidden)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
