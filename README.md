# Pollinator

A minimal, self-contained live polling platform — think Kahoot, without
accounts or persistence. Single Go binary, no database. Server-authoritative
state, ephemeral by design: restarting the container starts a fresh poll.

## Features

- Single Go binary, no database, no accounts, ephemeral by design
- Poll setup and editing entirely through the admin UI — no env vars or
  redeploys required to change questions
- Import/export a poll as JSON, plus a standalone offline poll-builder tool
- Real-time join, answer, and results via server-sent events — no polling,
  no page refreshes
- Kahoot-style color-coded answer options, carried through consistently
  from the answer screen to the results screen
- Synthesized countdown sound on `/display` — no licensed audio, no mute
  control needed
- QR code and short-link join flow, toggleable on `/display` for
  latecomers mid-poll
- CSV export of final results

## Pages

- `/` — participant join page
- `/display` — presentation screen (project this)
- `/admin/<token>` - the admin URL printed in the server logs at startup — host controls

## Setting up a poll

The poll itself (event name, question duration, questions and options) is
configured through the admin UI, not environment variables:

- On first boot with no poll configured, `/admin` shows a setup form
  directly.
- Once a poll exists, `/admin` shows the normal "Start poll" screen with a
  secondary "Edit poll" link for changing it before running.
- Editing is only ever available while the poll isn't running (fresh boot,
  or after clicking Reset) — never mid-poll.
- Reset clears participants, answers, and progress, but keeps the poll
  itself exactly as configured, ready to run again immediately.

`POLL_JSON` (see below) is an optional one-time seed for the very first
boot — convenient for a Railway template with a pre-baked poll — but it's
never consulted again after that; from then on the admin UI is the only
way the poll changes.

A standalone tool for authoring `POLL_JSON` ahead of time, independent of
any running container, lives in `poll-builder/` and deploys separately
(e.g. Cloudflare Pages) — see that folder.

## Admin capabilities

Start the poll, advance questions, toggle a QR overlay on `/display` for
latecomers (disabled during an active question, so it can't accidentally
hide one), preview all questions in a side panel at any point, and
download final results as CSV once the poll ends.

While editing a poll, "Import JSON" / "Export JSON" round-trip the form
to/from a `poll.json` file — Export downloads whatever's currently in the
form, including unsaved edits, as a safety net against a restart wiping
out anything that only ever lived in memory; Import loads one back in
(from the standalone builder, a previous export, or hand-written), after
a confirmation since it replaces the current form's contents.

`/display` plays a synthesized countdown tick during each question (quicker
and higher-pitched in the last 5 seconds, a low, long-decaying gong at
zero) — generated in-browser, not a licensed track, and only on the
projected screen, never on participants' own devices. No in-app mute;
muting the browser tab or the host laptop covers that.

## Configuration

Environment variables cover deployment concerns only — never something a
non-technical host running the event needs to touch.

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | |
| `ADMIN_TOKEN` | random | If unset, generated and printed at startup |
| `PUBLIC_URL` | inferred | Full base URL (scheme + host). Auto-detected on Railway via `RAILWAY_PUBLIC_DOMAIN`; otherwise inferred from the first request to `/display` if unset. Set explicitly if your setup doesn't forward the real host/proto correctly |
| `DISPLAY_URL` | *(unset)* | Optional override for what participants see and scan — e.g. a short link (`dub.sh/fridayquiz`) instead of the raw deployment URL. Drives both the QR code and the plain-text fallback shown under it, and takes priority over `PUBLIC_URL`/inferred if set. A scheme is added automatically if you leave it off |
| `POLL_JSON` | *(optional)* | One-time seed for first boot only — see below |

### `POLL_JSON`

```json
{
  "title": "Friday All-Hands",
  "duration": 20,
  "questions": [
    {
      "question": "Favourite language?",
      "options": ["Go", "Rust", "Python"]
    }
  ]
}
```

`title` and `duration` are optional (default to "Untitled Event" and 20
seconds); at least one question is required, each with 2-4 options. No
correct answers; this is polling, not trivia.

## Deployment

### Pre-built image (quickest way to run this)

A multi-arch image (`linux/amd64` + `linux/arm64` — including Apple
Silicon, no emulation needed) is published to GitHub Container Registry on
every tagged release:

```
docker run -p 8080:8080 ghcr.io/alphasecio/pollinator:latest
```

That's the whole setup — no `ADMIN_TOKEN` required, since one is
generated and printed to the container's logs on startup if you don't set
one yourself. Check the logs for the admin URL, or set `ADMIN_TOKEN`
explicitly up front if you want a known, memorable one — see
Configuration above for that and every other variable.

Pull a specific version (`:1.0.0`) to pin to it, or `:latest` for the
newest tagged release. You only need to build the Dockerfile yourself if
you're modifying the source.

### Building it yourself

Single container, no volumes, no external services besides two CDN-loaded
JS libraries (htmx and its SSE extension — pinned versions with SRI hashes,
see `base.html`). Dockerfile: Node stage compiles Tailwind, Go stage builds
the binary, final stage is `distroless/static`.

`go.mod` has one real dependency (`skip2/go-qrcode`, for server-side QR
generation), so `go.sum` isn't committed — your build's `go mod tidy` step
generates it, which needs network access during the build (standard on
Railway/Cloud Run).
