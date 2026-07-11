package internal

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Role is who's watching: each gets its own rendered view of the same
// underlying state, and its own SSE stream.
type Role string

const (
	RoleParticipant Role = "participant"
	RoleDisplay     Role = "display"
	RoleAdmin       Role = "admin"
)

// maxAliasLength is enforced here, not just via the input's maxlength
// attribute — that's only a UX nicety and is trivially bypassed by anyone
// posting to /join directly (curl, devtools, disabled JS). This is the
// actual enforcement.
const maxAliasLength = 20

// maxParticipants bounds unbounded growth from a scripted join-flood (each
// request with no session cookie gets a fresh one, so there's otherwise no
// natural limit) — generous enough that no real event ever approaches it,
// while capping the worst case for both memory and the O(N) rendering cost
// a broadcast does per subscriber (see broadcastAll).
const maxParticipants = 500

type joinRequest struct {
	sessionID string
	alias     string
	reply     chan error
}

type answerRequest struct {
	sessionID string
	choice    int
	reply     chan error
}

type snapshotRequest struct {
	role      Role
	sessionID string
	reply     chan string
}

type subscribeRequest struct {
	role      Role
	sessionID string
	ch        chan string
}

type unsubscribeRequest struct {
	role Role
	ch   chan string
}

type resultsRequest struct {
	reply chan []QuestionResult
}

type setPollRequest struct {
	poll  *Poll
	reply chan error
}

type setBaseURLRequest struct {
	baseURL string
}

// Hub owns PollState — and now Poll itself — exclusively. Every mutation is
// a message processed one at a time in Run's select loop. Nothing outside
// this file ever reads or writes them directly, so there is nothing to
// lock and no way for a reader to observe a half-applied change.
//
// Poll being mutable is new: it used to be loaded once at boot and never
// touched again, which is why every read site could safely assume it never
// changed underneath it. That's still true in spirit — SetPoll only ever
// succeeds while Phase is Waiting, enforced here, not just by hiding a
// button — but the assumption is now "frozen while running," not "frozen
// forever."
type Hub struct {
	poll      *Poll // nil until first configured
	adminBase string
	logger    *slog.Logger
	templates *template.Template

	// baseURL/joinURL/joinQRDataURI are resolved lazily now (see app.go's
	// resolveBaseURL) rather than fixed at construction, so they're
	// mutated via setBaseURLCh like everything else the hub owns — read
	// only from within Run's own goroutine, same as poll and state.
	displayURL    string // if set, fixes joinURL/joinQRDataURI permanently — see NewHub/setBaseURLCh
	baseURL       string
	joinURL       string
	joinQRDataURI string

	state PollState

	subscribers map[Role]map[chan string]string // ch -> sessionID

	joinCh       chan joinRequest
	answerCh     chan answerRequest
	startCh      chan chan error
	nextCh       chan chan error
	resetCh      chan chan error
	toggleQRCh   chan chan error
	setPollCh    chan setPollRequest
	setBaseURLCh chan setBaseURLRequest
	snapshotCh   chan snapshotRequest
	subscribeCh  chan subscribeRequest
	unsubCh      chan unsubscribeRequest
	resultsCh    chan resultsRequest
	eventTitleCh chan chan string

	timer *time.Timer
}

// NewHub takes poll as possibly nil — an unconfigured event is a normal,
// expected starting state now, not an error condition. adminBase is
// still fixed for the process lifetime (the secret-URL-as-bearer-token
// design doesn't change), but nothing about the poll's content is fixed
// anymore beyond "not while it's running."
//
// displayURL is an optional override for what participants see and scan
// (DISPLAY_URL — e.g. a short link like dub.co/fridayquiz), entirely
// separate from adminBase's secret-URL-as-bearer-token concern. When set,
// joinURL/joinQRDataURI are fixed here immediately and the lazy per-request
// inference in SetBaseURL never touches them again — see the setBaseURLCh
// case in Run.
func NewHub(poll *Poll, adminBase, displayURL string, templates *template.Template, logger *slog.Logger) *Hub {
	h := &Hub{
		poll:      poll,
		adminBase: adminBase,
		logger:    logger,
		templates: templates,
		state:     newPollState(),
		subscribers: map[Role]map[chan string]string{
			RoleParticipant: {},
			RoleDisplay:     {},
			RoleAdmin:       {},
		},
		joinCh:       make(chan joinRequest),
		answerCh:     make(chan answerRequest),
		startCh:      make(chan chan error),
		nextCh:       make(chan chan error),
		resetCh:      make(chan chan error),
		toggleQRCh:   make(chan chan error),
		setPollCh:    make(chan setPollRequest),
		setBaseURLCh: make(chan setBaseURLRequest),
		snapshotCh:   make(chan snapshotRequest),
		subscribeCh:  make(chan subscribeRequest),
		unsubCh:      make(chan unsubscribeRequest),
		resultsCh:    make(chan resultsRequest),
		eventTitleCh: make(chan chan string),
	}

	if displayURL != "" {
		h.displayURL = displayURL
		h.joinURL = displayURL
		if qr, err := qrDataURI(displayURL); err != nil {
			logger.Error("qr generation failed", "error", err)
		} else {
			h.joinQRDataURI = qr
		}
	}

	return h
}

