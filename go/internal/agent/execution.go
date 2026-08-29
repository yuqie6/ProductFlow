package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

func (s Service) ClaimExecution(ctx context.Context, conversationID string, taskID *string, idempotencyKey, harnessTurnID, ownerID string) (ExecutionLeaseResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "Agent execution idempotency key")
	if err != nil {
		return ExecutionLeaseResponse{}, err
	}
	turnID := stringsTrim(harnessTurnID)
	owner := stringsTrim(ownerID)
	if turnID == "" || len(turnID) > 120 {
		return ExecutionLeaseResponse{}, apperr.Validation("Agent execution harness turn ID 无效")
	}
	if owner == "" || len(owner) > 120 {
		return ExecutionLeaseResponse{}, apperr.Validation("Agent execution owner ID 无效")
	}
	var out ExecutionLeaseResponse
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		q := `
			SELECT id, harness_turn_id, status FROM agent_turn_projections
			WHERE conversation_id = $1 AND idempotency_key = $2
		`
		args := []any{conversationID, key}
		if taskID == nil {
			q += ` AND task_id IS NULL FOR UPDATE`
		} else {
			q += ` AND task_id = $3 FOR UPDATE`
			args = append(args, *taskID)
		}
		var projectionID string
		var existingTurn *string
		var status string
		err := pgxTx.QueryRow(ctx, q, args...).Scan(&projectionID, &existingTurn, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("Agent Turn projection 不存在")
		}
		if err != nil {
			return err
		}
		if existingTurn != nil && *existingTurn != turnID {
			return apperr.Conflict("Agent Turn projection 已绑定其他 harness Turn")
		}
		if inSet(terminalTurn, status) {
			return apperr.Conflict("Agent Turn 已进入终态，不能重新 claim")
		}
		var execID string
		var execTurn string
		var ownerCol, token *string
		var expires *time.Time
		var attempt, fencing int
		var phase string
		err = pgxTx.QueryRow(ctx, `
			SELECT id, harness_turn_id, owner_id, lease_token, lease_expires_at, attempt, fencing_token, phase
			FROM agent_turn_executions WHERE turn_projection_id = $1 FOR UPDATE
		`, projectionID).Scan(&execID, &execTurn, &ownerCol, &token, &expires, &attempt, &fencing, &phase)
		now := time.Now().UTC()
		if errors.Is(err, pgx.ErrNoRows) {
			execID = newID()
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO agent_turn_executions (
					id, turn_projection_id, harness_turn_id, attempt, fencing_token, phase, created_at, updated_at
				) VALUES ($1, $2, $3, 0, 0, 'claimed', NOW(), NOW())
			`, execID, projectionID, turnID); err != nil {
				return err
			}
			execTurn = turnID
			phase = "claimed"
		} else if err != nil {
			return err
		} else if execTurn != turnID {
			return apperr.Conflict("Agent execution 已绑定其他 harness Turn")
		}
		if ownerCol != nil && token != nil && expires != nil && expires.After(now) && *ownerCol == owner {
			out = ExecutionLeaseResponse{
				ExecutionID: execID, ProjectionID: projectionID, HarnessTurnID: execTurn,
				OwnerID: owner, LeaseToken: *token, Attempt: attempt, FencingToken: fencing,
				Phase: phase, LeaseExpiresAt: *expires,
			}
			return nil
		}
		if ownerCol != nil && token != nil && expires != nil && expires.After(now) && *ownerCol != owner {
			return apperr.Conflict("Agent Turn 已被其他 Agent worker claim")
		}
		if ownerCol != nil && token != nil && expires != nil && !expires.After(now) && phase != "claimed" {
			return apperr.Conflict("Agent Turn execution 已过期，必须先完成副作用对账")
		}
		if ownerCol == nil && token == nil && phase != "claimed" {
			return apperr.Conflict("Agent Turn execution 已越过安全重试边界")
		}
		lease := newID()
		expiresAt := now.Add(leaseSeconds * time.Second)
		if err := pgxTx.QueryRow(ctx, `
			UPDATE agent_turn_executions SET
				owner_id = $2, lease_token = $3, lease_expires_at = $4, last_heartbeat_at = $4,
				released_at = NULL, attempt = attempt + 1, fencing_token = fencing_token + 1,
				phase = 'claimed', harness_turn_id = $5, updated_at = NOW()
			WHERE id = $1
			RETURNING attempt, fencing_token
		`, execID, owner, lease, expiresAt, turnID).Scan(&attempt, &fencing); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `UPDATE agent_turn_projections SET harness_turn_id = $2 WHERE id = $1`, projectionID, turnID); err != nil {
			return err
		}
		out = ExecutionLeaseResponse{
			ExecutionID: execID, ProjectionID: projectionID, HarnessTurnID: turnID,
			OwnerID: owner, LeaseToken: lease, Attempt: attempt, FencingToken: fencing,
			Phase: "claimed", LeaseExpiresAt: expiresAt,
		}
		return nil
	})
	return out, err
}

func (s Service) HeartbeatExecution(ctx context.Context, conversationID, executionID, ownerID, leaseToken, phase string) (ExecutionLeaseResponse, error) {
	if !inSet(executionPhases, phase) {
		return ExecutionLeaseResponse{}, apperr.Validation("Agent execution phase 不受支持")
	}
	var out ExecutionLeaseResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		lease, err := requireLease(ctx, pgxTx, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		expires := time.Now().UTC().Add(leaseSeconds * time.Second)
		if _, err := pgxTx.Exec(ctx, `
			UPDATE agent_turn_executions SET phase = $2, lease_expires_at = $3, last_heartbeat_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, executionID, phase, expires); err != nil {
			return err
		}
		lease.Phase = phase
		lease.LeaseExpiresAt = expires
		out = lease
		return nil
	})
	return out, err
}

