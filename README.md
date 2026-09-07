# Pollinator

A minimal, self-contained live polling platform — think Kahoot, without
accounts. Single Go binary, no database, ephemeral by default. Optionally
persists across restarts via a mounted volume.

## Features

- Single Go binary, no database, no accounts — ephemeral by default,
  optionally persistent via `POLL_VOLUME`
- Poll setup and editing through the admin UI — no env vars or redeploys
  to change questions
- Optional quiz mode — mark a correct answer per question, shown once
  results are in
- Import/export a poll as JSON, plus a standalone offline poll-builder tool
- Real-time join, answer, and results via server-sent events — no
  page refreshes
- Kahoot-style color-coded answer options, consistent from answer screen
  to results
- Synthesized countdown sound on `/display` — no licensed audio, no mute
  control needed
- QR code and short-link join flow, toggleable on `/display` for
  latecomers mid-poll
- Final results download as a zip — raw CSV plus a self-contained HTML
  recap page

## Pages

- `/` — participant join page
- `/display` — presentation screen (project this)
- `/admin/<token>` — the admin URL printed in the server logs at startup

## Setting up a poll

Configured through the admin UI, not environment variables:

- No poll configured yet → `/admin` shows a setup form directly.
- Poll exists → `/admin` shows "Start poll," with a secondary "Edit poll"
  link.
- Editing only works while the poll isn't running (fresh boot, after
  Reset, or from the Finished screen) — never mid-poll.
- Reset clears participants, answers, and progress. The poll itself stays
  configured, ready to run again.

`POLL_JSON` seeds the poll on first boot only — after that, the admin UI
(or a previously-saved `POLL_VOLUME` poll, which takes priority) is the
only way it changes.

A standalone tool for authoring `POLL_JSON` ahead of time lives in
`poll-builder/` and deploys separately (e.g. Cloudflare Pages).

## Admin capabilities

- Start the poll, advance questions, or end it early from the results
  screen (skips remaining questions)
- Toggle a QR overlay on `/display` for latecomers (disabled during an
  active question)
- Preview all questions in a side panel at any time
- Download final results once the poll ends — a zip with the raw CSV
  and a self-contained HTML recap page
- Import/Export JSON while editing — Export grabs whatever's in the
  form, including unsaved edits; Import replaces the form's contents
  after confirmation

`/display` plays a synthesized countdown tick (faster/higher in the last
5 seconds, a low gong at zero) — generated in-browser, display only, no
mute control (mute the tab or the laptop).

## Configuration

Env vars are deployment concerns only — never something the host running
the event needs to touch.

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | |
| `ADMIN_TOKEN` | random | Printed at startup if unset |
| `PUBLIC_URL` | inferred | Full base URL. Auto-detected on Railway via `RAILWAY_PUBLIC_DOMAIN`; otherwise inferred from the first request. Set explicitly if your setup doesn't forward host/proto correctly |
| `DISPLAY_URL` | *(unset)* | Override for what participants see/scan — e.g. a short link. Drives the QR + text fallback, takes priority over `PUBLIC_URL` |
| `POLL_JSON` | *(optional)* | One-time seed for first boot only |
| `POLL_VOLUME` | *(unset)* | Path to a writable volume — persists the poll across restarts |

### `POLL_JSON`

```json
{
  "title": "Friday All-Hands",
  "duration": 20,
  "questions": [
    {
      "question": "Favourite language?",
      "options": ["Go", "Rust", "Python"],
      "correctIndex": 0
    }
  ]
}
```

`title`/`duration` are optional (default "Untitled Event", 20s). At least
one question, 2-4 options each. `correctIndex` (0-based) is optional per
question — enables quiz mode for that question; omit it for plain polling.

### `POLL_VOLUME`

Directory on a mounted volume, not a specific file. Validated at boot —
fails to start if it's not a writable directory, rather than failing
silently. A saved poll here beats `POLL_JSON` on boot; if nothing's saved
yet and `POLL_JSON` is set, that seed is saved immediately.

**On Railway**: volumes are root-owned by default. Add
`RAILWAY_RUN_UID=0` alongside `POLL_VOLUME`, or the app fails to boot
with a "not writable" error.

## Deployment

### Pre-built image

Multi-arch (`linux/amd64` + `linux/arm64`), published to GHCR on every
tagged release:

```
docker run -p 8080:8080 ghcr.io/alphasecio/pollinator:latest
```

No `ADMIN_TOKEN` required — one's generated and logged at startup if you
don't set one. Pull `:1.0.0` to pin a version, or `:latest` for newest.

### Building it yourself

Single container. No volume required unless using `POLL_VOLUME`. Two
CDN-loaded JS libs (htmx + SSE extension, pinned with SRI hashes).
Dockerfile: Node stage compiles Tailwind, Go stage builds the binary,
final stage is `distroless/static`.

One real Go dependency (`skip2/go-qrcode`). `go.sum` isn't committed —
`go mod tidy` generates it during build, which needs network access.