// Run is the hub's only goroutine. Everything else in this package talks to
// it through the channels below and never touches h.state/h.poll directly.
func (h *Hub) Run(ctx context.Context) {
	var timerC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return

		case req := <-h.subscribeCh:
			h.subscribers[req.role][req.ch] = req.sessionID

		case req := <-h.unsubCh:
			delete(h.subscribers[req.role], req.ch)

		case req := <-h.joinCh:
			err := h.handleJoin(req.sessionID, req.alias)
			req.reply <- err
			if err == nil {
				h.broadcastAll()
			}

		case req := <-h.answerCh:
			err := h.handleAnswer(req.sessionID, req.choice)
			req.reply <- err
			if err == nil {
				h.broadcastAll()
			}

		case reply := <-h.startCh:
			reply <- h.handleStart()
			timerC = h.armTimer()
			h.broadcastAll()

		case reply := <-h.nextCh:
			reply <- h.handleNext()
			timerC = h.armTimer()
			h.broadcastAll()

		case reply := <-h.resetCh:
			// Reset only ever clears runtime state — participants,
			// answers, phase. Poll itself (event name, duration,
			// questions) is configuration the admin owns, not something
			// that happened during a run, so it survives untouched and
			// the same poll is ready to run again immediately.
			h.state.reset()
			reply <- nil
			timerC = nil
			h.broadcastAll()

		case reply := <-h.toggleQRCh:
			if h.state.Phase != PhaseQuestion {
				h.state.ShowQR = !h.state.ShowQR
				h.broadcastAll()
			}
			reply <- nil

		case req := <-h.setPollCh:
			if h.state.Phase != PhaseWaiting {
				req.reply <- fmt.Errorf("can't edit the poll while it's running")
			} else {
				h.poll = req.poll
				req.reply <- nil
				h.broadcastAll()
			}

		case req := <-h.setBaseURLCh:
			if h.displayURL == "" && req.baseURL != h.baseURL {
				h.baseURL = req.baseURL
				h.joinURL = req.baseURL + "/"
				qr, err := qrDataURI(h.joinURL)
				if err != nil {
					h.logger.Error("qr generation failed", "error", err)
				} else {
					h.joinQRDataURI = qr
				}
				h.broadcastAll()
			}

		case req := <-h.snapshotCh:
			req.reply <- h.render(req.role, req.sessionID)

		case req := <-h.resultsCh:
			out := make([]QuestionResult, len(h.state.Results))
			copy(out, h.state.Results)
			req.reply <- out

		case reply := <-h.eventTitleCh:
			reply <- h.currentEventTitle()

		case <-timerC:
			h.handleTimerExpiry()
			timerC = nil
			h.broadcastAll()
		}
	}
}

// --- public, synchronous API — safe to call from any goroutine ---

func (h *Hub) Join(sessionID, alias string) error {
	reply := make(chan error, 1)
	h.joinCh <- joinRequest{sessionID: sessionID, alias: alias, reply: reply}
	return <-reply
}

func (h *Hub) Answer(sessionID string, choice int) error {
	reply := make(chan error, 1)
	h.answerCh <- answerRequest{sessionID: sessionID, choice: choice, reply: reply}
	return <-reply
}

func (h *Hub) Start() error {
	reply := make(chan error, 1)
	h.startCh <- reply
	return <-reply
}

