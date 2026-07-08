package internal

import "time"

// Phase is the poll lifecycle. Advances only forward, except Reset which
// returns to Waiting from Finished (or from anywhere, for the "start over"
// case).
type Phase int

const (
	PhaseWaiting Phase = iota
	PhaseQuestion
	PhaseResults
	PhaseFinished
)

// Participant is one joined attendee, keyed by session ID (see session.go).
type Participant struct {
	Alias    string
	Answered bool
	Choice   int
}

// QuestionResult is a permanent snapshot of one question's tally, recorded
// the moment its timer closes it (see hub.go's recordResult). Answers
// itself only ever holds the *current* question's live counts and gets
// wiped on every new question, so this is the one place final, all-question
// results survive for the download-results feature.
type QuestionResult struct {
	Question string
	Options  []string
	Counts   []int
}

// PollState is the single source of truth for the running event. It is
// mutated exclusively by the hub's command loop (see hub.go) — nothing else
// ever writes to it, so no locking is needed anywhere in this file.
type PollState struct {
	Phase            Phase
	QuestionIndex    int
	QuestionEnd      time.Time
	Participants     map[string]*Participant // keyed by session ID
	ParticipantOrder []string                // session IDs in join order — see hub.go's participantNames
	Answers          []int                   // live vote count per option, current question only
	Results          []QuestionResult        // finalized, one per closed question, in order
	ShowQR           bool                    // host-toggled, independent of Phase — see hub.go's ToggleQR
}

func newPollState() PollState {
	return PollState{
		Phase:        PhaseWaiting,
		Participants: make(map[string]*Participant),
	}
}

func (s *PollState) reset() {
	*s = newPollState()
}
