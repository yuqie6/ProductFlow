package agenttask

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	turnprotocol "github.com/yuqie6/agent-harness/turn"
	_ "modernc.org/sqlite"
)

const publicEventReadLimit = 256

type controlStore struct{ db *sql.DB }

func openControlStore(path string) (*controlStore, error) {
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
	store := &controlStore{db: db}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *controlStore) initialize(ctx context.Context) error {
	statements := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		`CREATE TABLE IF NOT EXISTS agent_turns_v1 (
            turn_id TEXT PRIMARY KEY,
            run_id TEXT NOT NULL,
			run_sequence INTEGER NOT NULL DEFAULT 0,
            idempotency_key TEXT NOT NULL,
            input_json BLOB NOT NULL,
            input_sha256 TEXT NOT NULL,
            status TEXT NOT NULL,
            output TEXT NOT NULL DEFAULT '',
            error TEXT NOT NULL DEFAULT '',
            question_json BLOB,
            artifact_json BLOB,
            durable_sequence INTEGER NOT NULL DEFAULT 0,
            cancel_requested INTEGER NOT NULL DEFAULT 0,
			resume_required INTEGER NOT NULL DEFAULT 0,
            created_at INTEGER NOT NULL,
            updated_at INTEGER NOT NULL,
            started_at INTEGER,
            finished_at INTEGER,
            UNIQUE(run_id, idempotency_key)
        )`,
		`CREATE INDEX IF NOT EXISTS idx_agent_turns_v1_run_order
            ON agent_turns_v1(run_id, created_at, turn_id)`,
		`CREATE TABLE IF NOT EXISTS agent_turn_events_v1 (
            turn_id TEXT NOT NULL REFERENCES agent_turns_v1(turn_id) ON DELETE CASCADE,
            sequence INTEGER NOT NULL,
            schema_version INTEGER NOT NULL,
            run_id TEXT NOT NULL,
            kind TEXT NOT NULL,
            payload_json BLOB NOT NULL,
            created_at INTEGER NOT NULL,
            PRIMARY KEY(turn_id, sequence)
        )`,
		`CREATE TABLE IF NOT EXISTS agent_turn_answers_v1 (
            turn_id TEXT NOT NULL REFERENCES agent_turns_v1(turn_id) ON DELETE CASCADE,
            question_id TEXT NOT NULL,
            answer_json BLOB NOT NULL,
            step_id TEXT NOT NULL,
            attempt_id TEXT NOT NULL,
            created_at INTEGER NOT NULL,
            PRIMARY KEY(turn_id, question_id)
        )`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize agenttask control store: %w", err)
		}
	}
	if err := s.ensureRunSequence(ctx); err != nil {
		return err
	}
	return nil
}

func (s *controlStore) ensureRunSequence(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(agent_turns_v1)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var columnID, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&columnID, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "run_sequence" {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE agent_turns_v1 ADD COLUMN run_sequence INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add agenttask run sequence: %w", err)
		}
	}
	var unsequenced int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_turns_v1 WHERE run_sequence = 0`).Scan(&unsequenced); err != nil {
		return err
	}
	if unsequenced > 0 {
		if _, err := s.db.ExecContext(ctx, `WITH ranked AS (
			SELECT turn_id, ROW_NUMBER() OVER (PARTITION BY run_id ORDER BY created_at, turn_id) AS sequence
			FROM agent_turns_v1
		)
		UPDATE agent_turns_v1
		SET run_sequence = (SELECT sequence FROM ranked WHERE ranked.turn_id = agent_turns_v1.turn_id)`); err != nil {
			return fmt.Errorf("backfill agenttask run sequence: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_turns_v1_run_sequence
		ON agent_turns_v1(run_id, run_sequence)`); err != nil {
		return err
	}
	return nil
}

func inputBytes(input turnprotocol.TurnInput) ([]byte, string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:]), nil
}