func (h *Hub) Next() error {
	reply := make(chan error, 1)
	h.nextCh <- reply
	return <-reply
}

func (h *Hub) Reset() error {
	reply := make(chan error, 1)
	h.resetCh <- reply
	return <-reply
}

// ToggleQR flips whether display is currently showing the join QR overlay
// instead of its normal phase content. It's independent of Phase on
// purpose: the host toggling it on to help a latecomer scan in, then off
// again, doesn't pause or otherwise affect the running question — the
// timer keeps counting down underneath exactly as if nothing happened,
// because nothing did; only what's currently rendered changed.
func (h *Hub) ToggleQR() error {
	reply := make(chan error, 1)
	h.toggleQRCh <- reply
	return <-reply
}

// SetPoll replaces the poll wholesale — used both for the very first setup
// (poll was nil) and for editing an existing one after Reset. It only
// succeeds while Phase is Waiting; the error path exists so a stray
// request can't slip through if it somehow arrives right at a phase
// transition, not just because the UI hides the form once running.
func (h *Hub) SetPoll(poll *Poll) error {
	reply := make(chan error, 1)
	h.setPollCh <- setPollRequest{poll: poll, reply: reply}
	return <-reply
}

// SetBaseURL is fire-and-forget on purpose (no reply channel) — it's
// called on admin page loads and reconnects (see handlers.go/sse.go —
// deliberately not on display, which is unauthenticated and would let
// anyone poison the cached domain via a spoofed Host header before a
// legitimate request ever arrives), and the caller doesn't need to wait
// for it; the hub applies it (or no-ops if unchanged) in its own time.
func (h *Hub) SetBaseURL(baseURL string) {
	h.setBaseURLCh <- setBaseURLRequest{baseURL: baseURL}
}

// Snapshot renders the current state for one viewer. Used both for the
// initial page load (so there's no flash of stale content before the SSE
// connection opens) and as the first message on every new SSE connection,
// so refresh / reconnect / a duplicate tab all resolve through the same
// code path.
func (h *Hub) Snapshot(role Role, sessionID string) string {
	reply := make(chan string, 1)
	h.snapshotCh <- snapshotRequest{role: role, sessionID: sessionID, reply: reply}
	return <-reply
}

// Results returns a copy of every closed question's final tally, in order —
// used by the admin "download results" action. A copy, not the live slice,
// so the caller can't observe (or corrupt) hub-owned state directly.
func (h *Hub) Results() []QuestionResult {
	reply := make(chan []QuestionResult, 1)
	h.resultsCh <- resultsRequest{reply: reply}
	return <-reply
}

// EventTitle is the one thing handlers.go needs outside of a rendered
// fragment — the browser tab title, set on every page load, independent
// of whatever role/phase content Snapshot returns.
func (h *Hub) EventTitle() string {
	reply := make(chan string, 1)
	h.eventTitleCh <- reply
	return <-reply
}

// Subscribe registers a channel that receives a fully-rendered HTML fragment
// every time state relevant to role changes. The channel is buffered so a
// momentarily slow reader doesn't block the hub; if it's still full when the
// next broadcast happens, that update is dropped for that subscriber — the
// next successful one carries the current (correct) state regardless, so
// nothing is ever left inconsistent, just briefly delayed.
func (h *Hub) Subscribe(role Role, sessionID string) chan string {
	ch := make(chan string, 4)
	h.subscribeCh <- subscribeRequest{role: role, sessionID: sessionID, ch: ch}
	return ch
}

func (h *Hub) Unsubscribe(role Role, ch chan string) {
	h.unsubCh <- unsubscribeRequest{role: role, ch: ch}
}

// --- internal command handlers — only ever called from Run's goroutine ---

func (h *Hub) currentEventTitle() string {
	if h.poll == nil {
		return "Pollinator"
	}
	return h.poll.EventTitle
}

// validateAlias covers what the server itself needs to enforce, as opposed
// to XSS — which html/template's automatic contextual escaping already
// handles for every place an alias is ever displayed — or SQL injection,
// which doesn't apply here since nothing in this app touches a database at
// all. What's left: a sane length limit (by rune count, so names in
// non-Latin scripts aren't cut short by counting bytes instead of
// characters), and rejecting control characters, which have no legitimate
// use in a display name and are a real log-injection / rendering-corruption
// vector if this were ever logged or embedded elsewhere verbatim.
func validateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("enter a name to join")
	}
	if utf8.RuneCountInString(alias) > maxAliasLength {
		return fmt.Errorf("name must be %d characters or fewer", maxAliasLength)
	}
	for _, r := range alias {
		if unicode.IsControl(r) {
			return fmt.Errorf("name contains invalid characters")
		}
	}
	return nil
}

