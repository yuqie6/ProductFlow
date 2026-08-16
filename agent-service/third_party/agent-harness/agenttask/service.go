package agenttask

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/durableagent"
	"github.com/yuqie6/agent-harness/internal/llm"
	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

type Turn = turnprotocol.State
type TurnStatus = turnprotocol.Status
type TurnEvent = turnprotocol.Event
type TextDeltaPayload = turnprotocol.TextDeltaPayload
type Question = turnprotocol.Question
type QuestionAnswer = turnprotocol.QuestionAnswer
type Artifact = turnprotocol.Artifact

const (
	EventTextDelta           = turnprotocol.EventTextDelta
	TurnQueued               = turnprotocol.StatusQueued
	TurnRunning              = turnprotocol.StatusRunning
	TurnRequiresInput        = turnprotocol.StatusRequiresInput
	TurnAwaitingConfirmation = turnprotocol.StatusAwaitingConfirmation
	TurnSucceeded            = turnprotocol.StatusSucceeded
	TurnFailed               = turnprotocol.StatusFailed
	TurnCancelRequested      = turnprotocol.StatusCancelRequested
	TurnCanceled             = turnprotocol.StatusCanceled
	TurnUnknown              = turnprotocol.StatusUnknown
)

var (
	ErrConflict                = turnprotocol.ErrConflict
	ErrIdempotencyConflict     = turnprotocol.ErrIdempotencyConflict
	ErrQuestionAlreadyAnswered = turnprotocol.ErrQuestionAlreadyAnswered
	ErrQuestionExpired         = turnprotocol.ErrQuestionExpired
	errEventSync               = errors.New("agenttask durable event sync failed")
)

func OptionAnswer(index int) QuestionAnswer { return turnprotocol.OptionAnswer(index) }
func TextAnswer(text string) QuestionAnswer { return turnprotocol.TextAnswer(text) }

type StartTurnRequest struct {
	RunID          string    `json:"run_id"`
	Input          TurnInput `json:"input"`
	IdempotencyKey string    `json:"idempotency_key"`
}

type ToolStepProjector func(durable.Job) []turnprotocol.ToolStep

type journalReader interface {
	Events(ctx context.Context, jobID string, afterSequence int64) ([]durable.Event, error)
	Task(ctx context.Context, jobID string) (durable.Job, error)
}

type ServiceConfig struct {
	Runner        Config
	ToolProjector ToolStepProjector
}

// Service is the asynchronous v1alpha1 control plane. Its metadata and event
// cursor live beside, but do not replace, the durable model/tool journal.
type Service struct {
	runner           *Runner
	journal          journalReader
	store            *controlStore
	requiredArtifact string
	toolProjector    ToolStepProjector
	ctx              context.Context
	cancel           context.CancelFunc

	mu      sync.Mutex
	workers map[string]context.CancelFunc
	closed  bool
	wg      sync.WaitGroup
}

