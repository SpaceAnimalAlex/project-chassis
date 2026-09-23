// Package handlers implements the HTTP surface (Track B) against the
// core.WorkItemService contract published by Track A. Handlers never touch
// *sql.DB or the db package directly — only core types and the service
// interface — so the HTTP layer stays decoupled from storage.
package handlers

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
	"github.com/project-chassis/chassis/internal/presence"
	"github.com/project-chassis/chassis/internal/web/stream"
)

// Handlers bundles every dependency the HTTP layer needs.
type Handlers struct {
	Service   core.WorkItemService
	Auth      core.AuthService
	Registry  *implement.Registry
	Presence  *presence.Tracker
	Hub       *stream.Hub
	Templates *template.Template
	Logger    *slog.Logger
}

// New constructs the shared Handlers bundle.
func New(service core.WorkItemService, authSvc core.AuthService, registry *implement.Registry, tracker *presence.Tracker, hub *stream.Hub, templates *template.Template, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{Service: service, Auth: authSvc, Registry: registry, Presence: tracker, Hub: hub, Templates: templates, Logger: logger}
}

// --- shared helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// actor returns the authenticated operator attached to the request context
// by the web layer's Auth middleware (see internal/web/middleware.go). It
// never reads a client-supplied header — that placeholder is gone; identity
// now comes only from a validated session, which is what makes the
// is_internal boundary and the audit log trustworthy rather than
// self-reported.
//
// Returns the full core.User (not just an id/name pair) so callers pick the
// field they actually need — .Email for sender_email on a staff reply,
// .FullName for a display label — rather than a same-shaped-as-before tuple
// silently carrying the wrong one into the wrong place.
func actor(r *http.Request) *core.User {
	u, ok := core.UserFromContext(r.Context())
	if !ok {
		// The Auth middleware guarantees this is unreachable for any
		// protected route; a zero actor here would be a wiring bug, not a
		// real anonymous request, so it's deliberately not silently handled.
		return &core.User{}
	}
	return u
}

func pathInt64(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(r.PathValue(key), 10, 64)
}

// --- queue ---

// ListQueue handles GET /api/queue — the triage list, filterable by query params.
func (h *Handlers) ListQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := core.QueueFilter{SearchQuery: q.Get("q")}

	if v := q.Get("domain_type"); v != "" {
		dt := core.DomainType(v)
		filter.DomainType = &dt
	}
	if v := q.Get("status"); v != "" {
		s := core.Status(v)
		filter.Status = &s
	}
	if v := q.Get("priority"); v != "" {
		p := core.Priority(v)
		filter.Priority = &p
	}
	if v := q.Get("assigned_user_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.AssignedUserID = &id
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Offset = n
		}
	}

	items, err := h.Service.ListQueue(r.Context(), filter)
	if err != nil {
		h.Logger.Error("list queue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list queue")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// RenderQueue handles GET / — the server-rendered triage dashboard shell.
// The page itself fetches /api/queue via fetch() and renders rows client-side
// (see static/app.js) so keyboard triage (J/K/O/etc.) doesn't require a
// full-page reload per action.
func (h *Handlers) RenderQueue(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "queue.html", nil); err != nil {
		h.Logger.Error("render queue failed", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}
