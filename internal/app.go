package internal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/alphasecio/pollinator/web"
)

var templateFuncs = template.FuncMap{
	"add1": func(i int) int { return i + 1 },
	"mod":  func(i, m int) int { return i % m },
	// toJSONBase64 seeds the admin edit form with the current poll's data.
	// Base64, not raw JSON, specifically so a question or option
	// containing something like "</script>" can never prematurely close
	// the embedding tag — sidesteps that whole class of escaping mistake
	// rather than trying to get manual escaping exactly right.
	"toJSONBase64": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(b), nil
	},
}

type App struct {
	cfg       *Config
	logger    *slog.Logger
	templates *template.Template
	hub       *Hub
	mux       *http.ServeMux
	assetVer  string // busts stale cached CSS on every deploy — see routes()/renderBase

	baseURLMu sync.Mutex
	baseURL   string // "" until resolved; see resolveBaseURL
}

func NewApp(cfg *Config, logger *slog.Logger) (*App, error) {
	tmpl, err := template.New("pollinator").Funcs(templateFuncs).ParseFS(
		web.Templates,
		"templates/*.html",
		"templates/fragments/*.html",
	)
	if err != nil {
		return nil, err
	}

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, err
	}

	adminBase := "/admin/" + cfg.AdminToken
	hub := NewHub(cfg.Poll, adminBase, cfg.DisplayURL, tmpl, logger)
	go hub.Run(context.Background())

	a := &App{
		cfg:       cfg,
		logger:    logger,
		templates: tmpl,
		hub:       hub,
		mux:       http.NewServeMux(),
		assetVer:  strconv.FormatInt(time.Now().Unix(), 10),
		baseURL:   cfg.BaseURL, // may be "" — resolved lazily from the first request if so
	}
	a.routes(staticFS, adminBase)

	return a, nil
}

func (a *App) Run() error {
	return http.ListenAndServe(":"+a.cfg.Port, a.mux)
}

func (a *App) AdminURL() string {
	return "/admin/" + a.cfg.AdminToken
}

// resolveBaseURL returns the known base URL, inferring it from the current
// request's Host header the first time nothing was already known from an
// explicit env var. An explicit value is never overridden by a request —
// this only ever fills in the gap when nothing was configured at boot.
func (a *App) resolveBaseURL(r *http.Request) string {
	a.baseURLMu.Lock()
	defer a.baseURLMu.Unlock()

	if a.baseURL != "" {
		return a.baseURL
	}

	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	a.baseURL = scheme + "://" + r.Host
	return a.baseURL
}

func (a *App) routes(staticFS fs.FS, adminBase string) {
	a.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	a.mux.HandleFunc("GET /{$}", a.indexPage)
	a.mux.HandleFunc("POST /join", a.joinAction)
	a.mux.HandleFunc("POST /answer", a.answerAction)
	a.mux.HandleFunc("GET /events", a.serveEvents(RoleParticipant))

	a.mux.HandleFunc("GET /display", a.displayPage)
	a.mux.HandleFunc("GET /display/events", a.serveEvents(RoleDisplay))

	a.mux.HandleFunc("GET "+adminBase, a.adminPage)
	a.mux.HandleFunc("GET "+adminBase+"/events", a.serveEvents(RoleAdmin))
	a.mux.HandleFunc("GET "+adminBase+"/download", a.downloadResultsAction)
	a.mux.HandleFunc("POST "+adminBase+"/poll", a.savePollAction)
	a.mux.HandleFunc("POST "+adminBase+"/start", a.adminAction(a.hub.Start))
	a.mux.HandleFunc("POST "+adminBase+"/next", a.adminAction(a.hub.Next))
	a.mux.HandleFunc("POST "+adminBase+"/reset", a.adminAction(a.hub.Reset))
	a.mux.HandleFunc("POST "+adminBase+"/qr", a.adminAction(a.hub.ToggleQR))
}