func (s *controlStore) insertTurn(
	ctx context.Context,
	runID, turnID, key string,
	input turnprotocol.TurnInput,
	now time.Time,
) (turnprotocol.State, bool, error) {
	encoded, digest, err := inputBytes(input)
	if err != nil {
		return turnprotocol.State{}, false, err
	}
	if existing, found, err := s.byIdempotencyKey(ctx, runID, key); err != nil {
		return turnprotocol.State{}, false, err
	} else if found {
		if existingDigest, digestErr := stateInputDigest(existing); digestErr != nil || existingDigest != digest {
			return turnprotocol.State{}, false, fmt.Errorf("%w: run %s key %s has different input", turnprotocol.ErrIdempotencyConflict, runID, key)
		}
		return existing, true, nil
	}
	timestamp := now.UnixNano()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return turnprotocol.State{}, false, err
	}
	defer tx.Rollback()
	var runSequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(run_sequence), 0) + 1 FROM agent_turns_v1 WHERE run_id = ?`, runID).Scan(&runSequence); err != nil {
		return turnprotocol.State{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_turns_v1(
        turn_id, run_id, run_sequence, idempotency_key, input_json, input_sha256, status, created_at, updated_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		turnID, runID, runSequence, key, encoded, digest, turnprotocol.StatusQueued, timestamp, timestamp)
	if err != nil {
		_ = tx.Rollback()
		if existing, found, getErr := s.byIdempotencyKey(ctx, runID, key); getErr == nil && found {
			if existingDigest, digestErr := stateInputDigest(existing); digestErr == nil && existingDigest == digest {
				return existing, true, nil
			}
			return turnprotocol.State{}, false, fmt.Errorf("%w: run %s key %s has different input", turnprotocol.ErrIdempotencyConflict, runID, key)
		}
		return turnprotocol.State{}, false, err
	}
	if _, err := appendControlEvent(ctx, tx, runID, turnID, "turn.queued", nil, now); err != nil {
		return turnprotocol.State{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return turnprotocol.State{}, false, err
	}
	state, err := s.getTurn(ctx, runID, turnID)
	return state, false, err
}

func stateInputDigest(state turnprotocol.State) (string, error) {
	_, digest, err := inputBytes(state.Input)
	return digest, err
}

func (s *controlStore) byIdempotencyKey(ctx context.Context, runID, key string) (turnprotocol.State, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT turn_id FROM agent_turns_v1 WHERE run_id = ? AND idempotency_key = ?`, runID, key)
	var turnID string
	if err := row.Scan(&turnID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return turnprotocol.State{}, false, nil
		}
		return turnprotocol.State{}, false, err
	}
	state, err := s.getTurn(ctx, runID, turnID)
	return state, err == nil, err
}

func (s *controlStore) getTurn(ctx context.Context, runID, turnID string) (turnprotocol.State, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
        run_id, turn_id, status, input_json, question_json, artifact_json, output, error,
        created_at, updated_at, started_at, finished_at
        FROM agent_turns_v1 WHERE run_id = ? AND turn_id = ?`, runID, turnID)
	return scanTurn(row)
}

func (s *controlStore) runIDForTurn(ctx context.Context, turnID string) (string, error) {
	var runID string
	if err := s.db.QueryRowContext(ctx, `SELECT run_id FROM agent_turns_v1 WHERE turn_id = ?`, turnID).Scan(&runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", durable.ErrNotFound
		}
		return "", err
	}
	return runID, nil
}

func (s *controlStore) latestSuccessfulTurnBefore(
	ctx context.Context,
	runID, turnID string,
) (string, bool, error) {
	var previous string
	err := s.db.QueryRowContext(ctx, `SELECT candidate.turn_id
		FROM agent_turns_v1 AS candidate
		JOIN agent_turns_v1 AS current ON current.turn_id = ? AND current.run_id = ?
		WHERE candidate.run_id = current.run_id
		  AND candidate.status IN (?, ?)
		  AND candidate.run_sequence < current.run_sequence
		ORDER BY candidate.run_sequence DESC
		LIMIT 1`, turnID, runID, turnprotocol.StatusSucceeded, turnprotocol.StatusAwaitingConfirmation).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return previous, err == nil, err
}

func (s *controlStore) hasPriorArtifact(ctx context.Context, runID, turnID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM agent_turns_v1 AS candidate
		JOIN agent_turns_v1 AS current ON current.turn_id = ? AND current.run_id = ?
		WHERE candidate.run_id = current.run_id
		  AND candidate.status = ?
		  AND candidate.run_sequence < current.run_sequence`,
		turnID, runID, turnprotocol.StatusAwaitingConfirmation).Scan(&count)
	return count > 0, err
}

