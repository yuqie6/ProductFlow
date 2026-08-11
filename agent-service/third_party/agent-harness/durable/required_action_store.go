package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type unresolvedRequiredAttempt struct {
	stepID    string
	attemptID string
}

func (s *sqliteStore) resolveRequiredAction(
	ctx context.Context,
	jobID string,
	resolution RequiredActionResolution,
	prepared json.RawMessage,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var jobStatus JobStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&jobStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if jobStatus != JobRequiresAction {
		return fmt.Errorf("%w: job %s 状态为 %s,不是 requires_action", ErrInvalidState, jobID, jobStatus)
	}
	current, err := selectUnresolvedRequiredAttempt(ctx, tx, jobID, resolution.StepID, resolution.AttemptID)
	if err != nil {
		return err
	}
	switch resolution.Outcome {
	case ResolutionApplied:
		err = resolveRequiredApplied(ctx, tx, jobID, current, resolution, now)
	case ResolutionFailed:
		err = resolveRequiredFailed(ctx, tx, jobID, current, resolution, now)
	case ResolutionRetry:
		err = authorizeRequiredRetry(ctx, tx, jobID, current, resolution, prepared, now)
	default:
		err = fmt.Errorf("required action outcome 无效: %q", resolution.Outcome)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func selectUnresolvedRequiredAttempt(ctx context.Context, tx *sql.Tx, jobID, stepID, attemptID string) (unresolvedRequiredAttempt, error) {
	var current unresolvedRequiredAttempt
	err := tx.QueryRowContext(ctx, `SELECT s.id, a.id
		FROM steps s
		JOIN attempts a ON a.step_id = s.id
		WHERE s.job_id = ? AND s.id = ? AND a.id = ? AND s.status = ? AND a.status = ?
		ORDER BY s.position, a.number DESC LIMIT 1`,
		jobID, stepID, attemptID, StepRequiresAction, AttemptRequiresAction,
	).Scan(&current.stepID, &current.attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return current, fmt.Errorf("%w: job %s 的 step %s/attempt %s 不是待处理 requires_action", ErrInvalidState, jobID, stepID, attemptID)
	}
	return current, err
}

func resolveRequiredApplied(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	current unresolvedRequiredAttempt,
	resolution RequiredActionResolution,
	now time.Time,
) error {
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
		status = ?, result_json = ?, error = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		AttemptSucceeded, []byte(resolution.Result), timestamp, timestamp, current.attemptID, AttemptRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE steps SET
		status = ?, result_json = ?, error = '', updated_at = ?
		WHERE id = ? AND status = ?`,
		StepSucceeded, []byte(resolution.Result), timestamp, current.stepID, StepRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	payload, _ := requiredActionPayload(resolution, "")
	if err := appendEvent(ctx, tx, jobID, current.stepID, current.attemptID, "attempt.action_resolved", payload, now); err != nil {
		return err
	}
	return resumeAfterResolvedStep(ctx, tx, jobID, current.stepID, JobRequiresAction, payload, now)
}

func resolveRequiredFailed(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	current unresolvedRequiredAttempt,
	resolution RequiredActionResolution,
	now time.Time,
) error {
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
		status = ?, error = ?, finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		AttemptFailed, resolution.Reason, timestamp, timestamp, current.attemptID, AttemptRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE steps SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`,
		StepFailed, resolution.Reason, timestamp, current.stepID, StepRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`,
		JobFailed, resolution.Reason, timestamp, jobID, JobRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	payload, _ := requiredActionPayload(resolution, "")
	return appendEvent(ctx, tx, jobID, current.stepID, current.attemptID, "attempt.action_resolved", payload, now)
}

func authorizeRequiredRetry(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	current unresolvedRequiredAttempt,
	resolution RequiredActionResolution,
	prepared json.RawMessage,
	now time.Time,
) error {
	if len(prepared) == 0 {
		return errors.New("required action retry 缺少 prepared payload")
	}
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, result_json = NULL, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		StepPrepared, timestamp, current.stepID, StepRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		JobPending, timestamp, jobID, JobRequiresAction)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	attemptID, err := newID("attempt")
	if err != nil {
		return err
	}
	var number int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM attempts WHERE step_id = ?`, current.stepID).Scan(&number); err != nil {
		return err
	}
	key := fmt.Sprintf("%s/%s/%d", jobID, current.stepID, number)
	if _, err := tx.ExecContext(ctx, `INSERT INTO attempts(
		id, job_id, step_id, number, idempotency_key, status, prepared_json, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attemptID, jobID, current.stepID, number, key, AttemptPrepared, []byte(prepared), timestamp, timestamp); err != nil {
		return err
	}
	payload, _ := requiredActionPayload(resolution, attemptID)
	if err := appendEvent(ctx, tx, jobID, current.stepID, current.attemptID, "attempt.action_retry_authorized", payload, now); err != nil {
		return err
	}
	preparedPayload, _ := json.Marshal(map[string]any{
		"number": number, "idempotency_key": key, "authorized_by": resolution.Actor,
	})
	if err := appendEvent(ctx, tx, jobID, current.stepID, attemptID, "attempt.prepared", preparedPayload, now); err != nil {
		return err
	}
	return appendEvent(ctx, tx, jobID, "", "", "job.resumed", payload, now)
}

func resumeAfterResolvedStep(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	stepID string,
	from JobStatus,
	payload json.RawMessage,
	now time.Time,
) error {
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM steps WHERE job_id = ? AND status != ?`, jobID, StepSucceeded).Scan(&remaining); err != nil {
		return err
	}
	target := JobPending
	if remaining == 0 {
		var awaitContinuation bool
		if err := tx.QueryRowContext(ctx, `SELECT await_continuation FROM steps WHERE id = ?`, stepID).Scan(&awaitContinuation); err != nil {
			return err
		}
		target = JobSucceeded
		if awaitContinuation {
			target = JobAwaitingSteps
		}
	}
	updated, err := tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		target, now.UnixNano(), jobID, from)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	switch target {
	case JobSucceeded:
		return appendEvent(ctx, tx, jobID, "", "", "job.succeeded", json.RawMessage(`{"resolution":"manual"}`), now)
	case JobAwaitingSteps:
		return appendEvent(ctx, tx, jobID, "", "", "job.awaiting_steps", json.RawMessage(`{"resolution":"manual"}`), now)
	default:
		return appendEvent(ctx, tx, jobID, "", "", "job.resumed", payload, now)
	}
}

func requiredActionPayload(resolution RequiredActionResolution, newAttemptID string) (json.RawMessage, error) {
	payload := map[string]any{
		"step_id":    resolution.StepID,
		"attempt_id": resolution.AttemptID,
		"outcome":    resolution.Outcome,
		"actor":      resolution.Actor,
		"reason":     resolution.Reason,
	}
	if len(resolution.Result) > 0 {
		payload["result"] = resolution.Result
	}
	if newAttemptID != "" {
		payload["new_attempt_id"] = newAttemptID
	}
	return json.Marshal(payload)
}
