package durable

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const storeSchemaVersion = 5

type sqliteStore struct {
	db   *sql.DB
	path string
}

func openSQLiteStore(path string) (*sqliteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("durable SQLite 路径不能为空")
	}
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = filepath.Clean(absolute)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, err
		}
	}
	dsn := path + "?_txlock=immediate"
	if strings.Contains(path, "?") {
		dsn = path + "&_txlock=immediate"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &sqliteStore{db: db, path: path}
	if err := store.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *sqliteStore) initialize(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("初始化 SQLite (%s): %w", statement, err)
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > storeSchemaVersion {
		return fmt.Errorf("durable SQLite schema v%d 高于当前支持的 v%d", version, storeSchemaVersion)
	}
	if version == storeSchemaVersion {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version < 1 {
		for _, statement := range durableSchema {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("创建 durable schema v1: %w", err)
			}
		}
	}
	if version < 2 {
		for _, statement := range durableSchemaV2 {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("迁移 durable schema v2: %w", err)
			}
		}
	}
	if version < 3 {
		for _, statement := range durableSchemaV3 {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("迁移 durable schema v3: %w", err)
			}
		}
	}
	if version < 4 {
		for _, statement := range durableSchemaV4 {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("迁移 durable schema v4: %w", err)
			}
		}
	}
	if version < 5 {
		for _, statement := range durableSchemaV5 {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("迁移 durable schema v5: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", storeSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

var durableSchema = []string{
	`CREATE TABLE IF NOT EXISTS jobs (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL,
        status TEXT NOT NULL,
        error TEXT NOT NULL DEFAULT '',
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
    )`,
	`CREATE TABLE IF NOT EXISTS steps (
        id TEXT PRIMARY KEY,
        job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
        position INTEGER NOT NULL,
        tool TEXT NOT NULL,
        effect TEXT NOT NULL,
        input_json BLOB NOT NULL,
        status TEXT NOT NULL,
        result_json BLOB,
        error TEXT NOT NULL DEFAULT '',
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL,
        UNIQUE(job_id, position)
    )`,
	`CREATE TABLE IF NOT EXISTS attempts (
        id TEXT PRIMARY KEY,
        job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
        step_id TEXT NOT NULL REFERENCES steps(id) ON DELETE CASCADE,
        number INTEGER NOT NULL,
        idempotency_key TEXT NOT NULL UNIQUE,
        status TEXT NOT NULL,
        prepared_json BLOB,
        result_json BLOB,
        error TEXT NOT NULL DEFAULT '',
        lease_owner TEXT NOT NULL DEFAULT '',
        lease_expires_at INTEGER,
        started_at INTEGER,
        finished_at INTEGER,
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL,
        UNIQUE(step_id, number)
    )`,
	`CREATE TABLE IF NOT EXISTS events (
        sequence INTEGER PRIMARY KEY AUTOINCREMENT,
        schema_version INTEGER NOT NULL,
        job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
        step_id TEXT NOT NULL DEFAULT '',
        attempt_id TEXT NOT NULL DEFAULT '',
        kind TEXT NOT NULL,
        payload_json BLOB NOT NULL,
        created_at INTEGER NOT NULL
    )`,
	`CREATE INDEX IF NOT EXISTS idx_steps_job_position ON steps(job_id, position)`,
	`CREATE INDEX IF NOT EXISTS idx_attempts_step_number ON attempts(step_id, number DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_events_job_sequence ON events(job_id, sequence)`,
}

var durableSchemaV2 = []string{
	`ALTER TABLE jobs ADD COLUMN lease_owner TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE jobs ADD COLUMN lease_expires_at INTEGER`,
}

var durableSchemaV3 = []string{
	`ALTER TABLE steps ADD COLUMN await_continuation INTEGER NOT NULL DEFAULT 0`,
}

var durableSchemaV4 = []string{
	`ALTER TABLE jobs ADD COLUMN kind TEXT NOT NULL DEFAULT 'tool_sequence/v1'`,
	`CREATE INDEX IF NOT EXISTS idx_jobs_kind_status_updated ON jobs(kind, status, updated_at)`,
}

var durableSchemaV5 = []string{
	`ALTER TABLE attempts ADD COLUMN checkpoint_json BLOB`,
}

func (s *sqliteStore) close() error { return s.db.Close() }

func (s *sqliteStore) submit(ctx context.Context, spec JobSpec, tools map[string]Tool, now time.Time) (Job, error) {
	if len(spec.Steps) == 0 || len(spec.Steps) > maxStepBatch {
		return Job{}, fmt.Errorf("durable job 必须包含 1 到 %d 个步骤", maxStepBatch)
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		spec.Name = "untitled job"
	}
	if len(spec.Name) > 500 {
		return Job{}, errors.New("durable job 名称过长")
	}
	if spec.Kind == "" {
		spec.Kind = JobKindToolSequence
	}
	if !spec.Kind.valid() {
		return Job{}, fmt.Errorf("durable job kind 无效: %q", spec.Kind)
	}
	if spec.ID == "" {
		var err error
		spec.ID, err = newID("job")
		if err != nil {
			return Job{}, err
		}
	}
	if err := validateID(spec.ID); err != nil {
		return Job{}, fmt.Errorf("job ID: %w", err)
	}

	normalized, err := normalizeStepSpecs(spec.Steps, tools)
	if err != nil {
		return Job{}, err
	}

	timestamp := now.UnixNano()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO jobs(id, kind, name, status, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
		spec.ID, spec.Kind, spec.Name, JobPending, timestamp, timestamp,
	); err != nil {
		return Job{}, err
	}
	for _, step := range normalized {
		if _, err := tx.ExecContext(ctx, `INSERT INTO steps(
	            id, job_id, position, tool, effect, input_json, status, await_continuation, created_at, updated_at
	        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			step.id, spec.ID, step.index, step.tool.Name(), step.tool.Effect(), []byte(step.input), StepPending,
			step.awaitContinuation, timestamp, timestamp,
		); err != nil {
			return Job{}, err
		}
	}
	payload, _ := json.Marshal(map[string]any{"kind": spec.Kind, "name": spec.Name, "step_count": len(normalized)})
	if err := appendEvent(ctx, tx, spec.ID, "", "", "job.submitted", payload, now); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return s.getJob(ctx, spec.ID)
}

func (s *sqliteStore) getJob(ctx context.Context, id string) (Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		j.id, j.kind, j.name, j.status, j.error, j.lease_owner, j.lease_expires_at, j.created_at, j.updated_at,
		s.id, s.job_id, s.position, s.tool, s.effect, s.input_json, s.status, s.result_json,
		s.error, s.await_continuation, s.created_at, s.updated_at
		FROM jobs j JOIN steps s ON s.job_id = j.id
		WHERE j.id = ? ORDER BY s.position`, id)
	if err != nil {
		return Job{}, err
	}
	defer rows.Close()
	var job Job
	for rows.Next() {
		var leaseExpires sql.NullInt64
		var jobCreated, jobUpdated int64
		var step Step
		var input, result []byte
		var stepCreated, stepUpdated int64
		if err := rows.Scan(
			&job.ID, &job.Kind, &job.Name, &job.Status, &job.Error, &job.LeaseOwner, &leaseExpires, &jobCreated, &jobUpdated,
			&step.ID, &step.JobID, &step.Position, &step.Tool, &step.Effect, &input,
			&step.Status, &result, &step.Error, &step.AwaitContinuation, &stepCreated, &stepUpdated,
		); err != nil {
			return Job{}, err
		}
		job.CreatedAt = fromUnixNano(jobCreated)
		job.UpdatedAt = fromUnixNano(jobUpdated)
		job.LeaseExpiresAt = nullableTime(leaseExpires)
		step.Input = cloneJSON(input)
		step.Result = cloneJSON(result)
		step.CreatedAt = fromUnixNano(stepCreated)
		step.UpdatedAt = fromUnixNano(stepUpdated)
		job.Steps = append(job.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return Job{}, err
	}
	if job.ID == "" {
		return Job{}, ErrNotFound
	}
	return job, nil
}

func (s *sqliteStore) latestAttempt(ctx context.Context, stepID string) (Attempt, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
	        id, job_id, step_id, number, idempotency_key, status, prepared_json, checkpoint_json, result_json,
        error, lease_owner, lease_expires_at, started_at, finished_at, created_at, updated_at
        FROM attempts WHERE step_id = ? ORDER BY number DESC LIMIT 1`, stepID)
	attempt, err := scanAttempt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, false, nil
	}
	return attempt, err == nil, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanAttempt(row rowScanner) (Attempt, error) {
	var attempt Attempt
	var prepared, checkpoint, result []byte
	var leaseExpires, started, finished sql.NullInt64
	var created, updated int64
	err := row.Scan(&attempt.ID, &attempt.JobID, &attempt.StepID, &attempt.Number, &attempt.IdempotencyKey,
		&attempt.Status, &prepared, &checkpoint, &result, &attempt.Error, &attempt.LeaseOwner, &leaseExpires,
		&started, &finished, &created, &updated)
	if err != nil {
		return Attempt{}, err
	}
	attempt.Prepared = cloneJSON(prepared)
	attempt.Checkpoint = cloneJSON(checkpoint)
	attempt.Result = cloneJSON(result)
	attempt.LeaseExpiresAt = nullableTime(leaseExpires)
	attempt.StartedAt = nullableTime(started)
	attempt.FinishedAt = nullableTime(finished)
	attempt.CreatedAt = fromUnixNano(created)
	attempt.UpdatedAt = fromUnixNano(updated)
	return attempt, nil
}

func (s *sqliteStore) attempts(ctx context.Context, jobID string) ([]Attempt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
	        id, job_id, step_id, number, idempotency_key, status, prepared_json, checkpoint_json, result_json,
        error, lease_owner, lease_expires_at, started_at, finished_at, created_at, updated_at
        FROM attempts WHERE job_id = ? ORDER BY created_at, number`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []Attempt
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func (s *sqliteStore) pendingResolution(
	ctx context.Context,
	jobID string,
	stepStatus StepStatus,
	attemptStatus AttemptStatus,
) (ResolutionTarget, error) {
	var target ResolutionTarget
	err := s.db.QueryRowContext(ctx, `SELECT s.id, a.id
		FROM steps s
		JOIN attempts a ON a.step_id = s.id
		WHERE s.job_id = ? AND s.status = ? AND a.status = ?
		ORDER BY s.position, a.number DESC LIMIT 1`,
		jobID, stepStatus, attemptStatus,
	).Scan(&target.StepID, &target.AttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolutionTarget{}, fmt.Errorf("%w: job %s 没有匹配的待处理 attempt", ErrInvalidState, jobID)
	}
	return target, err
}

func (s *sqliteStore) createPreparedAttempt(ctx context.Context, jobID, stepID string, prepared json.RawMessage, now time.Time) (Attempt, error) {
	prepared, err := normalizeJSON(prepared)
	if err != nil {
		return Attempt{}, fmt.Errorf("prepared payload: %w", err)
	}
	attemptID, err := newID("attempt")
	if err != nil {
		return Attempt{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	var number int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM attempts WHERE step_id = ?`, stepID).Scan(&number); err != nil {
		return Attempt{}, err
	}
	key := fmt.Sprintf("%s/%s/%d", jobID, stepID, number)
	timestamp := now.UnixNano()
	if _, err := tx.ExecContext(ctx, `INSERT INTO attempts(
        id, job_id, step_id, number, idempotency_key, status, prepared_json, created_at, updated_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attemptID, jobID, stepID, number, key, AttemptPrepared, []byte(prepared), timestamp, timestamp); err != nil {
		return Attempt{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, error = '', updated_at = ? WHERE id = ?`, StepPrepared, timestamp, stepID); err != nil {
		return Attempt{}, err
	}
	payload, _ := json.Marshal(map[string]any{"number": number, "idempotency_key": key})
	if err := appendEvent(ctx, tx, jobID, stepID, attemptID, "attempt.prepared", payload, now); err != nil {
		return Attempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Attempt{}, err
	}
	return Attempt{
		ID: attemptID, JobID: jobID, StepID: stepID, Number: number, IdempotencyKey: key,
		Status: AttemptPrepared, Prepared: prepared, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *sqliteStore) recordPreparationFailure(ctx context.Context, jobID, stepID string, status AttemptStatus, message string, now time.Time) error {
	if status != AttemptFailed && status != AttemptRequiresAction {
		return fmt.Errorf("无效 preparation 终态: %s", status)
	}
	attemptID, err := newID("attempt")
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var number int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM attempts WHERE step_id = ?`, stepID).Scan(&number); err != nil {
		return err
	}
	key := fmt.Sprintf("%s/%s/%d", jobID, stepID, number)
	timestamp := now.UnixNano()
	if _, err := tx.ExecContext(ctx, `INSERT INTO attempts(
        id, job_id, step_id, number, idempotency_key, status, error, finished_at, created_at, updated_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attemptID, jobID, stepID, number, key, status, message, timestamp, timestamp, timestamp); err != nil {
		return err
	}
	stepStatus, jobStatus := StepFailed, JobFailed
	eventKind := "attempt.prepare_failed"
	if status == AttemptRequiresAction {
		stepStatus, jobStatus = StepRequiresAction, JobRequiresAction
		eventKind = "attempt.requires_action"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, error = ?, updated_at = ? WHERE id = ?`, stepStatus, message, timestamp, stepID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET
		status = ?, error = ?, lease_owner = '', lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
		jobStatus, message, timestamp, jobID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"error": message})
	if err := appendEvent(ctx, tx, jobID, stepID, attemptID, eventKind, payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) claimAttempt(ctx context.Context, attempt Attempt, owner string, leaseTTL time.Duration, now time.Time) (Attempt, error) {
	expires := now.Add(leaseTTL)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE attempts SET
        status = ?, lease_owner = ?, lease_expires_at = ?, started_at = COALESCE(started_at, ?), updated_at = ?
        WHERE id = ? AND (status = ? OR (status = ? AND lease_expires_at <= ?))`,
		AttemptRunning, owner, expires.UnixNano(), now.UnixNano(), now.UnixNano(), attempt.ID,
		AttemptPrepared, AttemptRunning, now.UnixNano())
	if err != nil {
		return Attempt{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Attempt{}, err
	}
	if changed != 1 {
		return Attempt{}, ErrLeaseHeld
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, error = '', updated_at = ? WHERE id = ?`, StepRunning, now.UnixNano(), attempt.StepID); err != nil {
		return Attempt{}, err
	}
	jobUpdate, err := tx.ExecContext(ctx, `UPDATE jobs SET status = ?, error = '', updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?`, JobRunning, now.UnixNano(), attempt.JobID, JobRunning, owner)
	if err != nil {
		return Attempt{}, err
	}
	if changed, err := jobUpdate.RowsAffected(); err != nil {
		return Attempt{}, err
	} else if changed != 1 {
		return Attempt{}, ErrLeaseHeld
	}
	kind := "attempt.started"
	if attempt.Status == AttemptRunning {
		kind = "attempt.recovered"
	}
	payload, _ := json.Marshal(map[string]any{"owner": owner, "lease_expires_at": expires})
	if err := appendEvent(ctx, tx, attempt.JobID, attempt.StepID, attempt.ID, kind, payload, now); err != nil {
		return Attempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Attempt{}, err
	}
	attempt.Status = AttemptRunning
	attempt.LeaseOwner = owner
	attempt.LeaseExpiresAt = &expires
	if attempt.StartedAt == nil {
		started := now
		attempt.StartedAt = &started
	}
	attempt.UpdatedAt = now
	return attempt, nil
}

func (s *sqliteStore) heartbeat(ctx context.Context, attemptID, owner string, leaseTTL time.Duration, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE attempts SET lease_expires_at = ?, updated_at = ?
        WHERE id = ? AND status = ? AND lease_owner = ?`,
		now.Add(leaseTTL).UnixNano(), now.UnixNano(), attemptID, AttemptRunning, owner)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrLeaseHeld
	}
	return nil
}

func (s *sqliteStore) saveAttemptCheckpoint(
	ctx context.Context,
	attempt Attempt,
	owner string,
	checkpoint json.RawMessage,
	now time.Time,
) error {
	checkpoint, err := normalizeJSON(checkpoint)
	if err != nil {
		return fmt.Errorf("attempt checkpoint: %w", err)
	}
	if len(checkpoint) > 1<<20 {
		return errors.New("attempt checkpoint 超过 1 MiB")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET checkpoint_json = ?, updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?`,
		[]byte(checkpoint), now.UnixNano(), attempt.ID, AttemptRunning, owner)
	if err != nil {
		return err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrLeaseHeld
	}
	if err := appendEvent(ctx, tx, attempt.JobID, attempt.StepID, attempt.ID, "attempt.checkpointed", checkpoint, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) completeAttempt(ctx context.Context, attempt Attempt, owner string, resultJSON json.RawMessage, now time.Time) error {
	resultJSON, err := normalizeJSON(resultJSON)
	if err != nil {
		return fmt.Errorf("tool result: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
        status = ?, result_json = ?, error = '', lease_owner = '', lease_expires_at = NULL,
        finished_at = ?, updated_at = ? WHERE id = ? AND status = ? AND lease_owner = ?`,
		AttemptSucceeded, []byte(resultJSON), now.UnixNano(), now.UnixNano(), attempt.ID, AttemptRunning, owner)
	if err != nil {
		return err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrLeaseHeld
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, result_json = ?, error = '', updated_at = ? WHERE id = ?`,
		StepSucceeded, []byte(resultJSON), now.UnixNano(), attempt.StepID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, attempt.JobID, attempt.StepID, attempt.ID, "attempt.succeeded", resultJSON, now); err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM steps WHERE job_id = ? AND status != ?`, attempt.JobID, StepSucceeded).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		var awaitContinuation bool
		if err := tx.QueryRowContext(ctx, `SELECT await_continuation FROM steps WHERE id = ?`, attempt.StepID).
			Scan(&awaitContinuation); err != nil {
			return err
		}
		jobStatus := JobSucceeded
		eventKind := "job.succeeded"
		if awaitContinuation {
			jobStatus = JobAwaitingSteps
			eventKind = "job.awaiting_steps"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET
				status = ?, error = '', lease_owner = '', lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
			jobStatus, now.UnixNano(), attempt.JobID); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, attempt.JobID, "", "", eventKind, json.RawMessage(`{}`), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) finishAttemptWithError(ctx context.Context, attempt Attempt, owner string, status AttemptStatus, message string, resultJSON json.RawMessage, now time.Time) error {
	var stepStatus StepStatus
	var jobStatus JobStatus
	var eventKind string
	switch status {
	case AttemptFailed:
		stepStatus, jobStatus, eventKind = StepFailed, JobFailed, "attempt.failed"
	case AttemptUnknown:
		stepStatus, jobStatus, eventKind = StepUnknown, JobUnknown, "attempt.unknown"
	case AttemptRequiresAction:
		stepStatus, jobStatus, eventKind = StepRequiresAction, JobRequiresAction, "attempt.requires_action"
	default:
		return fmt.Errorf("无效 attempt 终态: %s", status)
	}
	var storedResult any
	if len(resultJSON) > 0 {
		normalized, err := normalizeJSON(resultJSON)
		if err != nil {
			return fmt.Errorf("失败结果不是有效 JSON: %w", err)
		}
		storedResult = []byte(normalized)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
		status = ?, result_json = ?, error = ?, lease_owner = '', lease_expires_at = NULL, finished_at = ?, updated_at = ?
		WHERE id = ? AND status = ? AND lease_owner = ?`,
		status, storedResult, message, now.UnixNano(), now.UnixNano(), attempt.ID, AttemptRunning, owner)
	if err != nil {
		return err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrLeaseHeld
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, result_json = ?, error = ?, updated_at = ? WHERE id = ?`, stepStatus, storedResult, message, now.UnixNano(), attempt.StepID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET
		status = ?, error = ?, lease_owner = '', lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
		jobStatus, message, now.UnixNano(), attempt.JobID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"error": message})
	if err := appendEvent(ctx, tx, attempt.JobID, attempt.StepID, attempt.ID, eventKind, payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) markOpaqueUnknown(ctx context.Context, attempt Attempt, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	message := "worker 在不透明副作用执行期间失联;结果无法自动判定"
	updated, err := tx.ExecContext(ctx, `UPDATE attempts SET
        status = ?, error = ?, lease_owner = '', lease_expires_at = NULL, finished_at = ?, updated_at = ?
        WHERE id = ? AND status = ? AND lease_expires_at <= ?`,
		AttemptUnknown, message, now.UnixNano(), now.UnixNano(), attempt.ID, AttemptRunning, now.UnixNano())
	if err != nil {
		return err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrLeaseHeld
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status = ?, error = ?, updated_at = ? WHERE id = ?`, StepUnknown, message, now.UnixNano(), attempt.StepID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET
		status = ?, error = ?, lease_owner = '', lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
		JobUnknown, message, now.UnixNano(), attempt.JobID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"reason": message})
	if err := appendEvent(ctx, tx, attempt.JobID, attempt.StepID, attempt.ID, "attempt.unknown", payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) events(ctx context.Context, jobID string, afterSequence int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
        sequence, schema_version, job_id, step_id, attempt_id, kind, payload_json, created_at
        FROM events WHERE job_id = ? AND sequence > ? ORDER BY sequence`, jobID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var payload []byte
		var created int64
		if err := rows.Scan(&event.Sequence, &event.SchemaVersion, &event.JobID, &event.StepID,
			&event.AttemptID, &event.Kind, &payload, &created); err != nil {
			return nil, err
		}
		event.Payload = cloneJSON(payload)
		event.CreatedAt = fromUnixNano(created)
		events = append(events, event)
	}
	return events, rows.Err()
}

func appendEvent(ctx context.Context, tx *sql.Tx, jobID, stepID, attemptID, kind string, payload json.RawMessage, now time.Time) error {
	payload, err := normalizeJSON(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(
        schema_version, job_id, step_id, attempt_id, kind, payload_json, created_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		EventSchemaVersion, jobID, stepID, attemptID, kind, []byte(payload), now.UnixNano())
	return err
}

func newID(prefix string) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(random), nil
}

func validateID(id string) error {
	if id == "" || len(id) > 200 || strings.TrimSpace(id) != id {
		return errors.New("ID 为空、过长或包含首尾空白")
	}
	return nil
}

func fromUnixNano(value int64) time.Time { return time.Unix(0, value).UTC() }

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := fromUnixNano(value.Int64)
	return &timestamp
}
