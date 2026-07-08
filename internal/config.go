package internal

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
)

const defaultPort = "8080"

// Config is deployment/infrastructure configuration only — event title,
// question duration, and the poll's questions all live in Poll instead
// (see poll.go), editable through the admin UI rather than fixed for the
// container's lifetime via env vars. That split is deliberate: Port,
// AdminToken, BaseURL, and DisplayURL are things whoever deploys this sets
// once and a non-technical host running the actual event never needs to
// see; Poll is exactly the opposite.
type Config struct {
	Port       string
	AdminToken string
	BaseURL    string // "" means unresolved — see app.go's resolveBaseURL
	DisplayURL string // optional override for what participants see/scan — see hub.go
	Poll       *Poll  // nil if POLL_JSON wasn't set — admin sees the setup form instead
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:       getenv("PORT", defaultPort),
		AdminToken: os.Getenv("ADMIN_TOKEN"),
		BaseURL:    detectBaseURL(),
		DisplayURL: ensureScheme(strings.TrimRight(os.Getenv("DISPLAY_URL"), "/")),
	}

	if cfg.AdminToken == "" {
		cfg.AdminToken = randomToken()
	}

	// POLL_JSON is optional. If it's set, it seeds the poll exactly as
	// before; if not, admin sees the setup form on first load instead of
	// the process refusing to start. This is the one moment an env var
	// gets consulted at all — from here on, the admin UI is the only way
	// the poll ever changes, so there's no "which one wins" ambiguity to
	// worry about later.
	if raw := os.Getenv("POLL_JSON"); raw != "" {
		poll, err := ParsePoll(raw)
		if err != nil {
			return nil, err
		}
		cfg.Poll = poll
	}

	return cfg, nil
}

// detectBaseURL only resolves the cases knowable at boot: an explicit
// override, or Railway's own convention. If neither is set, it returns ""
// and App infers it from the first real request's Host header instead of
// guessing localhost — see app.go's resolveBaseURL.
func detectBaseURL() string {
	if v := os.Getenv("PUBLIC_URL"); v != "" {
		return ensureScheme(strings.TrimRight(v, "/"))
	}
	if v := os.Getenv("RAILWAY_PUBLIC_DOMAIN"); v != "" {
		return "https://" + v
	}
	return ""
}

// ensureScheme protects against exactly the mistake that's easy to make
// and silently breaks a QR code: setting PUBLIC_URL or DISPLAY_URL to a
// bare domain with no "https://" in front. A phone's camera needs a real
// scheme to recognize a QR's contents as a link at all — without one, it
// falls back to treating it as plain text and offers to search for it
// instead of opening it. RAILWAY_PUBLIC_DOMAIN doesn't need this since
// Railway's own convention is always a bare domain, handled separately
// above.
func ensureScheme(url string) string {
	if url == "" || strings.Contains(url, "://") {
		return url
	}
	return "https://" + url
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// randomToken is shared by the admin token and per-participant session IDs
// (see session.go) so there's exactly one place that decides what "random
// enough" means for this app.
func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
