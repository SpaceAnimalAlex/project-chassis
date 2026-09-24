// Package web is the HTTP surface (Track B): routing, embedded static
// assets/templates, and middleware. It depends only on core types, the
// implement registry, and the presence tracker — never on internal/db — so
// storage stays swappable without touching this package.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/implement"
	"github.com/project-chassis/chassis/internal/presence"
	"github.com/project-chassis/chassis/internal/web/handlers"
	"github.com/project-chassis/chassis/internal/web/stream"
)

//go:embed static/*
var staticFS embed.FS

//go:embed templates/*.html
var templateFS embed.FS

// NewRouter builds the complete HTTP handler for Chassis: JSON API, embedded
// static assets, and the server-rendered page shells. Every route beyond the
// static bundle is registered with an explicit method, per Go 1.22+
// http.ServeMux pattern matching (no third-party router).
func NewRouter(service core.WorkItemService, authSvc core.AuthService, contacts core.ContactService, ingestor core.InboundIngestor, registry *implement.Registry, tracker *presence.Tracker, hub *stream.Hub, logger *slog.Logger) (http.Handler, error) {
	templates, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	h := handlers.New(service, authSvc, contacts, ingestor, registry, tracker, hub, templates, logger)
	// Read directly rather than widening NewRouter's already-long parameter
	// list for one optional webhook credential; an empty secret makes
	// VoiceIncoming reject every request (see handlers/telephony.go).
	h.VoiceWebhookSecret = os.Getenv("CHASSIS_VOICE_WEBHOOK_SECRET")

	mux := http.NewServeMux()

	// Page shells
	mux.HandleFunc("GET /{$}", h.RenderQueue)
	mux.HandleFunc("GET /items/{id}", h.RenderItem)
	mux.HandleFunc("GET /login", h.RenderLogin)

	// Static assets, embedded via go:embed — strip the leading "static/" so
	// the FS root lines up with the /static/ URL prefix.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// Auth API
	mux.HandleFunc("POST /api/auth/login", h.Login)
	mux.HandleFunc("POST /api/auth/logout", h.Logout)
	mux.HandleFunc("POST /api/auth/logout-others", h.LogoutOthers)
	mux.HandleFunc("GET /api/auth/session", h.CurrentUser)
	mux.HandleFunc("GET /api/auth/sessions", h.Sessions)

	// JSON API
	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/implements", h.ListImplements)

	mux.HandleFunc("GET /api/queue", h.ListQueue)
	mux.HandleFunc("GET /api/organizations", h.ListOrganizations)

	mux.HandleFunc("GET /api/items/{id}", h.GetItem)
	mux.HandleFunc("POST /api/items/{id}/transition", h.TransitionStatus)
	mux.HandleFunc("POST /api/items/{id}/messages", h.AddMessage)
	mux.HandleFunc("POST /api/items/{id}/assign", h.AssignItem)
	mux.HandleFunc("POST /api/items/{id}/assets/{assetID}", h.LinkAsset)
	mux.HandleFunc("DELETE /api/items/{id}/assets/{assetID}", h.UnlinkAsset)
	mux.HandleFunc("POST /api/items/{id}/promote-to-contact", h.PromoteToContact)

	mux.HandleFunc("POST /api/items/{id}/presence", h.Heartbeat)
	mux.HandleFunc("DELETE /api/items/{id}/presence", h.ReleasePresence)

	mux.HandleFunc("GET /api/items/{id}/stream", h.Stream)
	mux.HandleFunc("POST /api/items/{id}/typing", h.Typing)

	mux.HandleFunc("GET /api/stream/operator", h.OperatorStream)

	// Telephony ingress — authenticated by its own shared secret (see
	// VoiceIncoming), not an operator session, so it must bypass Auth's
	// cookie check; PublicPath in middleware.go carries this exemption.
	mux.HandleFunc("POST /api/webhooks/voice/incoming", h.VoiceIncoming)

	return Chain(mux, Recover(logger), Log(logger), CSRFOriginCheck(), Auth(authSvc, PublicPath)), nil
}