func (h *Hub) handleJoin(sessionID, alias string) error {
	if err := validateAlias(alias); err != nil {
		return err
	}
	for otherID, p := range h.state.Participants {
		if otherID != sessionID && strings.EqualFold(p.Alias, alias) {
			return fmt.Errorf("%q is already taken — try another name", alias)
		}
	}
	if existing, ok := h.state.Participants[sessionID]; ok {
		existing.Alias = alias // honor a changed name on reconnect, rather than silently keeping the old one
		return nil
	}
	if len(h.state.Participants) >= maxParticipants {
		return fmt.Errorf("this poll is full")
	}
	h.state.Participants[sessionID] = &Participant{
		Alias:  alias,
		Choice: -1,
	}
	h.state.ParticipantOrder = append(h.state.ParticipantOrder, sessionID)
	return nil
}

func (h *Hub) handleAnswer(sessionID string, choice int) error {
	if h.state.Phase != PhaseQuestion {
		return fmt.Errorf("there's no active question")
	}
	if !time.Now().Before(h.state.QuestionEnd) {
		return fmt.Errorf("time's up for this question")
	}
	p, ok := h.state.Participants[sessionID]
	if !ok {
		return fmt.Errorf("join the poll first")
	}
	if p.Answered {
		return fmt.Errorf("you've already answered")
	}
	options := h.poll.Questions[h.state.QuestionIndex].Options
	if choice < 0 || choice >= len(options) {
		return fmt.Errorf("that's not a valid option")
	}

	p.Answered = true
	p.Choice = choice
	h.state.Answers[choice]++
	return nil
}

func (h *Hub) handleStart() error {
	if h.poll == nil {
		return fmt.Errorf("set up the poll before starting")
	}
	if h.state.Phase != PhaseWaiting {
		return nil
	}
	h.state.QuestionIndex = 0
	h.beginQuestion()
	return nil
}

func (h *Hub) handleNext() error {
	if h.state.Phase != PhaseResults {
		return nil
	}
	if h.isLastQuestion() {
		h.state.Phase = PhaseFinished
		h.state.ShowQR = false // no toggle exists on the finished screen to undo this otherwise
		return nil
	}
	h.state.QuestionIndex++
	h.beginQuestion()
	return nil
}

func (h *Hub) beginQuestion() {
	q := h.poll.Questions[h.state.QuestionIndex]
	h.state.Phase = PhaseQuestion
	h.state.QuestionEnd = time.Now().Add(time.Duration(h.poll.Duration) * time.Second)
	h.state.Answers = make([]int, len(q.Options))
	for _, p := range h.state.Participants {
		p.Answered = false
		p.Choice = -1
	}
}

func (h *Hub) handleTimerExpiry() {
	if h.state.Phase == PhaseQuestion {
		h.recordResult()
		h.state.Phase = PhaseResults
		h.state.ShowQR = false // fresh default every time, not just at Finish
	}
}

// recordResult permanently snapshots the question that's just closing.
// h.state.Answers only ever holds the *current* question's live tally and
// gets overwritten by the next beginQuestion, so this is the one place
// final, all-question results survive for the download-results feature.
func (h *Hub) recordResult() {
	q := h.poll.Questions[h.state.QuestionIndex]
	counts := make([]int, len(h.state.Answers))
	copy(counts, h.state.Answers)
	h.state.Results = append(h.state.Results, QuestionResult{
		Question: q.Question,
		Options:  q.Options,
		Counts:   counts,
	})
}

func (h *Hub) isLastQuestion() bool {
	return h.state.QuestionIndex+1 >= len(h.poll.Questions)
}

// armTimer (re)starts the question countdown and returns the channel Run
// should select on. The server owns this deadline authoritatively — what a
// client displays locally (see app.js) is just a courtesy; this timer is
// what actually closes the question.
func (h *Hub) armTimer() <-chan time.Time {
	if h.timer != nil {
		h.timer.Stop()
	}
	if h.state.Phase != PhaseQuestion {
		h.timer = nil
		return nil
	}
	d := time.Until(h.state.QuestionEnd)
	if d < 0 {
		d = 0
	}
	h.timer = time.NewTimer(d)
	return h.timer.C
}

