package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxStepBatch = 100
	maxJobSteps  = 500
)

type normalizedStep struct {
	id                string
	tool              Tool
	input             json.RawMessage
	index             int
	awaitContinuation bool
}

func normalizeStepSpecs(specs []StepSpec, registered map[string]Tool) ([]normalizedStep, error) {
	normalized := make([]normalizedStep, len(specs))
	seen := make(map[string]bool, len(specs))
	for index, step := range specs {
		tool := registered[strings.TrimSpace(step.Tool)]
		if tool == nil {
			return nil, fmt.Errorf("步骤 %d 使用未注册工具 %q", index+1, step.Tool)
		}
		input, err := normalizeJSON(step.Input)
		if err != nil {
			return nil, fmt.Errorf("步骤 %d input: %w", index+1, err)
		}
		id := strings.TrimSpace(step.ID)
		if id == "" {
			id, err = newID("step")
			if err != nil {
				return nil, err
			}
		}
		if err := validateID(id); err != nil {
			return nil, fmt.Errorf("步骤 %d ID: %w", index+1, err)
		}
		if seen[id] {
			return nil, fmt.Errorf("步骤 ID 重复: %s", id)
		}
		seen[id] = true
		normalized[index] = normalizedStep{
			id: id, tool: tool, input: input, index: index, awaitContinuation: step.AwaitContinuation,
		}
	}
	return normalized, nil
}

// AppendSteps atomically resumes an awaiting job by appending steps after the
// expected tail. The tail precondition makes a retried orchestrator append at
// most once.
func (e *Engine) AppendSteps(ctx context.Context, jobID, afterStepID string, specs []StepSpec) (Job, error) {
	if len(specs) == 0 || len(specs) > maxStepBatch {
		return Job{}, fmt.Errorf("每次必须追加 1 到 %d 个 durable 步骤", maxStepBatch)
	}
	jobID = strings.TrimSpace(jobID)
	afterStepID = strings.TrimSpace(afterStepID)
	if err := validateID(jobID); err != nil {
		return Job{}, fmt.Errorf("job ID: %w", err)
	}
	if err := validateID(afterStepID); err != nil {
		return Job{}, fmt.Errorf("after step ID: %w", err)
	}
	normalized, err := normalizeStepSpecs(specs, e.tools)
	if err != nil {
		return Job{}, err
	}
	if err := e.store.appendSteps(ctx, jobID, afterStepID, normalized, time.Now().UTC()); err != nil {
		return Job{}, err
	}
	return e.store.getJob(ctx, jobID)
}

// Complete marks an awaiting job complete if afterStepID is still its tail.
func (e *Engine) Complete(ctx context.Context, jobID, afterStepID string) (Job, error) {
	return e.finishContinuation(ctx, jobID, afterStepID, JobSucceeded, "")
}

// Fail marks an awaiting job failed without manufacturing a tool attempt.
func (e *Engine) Fail(ctx context.Context, jobID, afterStepID, reason string) (Job, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 2000 {
		return Job{}, errors.New("durable continuation 失败原因不能为空且不能超过 2000 字符")
	}
	return e.finishContinuation(ctx, jobID, afterStepID, JobFailed, reason)
}

func (e *Engine) finishContinuation(ctx context.Context, jobID, afterStepID string, status JobStatus, reason string) (Job, error) {
	jobID = strings.TrimSpace(jobID)
	afterStepID = strings.TrimSpace(afterStepID)
	if err := validateID(jobID); err != nil {
		return Job{}, fmt.Errorf("job ID: %w", err)
	}
	if err := validateID(afterStepID); err != nil {
		return Job{}, fmt.Errorf("after step ID: %w", err)
	}
	if err := e.store.finishContinuation(ctx, jobID, afterStepID, status, reason, time.Now().UTC()); err != nil {
		return Job{}, err
	}
	return e.store.getJob(ctx, jobID)
}

func (s *sqliteStore) appendSteps(
	ctx context.Context,
	jobID string,
	afterStepID string,
	steps []normalizedStep,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status, tailID, tailPosition, err := continuationTail(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if status != JobAwaitingSteps || tailID != afterStepID {
		return fmt.Errorf("%w: job %s 状态为 %s,tail=%s", ErrInvalidState, jobID, status, tailID)
	}
	if tailPosition+1+len(steps) > maxJobSteps {
		return fmt.Errorf("durable job 步骤总数不能超过 %d", maxJobSteps)
	}
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE jobs SET
		status = ?, error = '', lease_owner = '', lease_expires_at = NULL, updated_at = ?
		WHERE id = ? AND status = ?`, JobPending, timestamp, jobID, JobAwaitingSteps)
	if err := requireSingleContinuationUpdate(updated, err); err != nil {
		return err
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, `INSERT INTO steps(
			id, job_id, position, tool, effect, input_json, status, await_continuation, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			step.id, jobID, tailPosition+1+step.index, step.tool.Name(), step.tool.Effect(), []byte(step.input),
			StepPending, step.awaitContinuation, timestamp, timestamp,
		); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]any{"after_step_id": afterStepID, "step_count": len(steps)})
	if err := appendEvent(ctx, tx, jobID, "", "", "job.steps_appended", payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) finishContinuation(
	ctx context.Context,
	jobID string,
	afterStepID string,
	status JobStatus,
	reason string,
	now time.Time,
) error {
	if status != JobSucceeded && status != JobFailed {
		return fmt.Errorf("无效 continuation 终态: %s", status)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, tailID, _, err := continuationTail(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if current != JobAwaitingSteps || tailID != afterStepID {
		return fmt.Errorf("%w: job %s 状态为 %s,tail=%s", ErrInvalidState, jobID, current, tailID)
	}
	updated, err := tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = ?, updated_at = ?
		WHERE id = ? AND status = ?`, status, reason, now.UnixNano(), jobID, JobAwaitingSteps)
	if err := requireSingleContinuationUpdate(updated, err); err != nil {
		return err
	}
	kind := "job.succeeded"
	if status == JobFailed {
		kind = "job.failed"
	}
	payload, _ := json.Marshal(map[string]string{"after_step_id": afterStepID, "reason": reason})
	if err := appendEvent(ctx, tx, jobID, "", "", kind, payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func continuationTail(ctx context.Context, tx *sql.Tx, jobID string) (JobStatus, string, int, error) {
	var status JobStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", 0, ErrNotFound
		}
		return "", "", 0, err
	}
	var stepID string
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT id, position FROM steps WHERE job_id = ? ORDER BY position DESC LIMIT 1`, jobID).
		Scan(&stepID, &position); err != nil {
		return "", "", 0, err
	}
	return status, stepID, position, nil
}

func requireSingleContinuationUpdate(result sql.Result, err error) error {
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