var controlID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`)

func OpenService(config ServiceConfig) (*Service, error) {
	mode := ResponseMode(strings.ToLower(strings.TrimSpace(string(config.Runner.Provider.ResponseMode))))
	if mode == ResponseModeStoredBackground {
		return nil, errors.New("agenttask OpenService requires opaque response mode so every model Turn can publish token-level text.delta events")
	}
	store, err := openControlStore(config.Runner.Database)
	if err != nil {
		return nil, err
	}
	requiredArtifact := ""
	if config.Runner.RequiredArtifact != nil {
		requiredArtifact = strings.TrimSpace(config.Runner.RequiredArtifact.Name)
		if requiredArtifact == "" {
			requiredArtifact = WorkflowDraftToolName
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		store: store, requiredArtifact: requiredArtifact, toolProjector: config.ToolProjector,
		ctx: ctx, cancel: cancel, workers: make(map[string]context.CancelFunc),
	}
	runner, err := open(config.Runner, service.persistTextDelta)
	if err != nil {
		cancel()
		_ = store.db.Close()
		return nil, err
	}
	service.runner = runner
	service.journal = runner
	return service, nil
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	for _, cancel := range s.workers {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	var storeErr, runnerErr error
	if s.store != nil {
		storeErr = s.store.db.Close()
	}
	if s.runner != nil {
		runnerErr = s.runner.Close()
	}
	return errors.Join(storeErr, runnerErr)
}

func (s *Service) StartTurn(ctx context.Context, request StartTurnRequest) (Turn, error) {
	if err := s.validate(); err != nil {
		return Turn{}, err
	}
	runID, key, err := validateStartRequest(request)
	if err != nil {
		return Turn{}, err
	}
	turnID, err := newControlTurnID()
	if err != nil {
		return Turn{}, err
	}
	state, existing, err := s.store.insertTurn(ctx, runID, turnID, key, request.Input, time.Now().UTC())
	if err != nil {
		return Turn{}, err
	}
	if existing {
		// A process can stop after the queued row commits but before the local
		// worker is scheduled. An idempotent retry closes that gap without
		// bypassing resume_required question turns.
		s.scheduleNext(runID)
		return state, nil
	}
	s.scheduleNext(runID)
	return s.store.getTurn(ctx, runID, state.TurnID)
}

func (s *Service) ensureSubmitted(ctx context.Context, state Turn) error {
	if _, err := s.runner.Task(ctx, state.TurnID); err == nil {
		return nil
	} else if !errors.Is(err, durable.ErrNotFound) {
		return err
	}
	var history []llm.Message
	previousTurnID, found, err := s.store.latestSuccessfulTurnBefore(ctx, state.RunID, state.TurnID)
	if err != nil {
		return err
	}
	if found {
		history, err = s.runner.transcript(ctx, previousTurnID)
		if err != nil {
			return fmt.Errorf("load prior turn %s transcript: %w", previousTurnID, err)
		}
	}
	if _, err := s.runner.submitTurnWithHistory(ctx, state.TurnID, state.Input, history); err == nil {
		return nil
	} else if _, getErr := s.runner.Task(ctx, state.TurnID); getErr == nil {
		// A concurrent retry won the stable-ID insert.
		return nil
	} else {
		return errors.Join(err, getErr)
	}
}

func validateStartRequest(request StartTurnRequest) (string, string, error) {
	runID := strings.TrimSpace(request.RunID)
	key := strings.TrimSpace(request.IdempotencyKey)
	if !controlID.MatchString(runID) {
		return "", "", fmt.Errorf("invalid run_id %q", request.RunID)
	}
	if key == "" || len(key) > 200 {
		return "", "", errors.New("idempotency_key must be between 1 and 200 bytes")
	}
	if err := request.Input.Validate(); err != nil {
		return "", "", err
	}
	return runID, key, nil
}

func (s *Service) GetTurn(ctx context.Context, runID, turnID string) (Turn, error) {
	if err := s.validateIDs(runID, turnID); err != nil {
		return Turn{}, err
	}
	if err := s.syncDurableEvents(ctx, runID, turnID); err != nil && !errors.Is(err, durable.ErrNotFound) {
		return Turn{}, err
	}
	state, err := s.store.getTurn(ctx, runID, turnID)
	if err != nil {
		return Turn{}, err
	}
	if s.toolProjector == nil {
		return state, nil
	}
	job, err := s.runner.Task(ctx, turnID)
	if errors.Is(err, durable.ErrNotFound) {
		return state, nil
	}
	if err != nil {
		return Turn{}, err
	}
	state.ToolSteps = s.toolProjector(job)
	return state, nil
}

func (s *Service) CancelTurn(ctx context.Context, runID, turnID string) (Turn, error) {
	if err := s.validateIDs(runID, turnID); err != nil {
		return Turn{}, err
	}
	state, interrupt, err := s.store.requestCancel(ctx, runID, turnID, time.Now().UTC())
	if err != nil {
		return Turn{}, err
	}
	if interrupt {
		s.mu.Lock()
		cancel := s.workers[turnID]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		} else {
			// No process-local worker can establish where an orphaned durable
			// attempt stopped. Cancellation is therefore terminally unknown rather
			// than permanently stuck in cancel_requested or falsely canceled.
			message := "cancellation requested without an owning worker; durable outcome requires reconciliation"
			if syncErr := s.syncDurableEvents(ctx, runID, turnID); syncErr != nil {
				message = errors.Join(errors.New(message), syncErr).Error()
			}
			if err := s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusUnknown, "", message, nil, nil, time.Now().UTC()); err != nil {
				return Turn{}, err
			}
			s.scheduleNext(runID)
			return s.store.getTurn(ctx, runID, turnID)
		}
	} else {
		s.scheduleNext(runID)
	}
	return state, nil
}

func (s *Service) ResumeTurn(ctx context.Context, runID, turnID string) (Turn, error) {
	state, err := s.GetTurn(ctx, runID, turnID)
	if err != nil {
		return Turn{}, err
	}
	if state.Status == turnprotocol.StatusRunning {
		s.mu.Lock()
		localWorker := s.workers[turnID] != nil
		s.mu.Unlock()
		if localWorker {
			return state, nil
		}
		if err := s.store.requeueInterrupted(ctx, runID, turnID, time.Now().UTC()); err != nil {
			return Turn{}, err
		}
		state, err = s.GetTurn(ctx, runID, turnID)
		if err != nil {
			return Turn{}, err
		}
	}
	if state.Status != turnprotocol.StatusQueued {
		return Turn{}, fmt.Errorf("%w: cannot resume turn in %s", turnprotocol.ErrConflict, state.Status)
	}
	if err := s.store.requestResume(ctx, runID, turnID, time.Now().UTC()); err != nil {
		return state, err
	}
	s.scheduleNext(runID)
	return s.store.getTurn(ctx, runID, turnID)
}

func (s *Service) AnswerQuestion(
	ctx context.Context,
	runID, turnID, questionID string,
	answer QuestionAnswer,
) (Turn, error) {
	state, err := s.GetTurn(ctx, runID, turnID)
	if err != nil {
		return Turn{}, err
	}
	questionID = strings.TrimSpace(questionID)
	answerJSON, err := normalizeQuestionAnswer(answer)
	if err != nil {
		return Turn{}, err
	}
	record, recorded, err := s.store.answer(ctx, turnID, questionID)
	if err != nil {
		return Turn{}, err
	}
	if recorded && !bytes.Equal(record.Answer, answerJSON) {
		return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionAlreadyAnswered)
	}
	if recorded && (state.Status != turnprotocol.StatusRequiresInput || state.Question == nil || state.Question.ID != questionID) {
		return state, nil
	}
	if state.Status != turnprotocol.StatusRequiresInput || state.Question == nil || state.Question.ID != questionID {
		return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionExpired)
	}
	if answer.Option != nil && *answer.Option >= len(state.Question.Options) {
		return Turn{}, fmt.Errorf("question option index out of range: %d", *answer.Option)
	}
	pending, found, err := s.runner.PendingQuestion(ctx, turnID)
	if err != nil {
		if recovered, ok, recoveryErr := s.recoverRecordedQuestionAnswer(ctx, runID, turnID, questionID, answerJSON); ok || recoveryErr != nil {
			return recovered, recoveryErr
		}
		return Turn{}, err
	}
	if !found {
		if recovered, ok, recoveryErr := s.recoverRecordedQuestionAnswer(ctx, runID, turnID, questionID, answerJSON); ok || recoveryErr != nil {
			return recovered, recoveryErr
		}
		return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionExpired)
	}
	if pending.StepID != questionID {
		return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionExpired)
	}
	if recorded && (record.StepID != pending.Target.StepID || record.AttemptID != pending.Target.AttemptID) {
		return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionExpired)
	}
	if !recorded {
		if err := s.store.reserveAnswer(ctx, turnID, questionID, answerJSON, pending.Target.StepID, pending.Target.AttemptID, time.Now().UTC()); err != nil {
			reserveErr := err
			// A concurrent identical request may have won the reservation. The
			// persisted answer remains the authority across processes and restarts.
			record, recorded, err = s.store.answer(ctx, turnID, questionID)
			if err != nil {
				return Turn{}, errors.Join(reserveErr, err)
			}
			if !recorded {
				return Turn{}, reserveErr
			}
			if !bytes.Equal(record.Answer, answerJSON) {
				return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionAlreadyAnswered)
			}
			if record.StepID != pending.Target.StepID || record.AttemptID != pending.Target.AttemptID {
				return Turn{}, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionExpired)
			}
		}
	}
	decision := Decision{Actor: "turn-api", Reason: "application answered question " + questionID}
	if answer.Option != nil {
		_, err = s.runner.AnswerQuestion(ctx, turnID, pending.Target, *answer.Option, decision)
	} else {
		_, err = s.runner.AnswerQuestionText(ctx, turnID, pending.Target, answer.Text, decision)
	}
	if err != nil {
		// Another identical caller can resolve the durable step between our
		// PendingQuestion read and resolution transaction. Observe that commit
		// instead of leaking an internal durable conflict through the alpha API.
		if recovered, ok, recoveryErr := s.recoverRecordedQuestionAnswer(ctx, runID, turnID, questionID, answerJSON); ok || recoveryErr != nil {
			return recovered, recoveryErr
		}
		return Turn{}, err
	}
	return s.finalizeQuestionAnswer(ctx, runID, turnID, questionID, answerJSON)
}

func (s *Service) recoverRecordedQuestionAnswer(
	ctx context.Context, runID, turnID, questionID string, answerJSON json.RawMessage,
) (Turn, bool, error) {
	record, recorded, err := s.store.answer(ctx, turnID, questionID)
	if err != nil || !recorded {
		return Turn{}, false, err
	}
	if !bytes.Equal(record.Answer, answerJSON) {
		return Turn{}, true, errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionAlreadyAnswered)
	}
	state, err := s.store.getTurn(ctx, runID, turnID)
	if err != nil {
		return Turn{}, true, err
	}
	if state.Status != turnprotocol.StatusRequiresInput {
		return state, true, nil
	}
	if state.Question == nil || state.Question.ID != questionID {
		return state, true, nil
	}
	job, err := s.runner.Task(ctx, turnID)
	if err != nil {
		return Turn{}, true, err
	}
	if !successfulQuestionStep(job, record.StepID) {
		return Turn{}, false, nil
	}
	state, err = s.finalizeQuestionAnswer(ctx, runID, turnID, questionID, answerJSON)
	return state, true, err
}

func (s *Service) finalizeQuestionAnswer(
	ctx context.Context, runID, turnID, questionID string, answerJSON json.RawMessage,
) (Turn, error) {
	if err := s.syncDurableEvents(ctx, runID, turnID); err != nil {
		return Turn{}, err
	}
	if err := s.store.queueAfterAnswer(ctx, runID, turnID, questionID, answerJSON, time.Now().UTC()); err != nil {
		// Concurrent identical answers may both observe the durable resolution;
		// only one needs to perform the requires_input -> queued transition.
		state, getErr := s.store.getTurn(ctx, runID, turnID)
		if getErr == nil && state.Status != turnprotocol.StatusRequiresInput {
			return state, nil
		}
		return Turn{}, errors.Join(err, getErr)
	}
	return s.store.getTurn(ctx, runID, turnID)
}

func normalizeQuestionAnswer(answer QuestionAnswer) (json.RawMessage, error) {
	hasOption := answer.Option != nil
	hasText := strings.TrimSpace(answer.Text) != ""
	if hasOption == hasText {
		return nil, errors.New("question answer must contain exactly one of option or text")
	}
	if hasOption {
		if *answer.Option < 0 {
			return nil, fmt.Errorf("question option index out of range: %d", *answer.Option)
		}
		answer.Text = ""
	} else {
		answer.Text = strings.TrimSpace(answer.Text)
		if len(answer.Text) > 4000 {
			return nil, errors.New("question free text exceeds 4000 bytes")
		}
	}
	return json.Marshal(answer)
}

func successfulQuestionStep(job durable.Job, stepID string) bool {
	for _, step := range job.Steps {
		if step.ID == stepID {
			return step.Status == durable.StepSucceeded
		}
	}
	return false
}

func (s *Service) Events(ctx context.Context, runID, turnID string, afterSequence int64) ([]TurnEvent, error) {
	if err := s.validateIDs(runID, turnID); err != nil {
		return nil, err
	}
	if afterSequence < 0 {
		return nil, errors.New("event cursor cannot be negative")
	}
	if _, err := s.store.getTurn(ctx, runID, turnID); err != nil {
		return nil, err
	}
	if err := s.syncDurableEvents(ctx, runID, turnID); err != nil && !errors.Is(err, durable.ErrNotFound) {
		return nil, err
	}
	return s.store.events(ctx, runID, turnID, afterSequence)
}

func (s *Service) schedule(runID, turnID string) {
	s.mu.Lock()
	if s.closed || s.workers[turnID] != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.workers[turnID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.workers, turnID)
			s.mu.Unlock()
		}()
		s.drive(ctx, runID, turnID)
	}()
}

func (s *Service) drive(ctx context.Context, runID, turnID string) {
	claimed, err := s.store.claimTurn(ctx, runID, turnID, time.Now().UTC())
	if err != nil || !claimed {
		return
	}
	defer s.scheduleNext(runID)
	state, err := s.store.getTurn(ctx, runID, turnID)
	if err != nil {
		return
	}
	if err := s.ensureSubmitted(ctx, state); err != nil {
		if ctx.Err() != nil && s.isClosed() {
			return
		}
		_ = s.store.setOutcome(
			context.Background(), runID, turnID, turnprotocol.StatusFailed, "",
			"submit durable turn: "+err.Error(), nil, nil, time.Now().UTC(),
		)
		return
	}
	for {
		advanced, runErr := s.runner.AdvanceOne(ctx, turnID)
		if syncErr := s.syncDurableEvents(context.Background(), runID, turnID); syncErr != nil {
			s.finishDrive(runID, turnID, advanced, fmt.Errorf("%w: %v", errEventSync, syncErr))
			return
		}
		if runErr != nil || advanced.Terminal {
			s.finishDrive(runID, turnID, advanced, runErr)
			return
		}
		if !advanced.Progressed {
			select {
			case <-ctx.Done():
				s.finishDrive(runID, turnID, advanced, ctx.Err())
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

func (s *Service) persistTextDelta(ctx context.Context, delta durableagent.TextDelta) error {
	if delta.Delta == "" {
		return nil
	}
	runID, err := s.store.runIDForTurn(ctx, delta.JobID)
	if err != nil {
		return err
	}
	// The engine journals attempt.started before invoking the model. Mirror that
	// boundary before appending its first token so application sequence reflects
	// the actual durable causal order.
	if err := s.syncDurableEvents(ctx, runID, delta.JobID); err != nil {
		return err
	}
	payload, err := json.Marshal(TextDeltaPayload{
		Delta: delta.Delta, StepID: delta.StepID, AttemptID: delta.AttemptID,
	})
	if err != nil {
		return err
	}
	return s.store.appendEvent(ctx, runID, delta.JobID, turnprotocol.EventTextDelta, payload, time.Now().UTC())
}

func (s *Service) finishDrive(runID, turnID string, advanced AdvanceResult, runErr error) {
	ctx := context.Background()
	now := time.Now().UTC()
	state, err := s.store.getTurn(ctx, runID, turnID)
	if err != nil {
		return
	}
	if state.Status == turnprotocol.StatusRunning && errors.Is(runErr, context.Canceled) && s.isClosed() {
		// Process shutdown is not a business cancellation. Keep the persisted
		// control state adoptable by ResumeTurn; the durable attempt remains the
		// authority for replay, reconciliation, or unknown.
		return
	}
	if state.Status == turnprotocol.StatusCancelRequested {
		if errors.Is(runErr, errEventSync) || advanced.Job.Status == durable.JobUnknown || errors.Is(runErr, durable.ErrUnknown) {
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusUnknown, "", errorText(runErr), nil, nil, now)
			return
		}
		// A terminal durable result wins a best-effort cancellation race. For
		// non-terminal work, a running attempt means the effect outcome is
		// ambiguous after its context was interrupted and must not be called
		// canceled.
		if advanced.Job.Status != durable.JobSucceeded && advanced.Job.Status != durable.JobFailed {
			attempts, attemptsErr := s.runner.Attempts(ctx, turnID)
			if attemptsErr != nil || hasRunningAttempt(attempts) {
				message := "cancellation interrupted a running durable attempt"
				if attemptsErr != nil {
					message = errors.Join(errors.New(message), attemptsErr).Error()
				}
				_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusUnknown, "", message, nil, nil, now)
				return
			}
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusCanceled, "", "", nil, nil, now)
			return
		}
	}
	if errors.Is(runErr, durable.ErrRequiresAction) {
		pending, found, err := s.runner.PendingQuestion(ctx, turnID)
		if err == nil && found {
			question := publicTurnQuestion(pending)
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusRequiresInput, "", "", &question, nil, now)
			return
		}
		_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusUnknown, "", errorText(runErr), nil, nil, now)
		return
	}
	if runErr != nil {
		status := turnprotocol.StatusFailed
		if errors.Is(runErr, durable.ErrUnknown) || errors.Is(runErr, errEventSync) {
			status = turnprotocol.StatusUnknown
		}
		_ = s.store.setOutcome(ctx, runID, turnID, status, "", errorText(runErr), nil, nil, now)
		return
	}
	if advanced.Job.Status != durable.JobSucceeded {
		_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusUnknown, "", "durable task stopped without a terminal result", nil, nil, now)
		return
	}
	if s.requiredArtifact != "" {
		artifact, found, artifactErr := ArtifactFromJob(advanced.Job, s.requiredArtifact)
		if found {
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusAwaitingConfirmation, advanced.Output, "", nil, &artifact, now)
			return
		}
		if artifactErr != nil {
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusFailed, "", errorText(errors.Join(ErrRequiredArtifactMissing, artifactErr)), nil, nil, now)
			return
		}
		priorArtifact, priorArtifactErr := s.store.hasPriorArtifact(ctx, runID, turnID)
		if priorArtifactErr != nil {
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusUnknown, "", errorText(priorArtifactErr), nil, nil, now)
			return
		}
		if !priorArtifact {
			_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusFailed, "", errorText(ErrRequiredArtifactMissing), nil, nil, now)
			return
		}
		// Once a workflow draft exists, follow-up turns may answer questions without
		// creating a new revision. A successful artifact still takes precedence above.
	}
	_ = s.store.setOutcome(ctx, runID, turnID, turnprotocol.StatusSucceeded, advanced.Output, "", nil, nil, now)
}

func hasRunningAttempt(attempts []durable.Attempt) bool {
	for _, attempt := range attempts {
		if attempt.Status == durable.AttemptRunning {
			return true
		}
	}
	return false
}

func publicTurnQuestion(question PendingQuestion) Question {
	options := make([]turnprotocol.QuestionOption, len(question.Options))
	for index, option := range question.Options {
		options[index] = turnprotocol.QuestionOption{Label: option.Label, Description: option.Description}
	}
	return Question{ID: question.StepID, Header: question.Header, Question: question.Question, Options: options}
}

func (s *Service) syncDurableEvents(ctx context.Context, runID, turnID string) error {
	cursor, err := s.store.durableCursor(ctx, turnID)
	if err != nil {
		return err
	}
	events, err := s.journal.Events(ctx, turnID, cursor)
	if err != nil {
		return err
	}
	var projectedByStep map[string]turnprotocol.ToolStep
	if s.toolProjector != nil && hasPublicToolStepEvent(events) {
		job, jobErr := s.journal.Task(ctx, turnID)
		if jobErr != nil {
			return jobErr
		}
		projected := s.toolProjector(job)
		projectedByStep = make(map[string]turnprotocol.ToolStep, len(projected))
		for _, step := range projected {
			projectedByStep[step.StepID] = step
		}
	}
	for _, event := range events {
		if err := s.store.appendDurableEvent(ctx, runID, turnID, event, projectDurableToolStep(event, projectedByStep)); err != nil {
			return err
		}
	}
	return nil
}

func projectDurableToolStep(event durable.Event, projectedByStep map[string]turnprotocol.ToolStep) *turnprotocol.ToolStep {
	status, ok := publicToolStepStatus(event.Kind)
	if !ok {
		return nil
	}
	step, found := projectedByStep[event.StepID]
	if !found {
		return nil
	}
	step.Status = status
	return &step
}

func hasPublicToolStepEvent(events []durable.Event) bool {
	for _, event := range events {
		if _, ok := publicToolStepStatus(event.Kind); ok {
			return true
		}
	}
	return false
}

func publicToolStepStatus(kind string) (string, bool) {
	switch kind {
	case "step.pending", "step.prepared", "step.running", "attempt.prepared", "attempt.started", "attempt.recovered":
		return "running", true
	case "step.succeeded", "attempt.succeeded":
		return "succeeded", true
	case "step.failed", "attempt.failed", "attempt.prepare_failed":
		return "failed", true
	case "step.unknown", "step.requires_action", "attempt.unknown", "attempt.requires_action":
		return "unknown", true
	default:
		return "", false
	}
}

func (s *Service) scheduleNext(runID string) {
	turnID, found, err := s.store.nextRunnable(context.Background(), runID)
	if err == nil && found {
		s.schedule(runID, turnID)
	}
}

func (s *Service) validate() error {
	if s == nil || s.runner == nil || s.store == nil {
		return errors.New("agenttask service is nil")
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("agenttask service is closed")
	}
	return nil
}

func (s *Service) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Service) validateIDs(runID, turnID string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if !controlID.MatchString(runID) || !controlID.MatchString(turnID) {
		return errors.New("invalid run_id or turn_id")
	}
	return nil
}

func newControlTurnID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "turn-" + hex.EncodeToString(value), nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var _ turnprotocol.EventStore = (*Service)(nil)
