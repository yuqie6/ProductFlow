package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/durableagent"
	questionpkg "github.com/yuqie6/agent-harness/internal/question"
)

type Decision struct {
	Actor  string
	Reason string
}

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type PendingQuestion struct {
	Target   durable.ResolutionTarget `json:"target"`
	StepID   string                   `json:"step_id"`
	Header   string                   `json:"header"`
	Question string                   `json:"question"`
	Options  []Option                 `json:"options"`
}

type PendingEditReview struct {
	Target         durable.ResolutionTarget `json:"target"`
	StepID         string                   `json:"step_id"`
	Path           string                   `json:"path"`
	ExpectedSHA256 string                   `json:"expected_sha256"`
	OldText        string                   `json:"old_text"`
	NewText        string                   `json:"new_text"`
	Detail         string                   `json:"detail,omitempty"`
}

func (r *Runner) PendingQuestion(ctx context.Context, jobID string) (PendingQuestion, bool, error) {
	if err := r.validate(); err != nil {
		return PendingQuestion{}, false, err
	}
	job, err := r.inner.Task(ctx, jobID)
	if err != nil {
		return PendingQuestion{}, false, err
	}
	question, found, err := QuestionFromJob(job)
	if err != nil || !found {
		return PendingQuestion{}, found, err
	}
	target, err := r.inner.PendingResolution(ctx, jobID)
	if err != nil {
		return PendingQuestion{}, false, err
	}
	if target.StepID != question.StepID {
		return PendingQuestion{}, false, fmt.Errorf("%w: question resolution target changed", durable.ErrConflict)
	}
	question.Target = target
	return question, true, nil
}

func QuestionFromJob(job durable.Job) (PendingQuestion, bool, error) {
	question, found, err := durableagent.QuestionFromJob(job)
	if err != nil || !found {
		return PendingQuestion{}, found, err
	}
	options := make([]Option, len(question.Options))
	for index, option := range question.Options {
		options[index] = Option{Label: option.Label, Description: option.Description}
	}
	return PendingQuestion{
		Target: durable.ResolutionTarget{StepID: question.StepID}, StepID: question.StepID,
		Header: question.Header, Question: question.Question, Options: options,
	}, true, nil
}

func (q PendingQuestion) EncodeAnswer(option int) (json.RawMessage, error) {
	if option < 0 || option >= len(q.Options) {
		return nil, fmt.Errorf("question option index out of range: %d", option)
	}
	return json.Marshal(struct {
		Option int    `json:"option"`
		Label  string `json:"label"`
	}{Option: option, Label: q.Options[option].Label})
}

func (q PendingQuestion) EncodeFreeTextAnswer(text string) (json.RawMessage, error) {
	question := durableagent.PendingQuestion{
		StepID: q.StepID, Header: q.Header, Question: q.Question,
		Options: make([]questionpkg.Option, len(q.Options)),
	}
	for index, option := range q.Options {
		question.Options[index] = questionpkg.Option{Label: option.Label, Description: option.Description}
	}
	return question.EncodeFreeTextAnswer(text)
}

func (r *Runner) AnswerQuestion(
	ctx context.Context,
	jobID string,
	target durable.ResolutionTarget,
	option int,
	decision Decision,
) (durable.Job, error) {
	question, found, err := r.PendingQuestion(ctx, jobID)
	if err != nil {
		return durable.Job{}, err
	}
	if !found {
		return durable.Job{}, errors.New("agenttask has no pending question")
	}
	if question.Target != target {
		return durable.Job{}, fmt.Errorf("%w: stale question resolution target", durable.ErrConflict)
	}
	if option < 0 || option >= len(question.Options) {
		return durable.Job{}, fmt.Errorf("question option index out of range: %d", option)
	}
	answer, err := question.EncodeAnswer(option)
	if err != nil {
		return durable.Job{}, err
	}
	decision = normalizeDecision(decision, fmt.Sprintf("selected question option %d", option+1))
	return r.inner.ResolveRequiredAction(ctx, jobID, durable.RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID, Outcome: durable.ResolutionApplied,
		Actor: decision.Actor, Reason: decision.Reason, Result: answer,
	})
}

