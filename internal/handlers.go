package internal

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// --- pages ---
//
// Every page renders "base" directly — there is no per-page template file
// to collide on, since html/template shares one name space across every
// parsed file. join/display/admin share "sse_wrapper" (they only differ in
// which SSE endpoint they connect to).

func (a *App) indexPage(w http.ResponseWriter, r *http.Request) {
	sessionID := getOrCreateSession(w, r)
	a.renderSSEPage(w, "/events", a.hub.Snapshot(RoleParticipant, sessionID))
}

func (a *App) displayPage(w http.ResponseWriter, r *http.Request) {
	sessionID := getOrCreateSession(w, r)
	a.renderSSEPage(w, "/display/events", a.hub.Snapshot(RoleDisplay, sessionID))
}

func (a *App) adminPage(w http.ResponseWriter, r *http.Request) {
	a.hub.SetBaseURL(a.resolveBaseURL(r))
	sessionID := getOrCreateSession(w, r)
	a.renderSSEPage(w, a.AdminURL()+"/events", a.hub.Snapshot(RoleAdmin, sessionID))
}

// renderSSEPage wraps an already-rendered hub fragment in the SSE-connected
// container and hands the result to renderBase. This is the one place that
// knows join/display/admin share identical wiring and differ only in which
// endpoint they connect to.
func (a *App) renderSSEPage(w http.ResponseWriter, connectURL, inner string) {
	var buf bytes.Buffer
	err := a.templates.ExecuteTemplate(&buf, "sse_wrapper", map[string]any{
		"ConnectURL": connectURL,
		"Inner":      template.HTML(inner),
	})
	if err != nil {
		a.logger.Error("render sse wrapper failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	a.renderBase(w, template.HTML(buf.String()))
}

func (a *App) renderBase(w http.ResponseWriter, content template.HTML) {
	data := map[string]any{
		"EventTitle": a.hub.EventTitle(),
		"Content":    content,
		"AssetVer":   a.assetVer,
	}
	if err := a.templates.ExecuteTemplate(w, "base", data); err != nil {
		a.logger.Error("render page failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- participant actions ---

func (a *App) joinAction(w http.ResponseWriter, r *http.Request) {
	sessionID := getOrCreateSession(w, r)
	alias := strings.TrimSpace(r.FormValue("alias"))

	if err := a.hub.Join(sessionID, alias); err != nil {
		writeInlineError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) answerAction(w http.ResponseWriter, r *http.Request) {
	sessionID := getOrCreateSession(w, r)

	choice, err := strconv.Atoi(r.FormValue("choice"))
	if err != nil {
		http.Error(w, "invalid choice", http.StatusBadRequest)
		return
	}

	if err := a.hub.Answer(sessionID, choice); err != nil {
		writeInlineError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeInlineError renders a small fragment for the one requester whose
// action failed (alias taken, answered twice, invalid poll data, etc).
// This is deliberately not a hub broadcast — it's not state anyone else
// needs to see, just feedback for the person who tried the thing.
//
// Status is 200, not 4xx: htmx only swaps the response body into
// hx-target on a 2xx by default, so a 4xx here — even with a perfectly
// good error fragment in the body — renders as nothing happening at all.
func writeInlineError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<p class="text-sm text-red-400 mt-2">%s</p>`, template.HTMLEscapeString(err.Error()))
}

// --- admin actions ---
//
// Every admin action just dispatches to the hub and returns 204. The actual
// UI change reaches every viewer, including the admin who clicked the
// button, through the SSE broadcast — there is nothing left for the HTTP
// response itself to render.

func (a *App) adminAction(fn func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// savePollAction handles both first-time setup (poll was nil) and editing
// an existing poll after Reset — SetPoll itself is what enforces "only
// while Waiting," so this handler doesn't need to know which case it is.
// The submitted data is the same JSON shape as POLL_JSON, so ParsePoll is
// reused exactly as-is rather than having two copies of validation logic.
func (a *App) savePollAction(w http.ResponseWriter, r *http.Request) {
	poll, err := ParsePoll(r.FormValue("poll_json"))
	if err != nil {
		writeInlineError(w, err)
		return
	}
	if err := a.hub.SetPoll(poll); err != nil {
		writeInlineError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// downloadResultsAction is a plain GET, not an htmx action — it's a file
// download, and htmx would try to swap the CSV body into the DOM as if it
// were HTML. A regular <a href> download link is the correct tool here.
func (a *App) downloadResultsAction(w http.ResponseWriter, r *http.Request) {
	results := a.hub.Results()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="poll-results.csv"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"question", "option", "votes"})
	for _, result := range results {
		for i, opt := range result.Options {
			_ = cw.Write([]string{result.Question, opt, strconv.Itoa(result.Counts[i])})
		}
	}
	cw.Flush()
}
