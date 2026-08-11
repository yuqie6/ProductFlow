package turn

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const EventSchemaVersion = 1

const EventTextDelta = "text.delta"

var (
	ErrConflict                = errors.New("turn conflict")
	ErrIdempotencyConflict     = errors.New("turn idempotency conflict")
	ErrQuestionAlreadyAnswered = errors.New("turn question already answered")
	ErrQuestionExpired         = errors.New("turn question expired")
	ErrRequiredArtifactMissing = errors.New("turn required artifact missing")
)

type Status string

const (
	StatusQueued               Status = "queued"
	StatusRunning              Status = "running"
	StatusRequiresInput        Status = "requires_input"
	StatusAwaitingConfirmation Status = "awaiting_confirmation"
	StatusSucceeded            Status = "succeeded"
	StatusFailed               Status = "failed"
	StatusCancelRequested      Status = "cancel_requested"
	StatusCanceled             Status = "canceled"
	StatusUnknown              Status = "unknown"
)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID       string           `json:"id"`
	Header   string           `json:"header"`
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options,omitempty"`
}

// QuestionAnswer is a one-of: Option must be non-nil for a listed choice, or
// Text must be non-empty for free text.
type QuestionAnswer struct {
	Option *int   `json:"option,omitempty"`
	Text   string `json:"text,omitempty"`
}

func OptionAnswer(index int) QuestionAnswer { return QuestionAnswer{Option: &index} }
func TextAnswer(text string) QuestionAnswer { return QuestionAnswer{Text: text} }

type Artifact struct {
	Name   string          `json:"name"`
	Value  json.RawMessage `json:"value"`
	StepID string          `json:"step_id"`
}

type State struct {
	APIVersion string     `json:"api_version"`
	RunID      string     `json:"run_id"`
	TurnID     string     `json:"turn_id"`
	Status     Status     `json:"status"`
	Input      TurnInput  `json:"input"`
	Question   *Question  `json:"question,omitempty"`
	Artifact   *Artifact  `json:"artifact,omitempty"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Event struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         string          `json:"run_id"`
	TurnID        string          `json:"turn_id"`
	Sequence      int64           `json:"sequence"`
	CreatedAt     time.Time       `json:"created_at"`
	Kind          string          `json:"kind"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

type TextDeltaPayload struct {
	Delta     string `json:"delta"`
	StepID    string `json:"step_id"`
	AttemptID string `json:"attempt_id"`
}

type EventSink interface {
	AppendEvent(context.Context, Event) error
}

type EventStore interface {
	Events(context.Context, string, string, int64) ([]Event, error)
}

func (input TurnInput) canonicalJSON() ([]byte, error) { return json.Marshal(input) }