type rowScanner interface{ Scan(...any) error }

func scanTurn(row rowScanner) (turnprotocol.State, error) {
	var state turnprotocol.State
	var inputJSON []byte
	var questionJSON, artifactJSON []byte
	var createdAt, updatedAt int64
	var startedAt, finishedAt sql.NullInt64
	if err := row.Scan(
		&state.RunID, &state.TurnID, &state.Status, &inputJSON, &questionJSON, &artifactJSON,
		&state.Output, &state.Error, &createdAt, &updatedAt, &startedAt, &finishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return turnprotocol.State{}, durable.ErrNotFound
		}
		return turnprotocol.State{}, err
	}
	if err := json.Unmarshal(inputJSON, &state.Input); err != nil {
		return turnprotocol.State{}, err
	}
	if len(questionJSON) > 0 {
		state.Question = new(turnprotocol.Question)
		if err := json.Unmarshal(questionJSON, state.Question); err != nil {
			return turnprotocol.State{}, err
		}
	}
	if len(artifactJSON) > 0 {
		state.Artifact = new(turnprotocol.Artifact)
		if err := json.Unmarshal(artifactJSON, state.Artifact); err != nil {
			return turnprotocol.State{}, err
		}
	}
	state.APIVersion = turnprotocol.APIVersion
	state.CreatedAt = time.Unix(0, createdAt).UTC()
	state.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if startedAt.Valid {
		value := time.Unix(0, startedAt.Int64).UTC()
		state.StartedAt = &value
	}
	if finishedAt.Valid {
		value := time.Unix(0, finishedAt.Int64).UTC()
		state.FinishedAt = &value
	}
	return state, nil
}

