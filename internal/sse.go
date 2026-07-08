package internal

import (
	"fmt"
	"net/http"
)

// serveEvents returns a handler bound to one role (participant, display, or
// admin) via closure — the mux registers a separate literal route per role,
// so there's no per-request role parsing or validation to get wrong here.
// This file only knows about the SSE wire format; it has no idea what a
// "poll" or a "phase" is, that all lives in hub.go.
func (a *App) serveEvents(role Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		sessionID := getOrCreateSession(w, r)

		if role == RoleDisplay {
			a.hub.SetBaseURL(a.resolveBaseURL(r))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		ch := a.hub.Subscribe(role, sessionID)
		defer a.hub.Unsubscribe(role, ch)

		// Catch-up snapshot: covers first load, refresh, reconnect after a
		// network blip, and a duplicate tab, all through the same path —
		// whatever the current state is, this viewer sees it immediately.
		writeEvent(w, a.hub.Snapshot(role, sessionID))
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-ch:
				if !ok {
					return
				}
				writeEvent(w, payload)
				flusher.Flush()
			}
		}
	}
}

// writeEvent writes one SSE "message" event. Multi-line payloads need a
// "data: " prefix on every line per the SSE spec.
func writeEvent(w http.ResponseWriter, payload string) {
	for _, line := range splitLines(payload) {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	return append(lines, s[start:])
}