func (r *Runner) AnswerQuestionText(
	ctx context.Context,
	jobID string,
	target durable.ResolutionTarget,
	text string,
	decision Decision,
) (durable.Job, error) {
	question, found, err := r.PendingQuestion(ctx, jobID)
	if err != nil {
		return durable.Job{}, err
	}
	if !found {
		return durable.Job{}, errors.New("agenttask has no pending question")
	}
	if question.Target != target {
		return durable.Job{}, fmt.Errorf("%w: stale question resolution target", durable.ErrConflict)
	}
	answer, err := question.EncodeFreeTextAnswer(text)
	if err != nil {
		return durable.Job{}, err
	}
	decision = normalizeDecision(decision, "answered question with free text")
	return r.inner.ResolveRequiredAction(ctx, jobID, durable.RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID, Outcome: durable.ResolutionApplied,
		Actor: decision.Actor, Reason: decision.Reason, Result: answer,
	})
}

func (r *Runner) PendingEditReview(ctx context.Context, jobID string) (PendingEditReview, bool, error) {
	if err := r.validate(); err != nil {
		return PendingEditReview{}, false, err
	}
	job, err := r.inner.Task(ctx, jobID)
	if err != nil {
		return PendingEditReview{}, false, err
	}
	review, found, err := EditReviewFromJob(job)
	if err != nil || !found {
		return PendingEditReview{}, found, err
	}
	target, err := r.inner.PendingResolution(ctx, jobID)
	if err != nil {
		return PendingEditReview{}, false, err
	}
	if target.StepID != review.StepID {
		return PendingEditReview{}, false, fmt.Errorf("%w: edit review resolution target changed", durable.ErrConflict)
	}
	review.Target = target
	return review, true, nil
}

func EditReviewFromJob(job durable.Job) (PendingEditReview, bool, error) {
	review, found, err := durableagent.EditReviewFromJob(job)
	if err != nil || !found {
		return PendingEditReview{}, found, err
	}
	return PendingEditReview{
		Target: durable.ResolutionTarget{StepID: review.StepID}, StepID: review.StepID,
		Path: review.Path, ExpectedSHA256: review.ExpectedSHA256,
		OldText: review.OldText, NewText: review.NewText, Detail: review.Detail,
	}, true, nil
}

func (r *Runner) ResolveEditReview(
	ctx context.Context,
	jobID string,
	target durable.ResolutionTarget,
	approve bool,
	decision Decision,
) (durable.Job, error) {
	review, found, err := r.PendingEditReview(ctx, jobID)
	if err != nil {
		return durable.Job{}, err
	}
	if !found {
		return durable.Job{}, errors.New("agenttask has no pending edit review")
	}
	if review.Target != target {
		return durable.Job{}, fmt.Errorf("%w: stale edit review resolution target", durable.ErrConflict)
	}
	outcome := durable.ResolutionFailed
	var result json.RawMessage
	fallback := "rejected edit " + review.Path
	if approve {
		outcome = durable.ResolutionApplied
		result = json.RawMessage(`{"approved":true}`)
		fallback = "approved edit " + review.Path
	}
	decision = normalizeDecision(decision, fallback)
	return r.inner.ResolveRequiredAction(ctx, jobID, durable.RequiredActionResolution{
		StepID: target.StepID, AttemptID: target.AttemptID, Outcome: outcome,
		Actor: decision.Actor, Reason: decision.Reason, Result: result,
	})
}

func normalizeDecision(decision Decision, fallback string) Decision {
	decision.Actor = strings.TrimSpace(decision.Actor)
	decision.Reason = strings.TrimSpace(decision.Reason)
	if decision.Actor == "" {
		decision.Actor = "application"
	}
	if decision.Reason == "" {
		decision.Reason = fallback
	}
	return decision
}