func (s Service) ReleaseExecution(ctx context.Context, conversationID, executionID, ownerID, leaseToken, phase string) (map[string]any, error) {
	if phase == "" {
		phase = "terminal"
	}
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := requireLease(ctx, pgxTx, conversationID, executionID, ownerID, leaseToken); err != nil {
			return err
		}
		_, err := pgxTx.Exec(ctx, `
			UPDATE agent_turn_executions
			SET owner_id = NULL, lease_token = NULL, lease_expires_at = NULL, released_at = NOW(), phase = $2, updated_at = NOW()
			WHERE id = $1
		`, executionID, phase)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"released": true}, nil
}

func (s Service) AppendCheckpoint(ctx context.Context, conversationID, executionID, ownerID, leaseToken string, sequence int, kind string, payload json.RawMessage) (CheckpointResponse, error) {
	if sequence < 1 || sequence > maxCheckpointSequence {
		return CheckpointResponse{}, apperr.Validation("Agent checkpoint sequence 无效")
	}
	if !inSet(checkpointKinds, kind) {
		return CheckpointResponse{}, apperr.Validation("Agent checkpoint kind 不受支持")
	}
	if len(payload) > maxCheckpointPayload {
		return CheckpointResponse{}, apperr.Validation("Agent checkpoint payload 超过大小限制")
	}
	if kind == "tool_effect_result" {
		var doc map[string]any
		_ = json.Unmarshal(payload, &doc)
		result, _ := doc["result"].(string)
		if result != "applied" && result != "failed" && result != "unknown" {
			return CheckpointResponse{}, apperr.Validation("Agent effect result 必须是 applied、failed 或 unknown")
		}
	}
	var out CheckpointResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		lease, err := requireLease(ctx, pgxTx, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		var existing CheckpointResponse
		scanErr := pgxTx.QueryRow(ctx, `
			SELECT id, turn_projection_id, execution_id, attempt, fencing_token, sequence, kind, created_at
			FROM agent_turn_checkpoints WHERE execution_id = $1 AND attempt = $2 AND sequence = $3
		`, executionID, lease.Attempt, sequence).Scan(
			&existing.ID, &existing.ProjectionID, &existing.ExecutionID, &existing.Attempt, &existing.FencingToken,
			&existing.Sequence, &existing.Kind, &existing.CreatedAt,
		)
		if scanErr == nil {
			if existing.Kind != kind || existing.FencingToken != lease.FencingToken {
				return apperr.Conflict("Agent checkpoint sequence 已绑定不同内容")
			}
			out = existing
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		var last int
		_ = pgxTx.QueryRow(ctx, `SELECT last_checkpoint_sequence FROM agent_turn_executions WHERE id = $1`, executionID).Scan(&last)
		if sequence != last+1 {
			return apperr.Conflict("Agent checkpoint sequence 必须连续提交")
		}
		id := newID()
		now := time.Now().UTC()
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO agent_turn_checkpoints (
				id, turn_projection_id, execution_id, attempt, fencing_token, sequence, kind, payload_json, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, id, lease.ProjectionID, executionID, lease.Attempt, lease.FencingToken, sequence, kind, payload, now); err != nil {
			if uniqueViolation(err) {
				return apperr.Conflict("Agent checkpoint 与其他 writer 冲突")
			}
			return err
		}
		if _, err := pgxTx.Exec(ctx, `
			UPDATE agent_turn_executions SET last_checkpoint_sequence = $2, last_checkpoint_at = $3, updated_at = NOW() WHERE id = $1
		`, executionID, sequence, now); err != nil {
			return err
		}
		out = CheckpointResponse{
			ID: id, ProjectionID: lease.ProjectionID, ExecutionID: executionID,
			Attempt: lease.Attempt, FencingToken: lease.FencingToken, Sequence: sequence, Kind: kind, CreatedAt: now,
		}
		return nil
	})
	return out, err
}

