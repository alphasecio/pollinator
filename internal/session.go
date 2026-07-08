package internal

import (
	"net/http"
	"time"
)

const sessionCookieName = "pollinator_session"

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
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	return sessionID
}
