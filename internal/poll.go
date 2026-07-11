package internal

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const (
	defaultEventTitle      = "Untitled Event"
	defaultQuestionSeconds = 20

	minOptions = 2
	maxOptions = 4 // matches the interaction model: full-width tappable buttons on
	// a phone and at-a-glance result bars on a projector both degrade past this
	maxQuestions = 50 // generous headroom over any real live poll — a sanity bound,
	// not a real restriction, against an absurdly large submitted poll

	maxTitleLength    = 60
	maxQuestionLength = 200
	maxOptionLength   = 60
)

// Question and Poll are the one authoritative description of an event: its
// name, its pacing, and its questions. There are no correct answers here —
// this is polling, not trivia.
//
// Poll is mutable now, unlike the rest of this app's state — see hub.go's
// SetPoll — but only ever while the poll is in PhaseWaiting. Once a poll is
// running, it's exactly as frozen as it always was; nothing about the
// admin-editable setup changes that guarantee.
type Question struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

type Poll struct {
	EventTitle string     `json:"title"`
	Duration   int        `json:"duration"` // seconds, applies to every question
	Questions  []Question `json:"questions"`
}

// ParsePoll parses and validates poll JSON — used both for POLL_JSON at
// boot and for the admin setup/edit form's submission, which is why
// EventTitle/Duration get sensible defaults here rather than hard
// failures: a host filling out a form shouldn't be blocked by forgetting
// to type an event name.
func ParsePoll(raw string) (*Poll, error) {
	if raw == "" {
		return nil, fmt.Errorf("no poll data was submitted")
	}

	var p Poll
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("invalid poll data: %w", err)
	}

	if err := ValidatePoll(&p); err != nil {
		return nil, err
	}
	applyPollDefaults(&p)

	return &p, nil
}

// ValidatePoll checks the parts that matter regardless of source (an env
// var or the setup form), so there's exactly one place poll validation
// rules live rather than two copies that could quietly drift apart.
//
// Lengths are measured by rune count, not len()'s byte count — matches the
// same reasoning already applied to alias validation (see hub.go): byte
// length would silently penalize non-Latin scripts and emoji, which take
// more than one byte per character, far more harshly than equivalent-length
// English text.
func ValidatePoll(p *Poll) error {
	if utf8.RuneCountInString(p.EventTitle) > maxTitleLength {
		return fmt.Errorf("event name must be %d characters or fewer", maxTitleLength)
	}
	if len(p.Questions) == 0 {
		return fmt.Errorf("poll must contain at least one question")
	}
	if len(p.Questions) > maxQuestions {
		return fmt.Errorf("poll has more than %d questions", maxQuestions)
	}

	for i := range p.Questions {
		q := p.Questions[i]

		if q.Question == "" {
			return fmt.Errorf("question %d is missing its text", i+1)
		}
		if utf8.RuneCountInString(q.Question) > maxQuestionLength {
			return fmt.Errorf("question %d is too long (max %d characters)", i+1, maxQuestionLength)
		}
		if len(q.Options) < minOptions {
			return fmt.Errorf("question %d must have at least %d options", i+1, minOptions)
		}
		if len(q.Options) > maxOptions {
			return fmt.Errorf("question %d has more than %d options", i+1, maxOptions)
		}
		for _, opt := range q.Options {
			if opt == "" {
				return fmt.Errorf("question %d has an empty option", i+1)
			}
			if utf8.RuneCountInString(opt) > maxOptionLength {
				return fmt.Errorf("question %d has an option longer than %d characters", i+1, maxOptionLength)
			}
		}
	}

	return nil
}

func applyPollDefaults(p *Poll) {
	if p.EventTitle == "" {
		p.EventTitle = defaultEventTitle
	}
	if p.Duration <= 0 {
		p.Duration = defaultQuestionSeconds
	}
}
