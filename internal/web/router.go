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
func NewRouter(service core.WorkItemService, authSvc core.AuthService, registry *implement.Registry, tracker *presence.Tracker, hub *stream.Hub, logger *slog.Logger) (http.Handler, error) {
	templates, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	h := handlers.New(service, authSvc, registry, tracker, hub, templates, logger)

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

	mux.HandleFunc("GET /api/items/{id}", h.GetItem)
	mux.HandleFunc("POST /api/items/{id}/transition", h.TransitionStatus)
	mux.HandleFunc("POST /api/items/{id}/messages", h.AddMessage)
	mux.HandleFunc("POST /api/items/{id}/assign", h.AssignItem)
	mux.HandleFunc("POST /api/items/{id}/assets/{assetID}", h.LinkAsset)
	mux.HandleFunc("DELETE /api/items/{id}/assets/{assetID}", h.UnlinkAsset)

	mux.HandleFunc("POST /api/items/{id}/presence", h.Heartbeat)
	mux.HandleFunc("DELETE /api/items/{id}/presence", h.ReleasePresence)

	mux.HandleFunc("GET /api/items/{id}/stream", h.Stream)
	mux.HandleFunc("POST /api/items/{id}/typing", h.Typing)

	return Chain(mux, Recover(logger), Log(logger), CSRFOriginCheck(), Auth(authSvc, PublicPath)), nil
}