func (s Service) AppendEvent(ctx context.Context, conversationID, executionID, ownerID, leaseToken string, sequence, schemaVersion int, runID, turnID, kind string, payload json.RawMessage, createdAt time.Time) (EventReceipt, error) {
	if schemaVersion != 1 {
		return EventReceipt{}, apperr.Validation("Agent event schema version 不受支持")
	}
	if sequence < 1 || sequence > maxEventSequence {
		return EventReceipt{}, apperr.Validation("Agent event sequence 无效")
	}
	runID = stringsTrim(runID)
	turnID = stringsTrim(turnID)
	kind = stringsTrim(kind)
	if runID == "" || len(runID) > 120 {
		return EventReceipt{}, apperr.Validation("Agent event run ID 无效")
	}
	if turnID == "" || len(turnID) > 120 {
		return EventReceipt{}, apperr.Validation("Agent event turn ID 无效")
	}
	if !inSet(eventKinds, kind) {
		return EventReceipt{}, apperr.Validation("Agent event kind 不受支持")
	}
	if len(payload) > maxEventPayloadBytes {
		return EventReceipt{}, apperr.Validation("Agent event payload 超过大小限制")
	}
	var out EventReceipt
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		lease, err := requireLease(ctx, pgxTx, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		row, err := loadTurnByID(ctx, pgxTx, lease.ProjectionID)
		if err != nil {
			return err
		}
		if row.HarnessTurnID == nil || *row.HarnessTurnID != turnID {
			return apperr.Conflict("Agent event turn ID 与 projection 不匹配")
		}
		expected := row.ConversationHarnessRunID
		if row.TaskHarnessRunID != nil && *row.TaskHarnessRunID != "" {
			expected = *row.TaskHarnessRunID
		}
		if expected != runID {
			return apperr.Conflict("Agent event run ID 与 Turn runtime 不匹配")
		}
		var existing EventReceipt
		var existingRun, existingTurn string
		var existingPayload []byte
		scanErr := pgxTx.QueryRow(ctx, `
			SELECT id, turn_projection_id, execution_id, sequence, schema_version, kind, created_at, run_id, turn_id, payload_json
			FROM agent_turn_events WHERE turn_projection_id = $1 AND sequence = $2
		`, lease.ProjectionID, sequence).Scan(
			&existing.ID, &existing.ProjectionID, &existing.ExecutionID, &existing.Sequence, &existing.SchemaVersion,
			&existing.Kind, &existing.CreatedAt, &existingRun, &existingTurn, &existingPayload,
		)
		if scanErr == nil {
			if existing.SchemaVersion != schemaVersion || existingRun != runID || existingTurn != turnID || existing.Kind != kind {
				return apperr.Conflict("Agent event sequence 已绑定不同内容")
			}
			out = existing
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		var last int
		_ = pgxTx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM agent_turn_events WHERE turn_projection_id = $1`, lease.ProjectionID).Scan(&last)
		if sequence != last+1 {
			return apperr.Conflict("Agent event sequence 必须连续提交")
		}
		id := newID()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO agent_turn_events (
				id, turn_projection_id, execution_id, run_id, turn_id, schema_version, sequence,
				attempt, fencing_token, kind, payload_json, created_at
			) VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8, $9, $10, $11)
		`, id, lease.ProjectionID, executionID, runID, turnID, sequence, lease.Attempt, lease.FencingToken, kind, payload, createdAt); err != nil {
			if uniqueViolation(err) {
				return apperr.Conflict("Agent event 与其他 writer 冲突")
			}
			return err
		}
		out = EventReceipt{
			ID: id, ProjectionID: lease.ProjectionID, ExecutionID: executionID,
			Sequence: sequence, SchemaVersion: 1, Kind: kind, CreatedAt: createdAt,
		}
		return nil
	})
	return out, err
}