func (s *controlStore) claimTurn(ctx context.Context, runID, turnID string, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status turnprotocol.Status
	var runSequence int64
	var canceled, resumeRequired bool
	if err := tx.QueryRowContext(ctx, `SELECT status, run_sequence, cancel_requested, resume_required FROM agent_turns_v1 WHERE run_id = ? AND turn_id = ?`, runID, turnID).
		Scan(&status, &runSequence, &canceled, &resumeRequired); err != nil {
		return false, err
	}
	if status != turnprotocol.StatusQueued {
		return false, nil
	}
	if canceled {
		return false, s.finishCanceledTx(ctx, tx, runID, turnID, now)
	}
	if resumeRequired {
		return false, nil
	}
	var blockers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_turns_v1
        WHERE run_id = ? AND turn_id != ? AND (
			status IN (?, ?, ?) OR
			(status = ? AND run_sequence < ?)
		)`, runID, turnID, turnprotocol.StatusRunning, turnprotocol.StatusRequiresInput,
		turnprotocol.StatusCancelRequested, turnprotocol.StatusQueued, runSequence).Scan(&blockers); err != nil {
		return false, err
	}
	if blockers != 0 {
		return false, nil
	}
	timestamp := now.UnixNano()
	updated, err := tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
        WHERE turn_id = ? AND status = ?`, turnprotocol.StatusRunning, timestamp, timestamp, turnID, turnprotocol.StatusQueued)
	if err != nil {
		return false, err
	}
	changed, _ := updated.RowsAffected()
	if changed != 1 {
		return false, nil
	}
	if _, err := appendControlEvent(ctx, tx, runID, turnID, "turn.started", nil, now); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *controlStore) finishCanceledTx(ctx context.Context, tx *sql.Tx, runID, turnID string, now time.Time) error {
	timestamp := now.UnixNano()
	if _, err := tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET
		status = ?, cancel_requested = 1, updated_at = ?, finished_at = ? WHERE turn_id = ?`,
		turnprotocol.StatusCanceled, timestamp, timestamp, turnID); err != nil {
		return err
	}
	if _, err := appendControlEvent(ctx, tx, runID, turnID, "turn.canceled", nil, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *controlStore) setOutcome(
	ctx context.Context, runID, turnID string, status turnprotocol.Status,
	output, message string, question *turnprotocol.Question, artifact *turnprotocol.Artifact, now time.Time,
) error {
	questionJSON, _ := json.Marshal(question)
	artifactJSON, _ := json.Marshal(artifact)
	if question == nil {
		questionJSON = nil
	}
	if artifact == nil {
		artifactJSON = nil
	}
	finished := any(nil)
	if status == turnprotocol.StatusSucceeded || status == turnprotocol.StatusFailed ||
		status == turnprotocol.StatusCanceled || status == turnprotocol.StatusUnknown ||
		status == turnprotocol.StatusAwaitingConfirmation {
		finished = now.UnixNano()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET
        status = ?, output = ?, error = ?, question_json = ?, artifact_json = ?, updated_at = ?, finished_at = ?
        WHERE run_id = ? AND turn_id = ?`, status, output, message, questionJSON, artifactJSON,
		now.UnixNano(), finished, runID, turnID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"status": status, "output": output, "error": message, "question": question, "artifact": artifact})
	kind := "turn." + string(status)
	if _, err := appendControlEvent(ctx, tx, runID, turnID, kind, payload, now); err != nil {
		return err
	}
	if question != nil {
		questionPayload, _ := json.Marshal(question)
		if _, err := appendControlEvent(ctx, tx, runID, turnID, "question.required", questionPayload, now); err != nil {
			return err
		}
	}
	if artifact != nil {
		artifactPayload, _ := json.Marshal(artifact)
		if _, err := appendControlEvent(ctx, tx, runID, turnID, "artifact.proposed", artifactPayload, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func appendControlEvent(
	ctx context.Context, tx *sql.Tx, runID, turnID, kind string, payload json.RawMessage, now time.Time,
) (turnprotocol.Event, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM agent_turn_events_v1 WHERE turn_id = ?`, turnID).Scan(&sequence); err != nil {
		return turnprotocol.Event{}, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_turn_events_v1(
        turn_id, sequence, schema_version, run_id, kind, payload_json, created_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?)`, turnID, sequence, turnprotocol.EventSchemaVersion,
		runID, kind, []byte(payload), now.UnixNano())
	if err != nil {
		return turnprotocol.Event{}, err
	}
	return turnprotocol.Event{
		SchemaVersion: turnprotocol.EventSchemaVersion, RunID: runID, TurnID: turnID,
		Sequence: sequence, CreatedAt: now.UTC(), Kind: kind, Payload: append(json.RawMessage(nil), payload...),
	}, nil
}

func (s *controlStore) appendEvent(
	ctx context.Context,
	runID, turnID, kind string,
	payload json.RawMessage,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := appendControlEvent(ctx, tx, runID, turnID, kind, payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *controlStore) events(ctx context.Context, runID, turnID string, after int64) ([]turnprotocol.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT schema_version, run_id, turn_id, sequence, created_at, kind, payload_json
        FROM agent_turn_events_v1
        WHERE run_id = ? AND turn_id = ? AND sequence > ? AND kind NOT LIKE 'journal.%'
        ORDER BY sequence LIMIT ?`, runID, turnID, after, publicEventReadLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []turnprotocol.Event
	for rows.Next() {
		var event turnprotocol.Event
		var created int64
		if err := rows.Scan(&event.SchemaVersion, &event.RunID, &event.TurnID, &event.Sequence, &created, &event.Kind, &event.Payload); err != nil {
			return nil, err
		}
		event.CreatedAt = time.Unix(0, created).UTC()
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *controlStore) durableCursor(ctx context.Context, turnID string) (int64, error) {
	var value int64
	err := s.db.QueryRowContext(ctx, `SELECT durable_sequence FROM agent_turns_v1 WHERE turn_id = ?`, turnID).Scan(&value)
	return value, err
}

func (s *controlStore) appendDurableEvent(ctx context.Context, runID, turnID string, source durable.Event, projected *turnprotocol.ToolStep) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT durable_sequence FROM agent_turns_v1 WHERE turn_id = ?`, turnID).Scan(&current); err != nil {
		return err
	}
	if source.Sequence <= current {
		return nil
	}
	if projected != nil {
		payload, err := json.Marshal(projected)
		if err != nil {
			return err
		}
		if _, err := appendControlEvent(ctx, tx, runID, turnID, turnprotocol.EventToolStep, payload, time.Now().UTC()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET durable_sequence = ? WHERE turn_id = ?`,
		source.Sequence, turnID); err != nil {
		return err
	}
	return tx.Commit()
}

type answerRecord struct {
	Answer    json.RawMessage
	StepID    string
	AttemptID string
}

func (s *controlStore) answer(ctx context.Context, turnID, questionID string) (answerRecord, bool, error) {
	var record answerRecord
	err := s.db.QueryRowContext(ctx, `SELECT answer_json, step_id, attempt_id FROM agent_turn_answers_v1
        WHERE turn_id = ? AND question_id = ?`, turnID, questionID).Scan(&record.Answer, &record.StepID, &record.AttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return answerRecord{}, false, nil
	}
	return record, err == nil, err
}

func (s *controlStore) reserveAnswer(
	ctx context.Context, turnID, questionID string, answer json.RawMessage, stepID, attemptID string, now time.Time,
) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_turn_answers_v1(
        turn_id, question_id, answer_json, step_id, attempt_id, created_at
        ) VALUES(?, ?, ?, ?, ?, ?)`, turnID, questionID, []byte(answer), stepID, attemptID, now.UnixNano())
	return err
}

func (s *controlStore) requestCancel(ctx context.Context, runID, turnID string, now time.Time) (turnprotocol.State, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return turnprotocol.State{}, false, err
	}
	defer tx.Rollback()
	var status turnprotocol.Status
	var cancelRequested bool
	if err := tx.QueryRowContext(ctx, `SELECT status, cancel_requested FROM agent_turns_v1 WHERE run_id = ? AND turn_id = ?`, runID, turnID).
		Scan(&status, &cancelRequested); err != nil {
		return turnprotocol.State{}, false, err
	}
	switch status {
	case turnprotocol.StatusQueued, turnprotocol.StatusRequiresInput:
		if err := s.finishCanceledTx(ctx, tx, runID, turnID, now); err != nil {
			return turnprotocol.State{}, false, err
		}
		state, err := s.getTurn(ctx, runID, turnID)
		return state, false, err
	case turnprotocol.StatusRunning:
		_, err = tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET status = ?, cancel_requested = 1, updated_at = ? WHERE turn_id = ?`,
			turnprotocol.StatusCancelRequested, now.UnixNano(), turnID)
		if err != nil {
			return turnprotocol.State{}, false, err
		}
		if _, err := appendControlEvent(ctx, tx, runID, turnID, "turn.cancel_requested", nil, now); err != nil {
			return turnprotocol.State{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return turnprotocol.State{}, false, err
		}
		state, err := s.getTurn(ctx, runID, turnID)
		return state, true, err
	case turnprotocol.StatusCancelRequested:
		// Cancellation is idempotent. A repeated request must not declare an
		// in-flight durable attempt canceled before the worker has observed its
		// actual outcome.
		_ = tx.Rollback()
		state, err := s.getTurn(ctx, runID, turnID)
		return state, true, err
	default:
		if cancelRequested {
			// The original cancellation already reached a terminal outcome.
			_ = tx.Rollback()
			state, err := s.getTurn(ctx, runID, turnID)
			return state, false, err
		}
		return turnprotocol.State{}, false, fmt.Errorf("%w: cannot cancel turn in %s", turnprotocol.ErrConflict, status)
	}
}

func (s *controlStore) requeueInterrupted(ctx context.Context, runID, turnID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET status = ?, resume_required = 0, updated_at = ?
		WHERE run_id = ? AND turn_id = ? AND status = ?`, turnprotocol.StatusQueued, now.UnixNano(), runID, turnID, turnprotocol.StatusRunning)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("%w: interrupted turn is no longer running", turnprotocol.ErrConflict)
	}
	if _, err := appendControlEvent(ctx, tx, runID, turnID, "turn.resume_requested", nil, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *controlStore) requestResume(ctx context.Context, runID, turnID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET resume_required = 0, updated_at = ?
		WHERE run_id = ? AND turn_id = ? AND status = ? AND resume_required = 1`,
		now.UnixNano(), runID, turnID, turnprotocol.StatusQueued)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return tx.Commit()
	}
	if _, err := appendControlEvent(ctx, tx, runID, turnID, "turn.resume_requested", nil, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *controlStore) queueAfterAnswer(
	ctx context.Context, runID, turnID, questionID string, answer json.RawMessage, now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status turnprotocol.Status
	var questionJSON []byte
	if err := tx.QueryRowContext(ctx, `SELECT status, question_json FROM agent_turns_v1
		WHERE run_id = ? AND turn_id = ?`, runID, turnID).Scan(&status, &questionJSON); err != nil {
		return err
	}
	if status != turnprotocol.StatusRequiresInput {
		return fmt.Errorf("%w: question turn is no longer requires_input", turnprotocol.ErrConflict)
	}
	var question turnprotocol.Question
	if err := json.Unmarshal(questionJSON, &question); err != nil {
		return fmt.Errorf("decode current turn question: %w", err)
	}
	if question.ID != questionID {
		return errors.Join(turnprotocol.ErrConflict, turnprotocol.ErrQuestionExpired)
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_turns_v1 SET status = ?, resume_required = 1, question_json = NULL, updated_at = ?
	        WHERE run_id = ? AND turn_id = ? AND status = ?`, turnprotocol.StatusQueued, now.UnixNano(), runID, turnID, turnprotocol.StatusRequiresInput)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("%w: question turn is no longer requires_input", turnprotocol.ErrConflict)
	}
	payload, _ := json.Marshal(map[string]any{"question_id": questionID, "answer": answer})
	if _, err := appendControlEvent(ctx, tx, runID, turnID, "question.answered", payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *controlStore) nextRunnable(ctx context.Context, runID string) (string, bool, error) {
	var turnID string
	var resumeRequired bool
	err := s.db.QueryRowContext(ctx, `SELECT candidate.turn_id, candidate.resume_required FROM agent_turns_v1 AS candidate
			WHERE candidate.run_id = ? AND candidate.status = ? AND NOT EXISTS (
				SELECT 1 FROM agent_turns_v1 AS blocker
				WHERE blocker.run_id = candidate.run_id AND blocker.status IN (?, ?, ?)
			)
			ORDER BY candidate.run_sequence LIMIT 1`, runID, turnprotocol.StatusQueued,
		turnprotocol.StatusRunning, turnprotocol.StatusRequiresInput, turnprotocol.StatusCancelRequested).Scan(&turnID, &resumeRequired)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return turnID, err == nil && !resumeRequired, err
}
