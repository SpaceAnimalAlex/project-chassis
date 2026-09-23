// Package session holds the session-cookie mechanics shared between the
// handlers package (sets the cookie on login) and the web package's Auth
// middleware (reads/clears it). It's a separate leaf package specifically
// so neither of those two needs to import the other — web already imports
// handlers to build the router, so handlers importing web back would cycle.
package session

import (
	"net/http"
	"strings"
	"time"
)

// CookieName is the cookie the browser holds the session bearer token in.
const CookieName = "chassis_session"

// SetCookie writes the session cookie after a successful login.
// HttpOnly so client JS (and therefore XSS) can never read the token,
// SameSite=Lax so it isn't attached to cross-site subrequests (the primary
// CSRF defense), Secure only when the request arrived over TLS — this app
// is expected to run unencrypted on a bare LAN with no reverse proxy, and
// requiring Secure unconditionally would silently break login there.
func SetCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

// ClearCookie removes the session cookie (logout, or an invalid/expired
// session detected by the Auth middleware).
func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

// DescribeDevice turns a User-Agent string into a short human label for the
// "your devices" list (e.g. "Chrome on Windows"). It's cosmetic only — never
// used for any security decision — so a best-effort heuristic is fine.
func DescribeDevice(userAgent string) string {
	ua := userAgent
	browser := "Unknown browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/") && !strings.Contains(ua, "Chrome/"):
		browser = "Safari"
	}

	os := "Unknown OS"
	switch {
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Mac OS X"):
		os = "macOS"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}

	if ua == "" {
		return "Unknown device"
	}
	return browser + " on " + os
}