func requireLease(ctx context.Context, pgxTx pgx.Tx, conversationID, executionID, ownerID, leaseToken string) (ExecutionLeaseResponse, error) {
	var out ExecutionLeaseResponse
	var owner, token *string
	var expires *time.Time
	err := pgxTx.QueryRow(ctx, `
		SELECT e.id, e.turn_projection_id, e.harness_turn_id, e.owner_id, e.lease_token,
			e.attempt, e.fencing_token, e.phase, e.lease_expires_at
		FROM agent_turn_executions e
		JOIN agent_turn_projections t ON t.id = e.turn_projection_id
		WHERE e.id = $1 AND t.conversation_id = $2
		FOR UPDATE OF e
	`, executionID, conversationID).Scan(
		&out.ExecutionID, &out.ProjectionID, &out.HarnessTurnID, &owner, &token,
		&out.Attempt, &out.FencingToken, &out.Phase, &expires,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionLeaseResponse{}, apperr.NotFound("Agent execution 不存在")
	}
	if err != nil {
		return ExecutionLeaseResponse{}, err
	}
	if owner != nil {
		out.OwnerID = *owner
	}
	if token != nil {
		out.LeaseToken = *token
	}
	if expires != nil {
		out.LeaseExpiresAt = *expires
	}
	if out.OwnerID != stringsTrim(ownerID) || out.LeaseToken != stringsTrim(leaseToken) || expires == nil || !expires.After(time.Now().UTC()) {
		return ExecutionLeaseResponse{}, apperr.Conflict("Agent execution lease 已失效")
	}
	return out, nil
}

func (s Service) ListEvents(ctx context.Context, projectionID string, after int) ([]eventRow, error) {
	if after < 0 || after > maxEventSequence {
		return nil, apperr.Validation("Agent event cursor 无效")
	}
	var out []eventRow
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		rows, err := pgxTx.Query(ctx, `
			SELECT id, sequence, schema_version, kind, payload_json, run_id, turn_id, created_at
			FROM agent_turn_events WHERE turn_projection_id = $1 AND sequence > $2
			ORDER BY sequence LIMIT 100
		`, projectionID, after)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item eventRow
			if err := rows.Scan(&item.ID, &item.Sequence, &item.SchemaVersion, &item.Kind, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

type eventRow struct {
	ID            string
	Sequence      int
	SchemaVersion int
	Kind          string
	Payload       []byte
	RunID         string
	TurnID        string
	CreatedAt     time.Time
}
