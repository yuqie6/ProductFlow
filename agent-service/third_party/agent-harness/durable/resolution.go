package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type unresolvedOpaqueAttempt struct {
	stepID    string
	attemptID string
	prepared  json.RawMessage
}

func (s *sqliteStore) resolveUnknown(ctx context.Context, jobID string, resolution UnknownResolution, now time.Time) error {
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
	if jobStatus != JobUnknown {
		return fmt.Errorf("%w: job %s 状态为 %s,不是 unknown", ErrInvalidState, jobID, jobStatus)
	}

	current, err := selectUnresolvedOpaqueAttempt(ctx, tx, jobID, resolution.StepID, resolution.AttemptID)
	if err != nil {
		return err
	}
	switch resolution.Outcome {
	case ResolutionApplied:
		err = resolveUnknownApplied(ctx, tx, jobID, current, resolution, now)
	case ResolutionFailed:
		err = resolveUnknownFailed(ctx, tx, jobID, current, resolution, now)
	case ResolutionRetry:
		err = authorizeUnknownRetry(ctx, tx, jobID, current, resolution, now)
	default:
		err = fmt.Errorf("unknown resolution outcome 无效: %q", resolution.Outcome)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func selectUnresolvedOpaqueAttempt(ctx context.Context, tx *sql.Tx, jobID, stepID, attemptID string) (unresolvedOpaqueAttempt, error) {
	var current unresolvedOpaqueAttempt
	var prepared []byte
	var effect EffectClass
	err := tx.QueryRowContext(ctx, `SELECT
		s.id, s.effect, a.id, a.prepared_json
		FROM steps s
		JOIN attempts a ON a.step_id = s.id
		WHERE s.job_id = ? AND s.id = ? AND a.id = ? AND s.status = ? AND a.status = ?
		ORDER BY s.position, a.number DESC LIMIT 1`,
		jobID, stepID, attemptID, StepUnknown, AttemptUnknown,
	).Scan(&current.stepID, &effect, &current.attemptID, &prepared)
	if errors.Is(err, sql.ErrNoRows) {
		return current, fmt.Errorf("%w: job %s 的 step %s/attempt %s 不是待核对 unknown", ErrInvalidState, jobID, stepID, attemptID)
	}
	if err != nil {
		return current, err
	}
	if effect != EffectOpaque {
		return current, fmt.Errorf("%w: unknown attempt 的 effect 为 %s,不是 opaque", ErrInvalidState, effect)
	}
	current.prepared = cloneJSON(prepared)
	return current, nil
}

func resolveUnknownApplied(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	current unresolvedOpaqueAttempt,
	resolution UnknownResolution,
	now time.Time,
) error {
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
		status = ?, result_json = ?, error = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		AttemptSucceeded, []byte(resolution.Result), timestamp, timestamp, current.attemptID, AttemptUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE steps SET
		status = ?, result_json = ?, error = '', updated_at = ?
		WHERE id = ? AND status = ?`,
		StepSucceeded, []byte(resolution.Result), timestamp, current.stepID, StepUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	payload, _ := resolutionPayload(resolution, "")
	if err := appendEvent(ctx, tx, jobID, current.stepID, current.attemptID, "attempt.resolved", payload, now); err != nil {
		return err
	}

	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM steps WHERE job_id = ? AND status != ?`, jobID, StepSucceeded).Scan(&remaining); err != nil {
		return err
	}
	target := JobPending
	if remaining == 0 {
		var awaitContinuation bool
		if err := tx.QueryRowContext(ctx, `SELECT await_continuation FROM steps WHERE id = ?`, current.stepID).
			Scan(&awaitContinuation); err != nil {
			return err
		}
		target = JobSucceeded
		if awaitContinuation {
			target = JobAwaitingSteps
		}
	}
	updated, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		target, timestamp, jobID, JobUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	if target == JobSucceeded {
		return appendEvent(ctx, tx, jobID, "", "", "job.succeeded", json.RawMessage(`{"resolution":"manual"}`), now)
	}
	if target == JobAwaitingSteps {
		return appendEvent(ctx, tx, jobID, "", "", "job.awaiting_steps", json.RawMessage(`{"resolution":"manual"}`), now)
	}
	return appendEvent(ctx, tx, jobID, "", "", "job.resumed", payload, now)
}

func resolveUnknownFailed(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	current unresolvedOpaqueAttempt,
	resolution UnknownResolution,
	now time.Time,
) error {
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
		status = ?, error = ?, finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		AttemptFailed, resolution.Reason, timestamp, timestamp, current.attemptID, AttemptUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE steps SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`,
		StepFailed, resolution.Reason, timestamp, current.stepID, StepUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`,
		JobFailed, resolution.Reason, timestamp, jobID, JobUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	payload, _ := resolutionPayload(resolution, "")
	return appendEvent(ctx, tx, jobID, current.stepID, current.attemptID, "attempt.resolved", payload, now)
}

func authorizeUnknownRetry(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	current unresolvedOpaqueAttempt,
	resolution UnknownResolution,
	now time.Time,
) error {
	prepared, err := normalizeJSON(current.prepared)
	if err != nil {
		return fmt.Errorf("unknown attempt prepared payload: %w", err)
	}
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, result_json = NULL, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		StepPrepared, timestamp, current.stepID, StepUnknown)
	if err := requireSingleResolutionUpdate(updated, err); err != nil {
		return err
	}
	updated, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		JobPending, timestamp, jobID, JobUnknown)
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
	payload, _ := resolutionPayload(resolution, attemptID)
	if err := appendEvent(ctx, tx, jobID, current.stepID, current.attemptID, "attempt.retry_authorized", payload, now); err != nil {
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

func resolutionPayload(resolution UnknownResolution, newAttemptID string) (json.RawMessage, error) {
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

func requireSingleResolutionUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrInvalidState
	}
	return nil
}
