package internal

import (
	"bytes"
	"html/template"
	"time"
)

// recapOptionColors mirrors the same red/blue/yellow/green-by-position
// system already used live (see partials.frag.html's option_bg_class) —
// deliberately not a separate palette invented for this page. The color
// an option shows here is the same one it showed as a tappable button
// during the poll, so the downloaded recap visually continues what
// people actually saw, rather than introducing a new color language.
var recapOptionColors = [4]string{"#ef4444", "#3b82f6", "#eab308", "#22c55e"}

type recapOption struct {
	Option  string
	Count   int
	Percent int
	Color   string
	Lead    bool // most-voted option in this question — marked by weight, not by reordering, so position (and therefore color) stays meaningful
}

type recapQuestion struct {
	Num      int
	Question string
	Total    int
	Options  []recapOption
}

// buildRecapQuestions computes percentages and identifies each question's
// leading option, while deliberately preserving original option order —
// sorting by vote count would scramble which color maps to which option
// relative to what participants actually saw live.
func buildRecapQuestions(results []QuestionResult) []recapQuestion {
	out := make([]recapQuestion, len(results))
	for i, r := range results {
		total := 0
		for _, c := range r.Counts {
			total += c
		}

		leadIdx, leadCount := -1, -1
		for j, c := range r.Counts {
			if c > leadCount {
				leadIdx, leadCount = j, c
			}
		}

		opts := make([]recapOption, len(r.Options))
		for j, opt := range r.Options {
			percent := 0
			if total > 0 {
				percent = r.Counts[j] * 100 / total
			}
			opts[j] = recapOption{
				Option:  opt,
				Count:   r.Counts[j],
				Percent: percent,
				Color:   recapOptionColors[j%len(recapOptionColors)],
				Lead:    j == leadIdx,
			}
		}

		out[i] = recapQuestion{
			Num:      i + 1,
			Question: r.Question,
			Total:    total,
			Options:  opts,
		}
	}
	return out
}

// renderRecapHTML produces a fully self-contained HTML page — no
// dependency on anything else in this repo once downloaded, matching
// the standalone poll-builder tool's own "works with nothing else"
// philosophy. A completely separate template.Template instance, not
// merged into the app's shared one (see app.go's NewApp) — this page
// shares no partials with the rest of the UI and uses its own fonts and
// palette, so there's no reason to risk a name collision with anything
// else defined via {{define}}.
func renderRecapHTML(eventTitle string, results []QuestionResult) (string, error) {
	tmpl, err := template.New("recap").Parse(recapTemplateSrc)
	if err != nil {
		return "", err
	}

	data := map[string]any{
		"EventTitle":   eventTitle,
		"Questions":    buildRecapQuestions(results),
		"DownloadedAt": time.Now().Format("Jan 2, 2006"),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const recapTemplateSrc = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.EventTitle}} — Poll Results</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,400;9..144,600;9..144,700&family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  :root {
    --ink: #09090b;
    --border: #232c44;
    --text: #edf0f5;
    --muted: #8891a8;
    --muted-2: #5c6479;
    --bar-track: #1c2438;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    background: var(--ink);
    color: var(--text);
    font-family: 'IBM Plex Sans', sans-serif;
    -webkit-font-smoothing: antialiased;
    line-height: 1.5;
  }
  .wrap { max-width: 860px; margin: 0 auto; padding: 0 1.75rem; }
  header.hero { padding: 4.5rem 0 3rem; border-bottom: 1px solid var(--border); }
  h1.title {
    font-family: 'Fraunces', serif;
    font-optical-sizing: auto;
    font-weight: 600;
    font-size: clamp(2rem, 5vw, 3.1rem);
    line-height: 1.06;
    letter-spacing: -0.015em;
    margin: 0 0 1rem;
  }
  .hero-meta { font-family: 'IBM Plex Mono', monospace; font-size: 0.82rem; color: var(--muted); }
  .hero-meta strong { color: var(--text); font-weight: 500; }
  section.q { padding: 3.25rem 0; border-bottom: 1px solid var(--border); }
  section.q:last-of-type { border-bottom: none; }
  .q-head { display: grid; grid-template-columns: auto 1fr; gap: 1.1rem; align-items: start; margin-bottom: 1.9rem; }
  .q-num { font-family: 'IBM Plex Mono', monospace; font-size: 0.82rem; font-weight: 500; color: var(--muted-2); padding-top: 0.3rem; }
  h2.q-title { font-family: 'Fraunces', serif; font-weight: 600; font-size: clamp(1.25rem, 2.8vw, 1.55rem); letter-spacing: -0.005em; margin: 0; line-height: 1.3; }
  .bars { display: flex; flex-direction: column; gap: 1.05rem; }
  .bar-row .top { display: flex; justify-content: space-between; align-items: baseline; gap: 1rem; margin-bottom: 0.4rem; }
  .bar-row .opt { font-size: 0.98rem; color: var(--text); flex: 1; }
  .bar-row.lead .opt { font-weight: 600; }
  .bar-row .stat { font-family: 'IBM Plex Mono', monospace; font-size: 0.86rem; color: var(--muted); white-space: nowrap; flex-shrink: 0; }
  .bar-row.lead .stat { color: var(--text); font-weight: 600; }
  .track { height: 8px; border-radius: 999px; background: var(--bar-track); overflow: hidden; }
  .fill { height: 100%; border-radius: 999px; }
  .n-note { font-family: 'IBM Plex Mono', monospace; font-size: 0.74rem; color: var(--muted-2); margin-top: 1.4rem; }
</style>
</head>
<body>
<div class="wrap">
  <header class="hero">
    <h1 class="title">{{.EventTitle}}</h1>
    <div class="hero-meta">{{.DownloadedAt}} - <strong>{{len .Questions}}</strong> Questions</div>
  </header>

  {{range .Questions}}
  <section class="q">
    <div class="q-head">
      <div class="q-num">{{printf "%02d" .Num}}</div>
      <h2 class="q-title">{{.Question}}</h2>
    </div>
    <div class="bars">
      {{range .Options}}
      <div class="bar-row{{if .Lead}} lead{{end}}">
        <div class="top"><span class="opt">{{.Option}}</span><span class="stat">{{.Count}} · {{.Percent}}%</span></div>
        <div class="track"><div class="fill" style="width: {{.Percent}}%; background: {{.Color}};"></div></div>
      </div>
      {{end}}
    </div>
    <p class="n-note">n = {{.Total}}</p>
  </section>
  {{end}}
</div>
</body>
</html>
`
