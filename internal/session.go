package internal

import (
	"net/http"
	"time"
)

const sessionCookieName = "pollinator_session"

// isSecureRequest reports whether this request arrived over HTTPS, either
// directly or via a trusted reverse proxy's X-Forwarded-Proto header —
// Railway, Cloud Run, and similar platforms terminate TLS at a proxy and
// forward plain HTTP to the container, so r.TLS alone isn't sufficient.
// Shared by the session cookie's Secure flag and resolveBaseURL's scheme
// inference (see app.go) — one place decides what "secure" means here.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}

// getOrCreateSession is the entire identity layer: no accounts, no login.
// A random session ID in an HttpOnly cookie is enough to let a refresh or a
// dropped connection reconnect as the same participant, while remaining
// invisible to the person using it. Because reads happen over SSE (which
// carries cookies on the initial request same as any other GET) and writes
// happen over plain POST, there is no separate token-passing dance needed
// for either transport.
func getOrCreateSession(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}

	sessionID := randomToken()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	return sessionID
}