// --- rendering ---

// participantNames returns joined aliases in join order — not map iteration
// order, which Go randomizes on purpose, and which would make a "live
// roster" visibly shuffle names around on every unrelated re-render.
func (h *Hub) participantNames() []string {
	names := make([]string, 0, len(h.state.ParticipantOrder))
	for _, sessionID := range h.state.ParticipantOrder {
		if p, ok := h.state.Participants[sessionID]; ok {
			names = append(names, p.Alias)
		}
	}
	return names
}

func (h *Hub) broadcastAll() {
	for role, subs := range h.subscribers {
		for ch, sessionID := range subs {
			payload := h.render(role, sessionID)
			select {
			case ch <- payload:
			default:
				// Slow subscriber; dropped, but the next successful send
				// (or the catch-up snapshot on reconnect) is always current.
			}
		}
	}
}

type resultRow struct {
	Option  string
	Count   int
	Percent int
}

// render always resolves to a "_waiting" template when h.poll is nil,
// since Phase can only ever be Waiting in that state — handleStart
// refuses to leave Waiting without a poll — so every other phase's
// templates can safely assume h.poll and h.poll.Questions exist without
// re-checking. Only the three "_waiting" templates (and their shared
// partials) need to handle a nil Poll gracefully.
func (h *Hub) render(role Role, sessionID string) string {
	name := string(role) + "_" + phaseName(h.state.Phase)

	if role == RoleDisplay && h.state.ShowQR && h.state.Phase != PhaseWaiting {
		name = "display_qr_overlay"
	}

	data := map[string]any{
		"Poll":        h.poll,
		"State":       h.state,
		"Participant": h.state.Participants[sessionID],
		"Results":     h.resultsView(),
		"AdminBase":   h.adminBase,
	}

	if h.poll != nil {
		data["EventTitle"] = h.poll.EventTitle

		if h.state.Phase == PhaseQuestion || h.state.Phase == PhaseResults {
			data["Question"] = h.poll.Questions[h.state.QuestionIndex]
			data["IsLastQuestion"] = h.isLastQuestion()
		}
	}
	if h.state.Phase == PhaseQuestion {
		data["SecondsLeft"] = int(time.Until(h.state.QuestionEnd).Seconds())
		data["EndUnixMilli"] = h.state.QuestionEnd.UnixMilli()
	}
	if role == RoleDisplay {
		data["JoinURL"] = displayJoinURL(h.joinURL)
		data["QRDataURI"] = template.URL(h.joinQRDataURI)
		data["ParticipantNames"] = h.participantNames()
	}

	var buf bytes.Buffer
	if err := h.templates.ExecuteTemplate(&buf, name, data); err != nil {
		h.logger.Error("render failed", "template", name, "error", err)
		return `<div class="p-8 text-red-400">Something went wrong rendering this view.</div>`
	}
	return buf.String()
}

func (h *Hub) resultsView() []resultRow {
	if h.state.Answers == nil || h.poll == nil {
		return nil
	}
	options := h.poll.Questions[h.state.QuestionIndex].Options

	total := 0
	for _, c := range h.state.Answers {
		total += c
	}

	rows := make([]resultRow, len(options))
	for i, opt := range options {
		count := h.state.Answers[i]
		percent := 0
		if total > 0 {
			percent = count * 100 / total
		}
		rows[i] = resultRow{Option: opt, Count: count, Percent: percent}
	}
	return rows
}

// displayJoinURL strips the scheme for the plain-text fallback shown next
// to the QR — cleaner to read without "https://" cluttering it, and
// nobody needs to type a scheme by hand anyway. The QR itself always
// encodes the full scheme-included URL (joinQRDataURI, generated
// separately from joinURL whenever it's set) — a phone camera needs a
// real scheme to recognize the contents as a link instead of falling back
// to treating it as plain text to search for.
func displayJoinURL(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	return url
}

func phaseName(p Phase) string {
	switch p {
	case PhaseQuestion:
		return "question"
	case PhaseResults:
		return "results"
	case PhaseFinished:
		return "finished"
	default:
		return "waiting"
	}
}
