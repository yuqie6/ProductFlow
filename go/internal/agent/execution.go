package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
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
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		projQ := gdb.Clauses(pfdb.ForUpdate()).Where("conversation_id = ? AND idempotency_key = ?", conversationID, key)
		if taskID == nil {
			projQ = projQ.Where("task_id IS NULL")
		} else {
			projQ = projQ.Where("task_id = ?", *taskID)
		}
		var proj schema.AgentTurnProjections
		err := projQ.Take(&proj).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("Agent Turn projection 不存在")
		}
		if err != nil {
			return err
		}
		if proj.HarnessTurnID != nil && *proj.HarnessTurnID != turnID {
			return apperr.Conflict("Agent Turn projection 已绑定其他 harness Turn")
		}
		if inSet(terminalTurn, proj.Status) {
			return apperr.Conflict("Agent Turn 已进入终态，不能重新 claim")
		}
		var exec schema.AgentTurnExecutions
		err = gdb.Clauses(pfdb.ForUpdate()).Where("turn_projection_id = ?", proj.ID).Take(&exec).Error
		now := time.Now().UTC()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			exec = schema.AgentTurnExecutions{
				ID:                     newID(),
				TurnProjectionID:       proj.ID,
				HarnessTurnID:          turnID,
				Attempt:                0,
				FencingToken:           0,
				Phase:                  "claimed",
				LastCheckpointSequence: 0,
				CreatedAt:              now,
				UpdatedAt:              now,
			}
			if err := gdb.Create(&exec).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if exec.HarnessTurnID != turnID {
			return apperr.Conflict("Agent execution 已绑定其他 harness Turn")
		}
		if exec.OwnerID != nil && exec.LeaseToken != nil && exec.LeaseExpiresAt != nil && exec.LeaseExpiresAt.After(now) && *exec.OwnerID == owner {
			out = leaseFromExec(exec, proj.ID, owner)
			return nil
		}
		if exec.OwnerID != nil && exec.LeaseToken != nil && exec.LeaseExpiresAt != nil && exec.LeaseExpiresAt.After(now) && *exec.OwnerID != owner {
			return apperr.Conflict("Agent Turn 已被其他 Agent worker claim")
		}
		if exec.OwnerID != nil && exec.LeaseToken != nil && exec.LeaseExpiresAt != nil && !exec.LeaseExpiresAt.After(now) && exec.Phase != "claimed" && exec.Phase != "waiting_input" {
			return apperr.Conflict("Agent Turn execution 已过期，必须先完成副作用对账")
		}
		if exec.OwnerID == nil && exec.LeaseToken == nil && exec.Phase != "claimed" {
			return apperr.Conflict("Agent Turn execution 已越过安全重试边界")
		}
		lease := newID()
		expiresAt := now.Add(leaseSeconds * time.Second)
		if err := gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", exec.ID).Updates(map[string]any{
			"owner_id":          owner,
			"lease_token":       lease,
			"lease_expires_at":  expiresAt,
			"last_heartbeat_at": expiresAt,
			"released_at":       nil,
			"attempt":           gorm.Expr("attempt + 1"),
			"fencing_token":     gorm.Expr("fencing_token + 1"),
			"phase":             "claimed",
			"harness_turn_id":   turnID,
			"updated_at":        now,
		}).Error; err != nil {
			return err
		}
		if err := gdb.Where("id = ?", exec.ID).Take(&exec).Error; err != nil {
			return err
		}
		if err := gdb.Model(&schema.AgentTurnProjections{}).Where("id = ?", proj.ID).Updates(map[string]any{
			"harness_turn_id": turnID,
		}).Error; err != nil {
			return err
		}
		out = ExecutionLeaseResponse{
			ExecutionID: exec.ID, ProjectionID: proj.ID, HarnessTurnID: turnID,
			OwnerID: owner, LeaseToken: lease, Attempt: exec.Attempt, FencingToken: exec.FencingToken,
			Phase: "claimed", LeaseExpiresAt: expiresAt,
		}
		return nil
	})
	return out, err
}

func leaseFromExec(exec schema.AgentTurnExecutions, projectionID, owner string) ExecutionLeaseResponse {
	out := ExecutionLeaseResponse{
		ExecutionID: exec.ID, ProjectionID: projectionID, HarnessTurnID: exec.HarnessTurnID,
		OwnerID: owner, Attempt: exec.Attempt, FencingToken: exec.FencingToken, Phase: exec.Phase,
	}
	if exec.LeaseToken != nil {
		out.LeaseToken = *exec.LeaseToken
	}
	if exec.LeaseExpiresAt != nil {
		out.LeaseExpiresAt = *exec.LeaseExpiresAt
	}
	return out
}

func (s Service) HeartbeatExecution(ctx context.Context, conversationID, executionID, ownerID, leaseToken, phase string) (ExecutionLeaseResponse, error) {
	if !inSet(executionPhases, phase) {
		return ExecutionLeaseResponse{}, apperr.Validation("Agent execution phase 不受支持")
	}
	var out ExecutionLeaseResponse
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		lease, err := requireLease(ctx, gdb, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		expires := time.Now().UTC().Add(leaseSeconds * time.Second)
		if err := gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", executionID).Updates(map[string]any{
			"phase":             phase,
			"lease_expires_at":  expires,
			"last_heartbeat_at": time.Now().UTC(),
			"updated_at":        time.Now().UTC(),
		}).Error; err != nil {
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
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		if _, err := requireLease(ctx, gdb, conversationID, executionID, ownerID, leaseToken); err != nil {
			return err
		}
		now := time.Now().UTC()
		return gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", executionID).Updates(map[string]any{
			"owner_id":         nil,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"released_at":      now,
			"phase":            phase,
			"updated_at":       now,
		}).Error
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
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		lease, err := requireLease(ctx, gdb, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		var existing schema.AgentTurnCheckpoints
		scanErr := gdb.Where("execution_id = ? AND attempt = ? AND sequence = ?", executionID, lease.Attempt, sequence).Take(&existing).Error
		if scanErr == nil {
			if existing.Kind != kind || existing.FencingToken != lease.FencingToken {
				return apperr.Conflict("Agent checkpoint sequence 已绑定不同内容")
			}
			out = CheckpointResponse{
				ID: existing.ID, ProjectionID: existing.TurnProjectionID, ExecutionID: existing.ExecutionID,
				Attempt: existing.Attempt, FencingToken: existing.FencingToken, Sequence: existing.Sequence,
				Kind: existing.Kind, CreatedAt: existing.CreatedAt,
			}
			return nil
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return scanErr
		}
		var exec schema.AgentTurnExecutions
		if err := gdb.Where("id = ?", executionID).Take(&exec).Error; err != nil {
			return err
		}
		if sequence != exec.LastCheckpointSequence+1 {
			return apperr.Conflict("Agent checkpoint sequence 必须连续提交")
		}
		now := time.Now().UTC()
		row := schema.AgentTurnCheckpoints{
			ID:               newID(),
			TurnProjectionID: lease.ProjectionID,
			ExecutionID:      executionID,
			Attempt:          lease.Attempt,
			FencingToken:     lease.FencingToken,
			Sequence:         sequence,
			Kind:             kind,
			PayloadJSON:      string(payload),
			CreatedAt:        now,
		}
		if err := gdb.Create(&row).Error; err != nil {
			if uniqueViolation(err) {
				return apperr.Conflict("Agent checkpoint 与其他 writer 冲突")
			}
			return err
		}
		if err := gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", executionID).Updates(map[string]any{
			"last_checkpoint_sequence": sequence,
			"last_checkpoint_at":       now,
			"updated_at":               now,
		}).Error; err != nil {
			return err
		}
		out = CheckpointResponse{
			ID: row.ID, ProjectionID: lease.ProjectionID, ExecutionID: executionID,
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
	if inSet(liveOnlyEventKinds, kind) {
		return EventReceipt{}, apperr.Validation("Agent 直播事件不写入 PostgreSQL")
	}
	if !inSet(eventKinds, kind) {
		return EventReceipt{}, apperr.Validation("Agent event kind 不受支持")
	}
	if len(payload) > maxEventPayloadBytes {
		return EventReceipt{}, apperr.Validation("Agent event payload 超过大小限制")
	}
	var out EventReceipt
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		lease, err := requireLease(ctx, gdb, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		row, err := loadTurnByID(ctx, gdb, lease.ProjectionID)
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
		var existing schema.AgentTurnEvents
		scanErr := gdb.Where("turn_projection_id = ? AND sequence = ?", lease.ProjectionID, sequence).Take(&existing).Error
		if scanErr == nil {
			if existing.SchemaVersion != schemaVersion || existing.RunID != runID || existing.TurnID != turnID || existing.Kind != kind {
				return apperr.Conflict("Agent event sequence 已绑定不同内容")
			}
			out = EventReceipt{
				ID: existing.ID, ProjectionID: existing.TurnProjectionID, ExecutionID: executionID,
				Sequence: existing.Sequence, SchemaVersion: existing.SchemaVersion, Kind: existing.Kind, CreatedAt: existing.CreatedAt,
			}
			return nil
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return scanErr
		}
		var last int
		if err := gdb.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", lease.ProjectionID).
			Select("COALESCE(MAX(sequence), 0)").Scan(&last).Error; err != nil {
			return err
		}
		if sequence <= last {
			return apperr.Conflict("Agent event sequence 必须递增提交")
		}
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		attempt := lease.Attempt
		fencing := lease.FencingToken
		ev := schema.AgentTurnEvents{
			ID:               newID(),
			TurnProjectionID: lease.ProjectionID,
			ExecutionID:      &executionID,
			RunID:            runID,
			TurnID:           turnID,
			SchemaVersion:    1,
			Sequence:         sequence,
			Attempt:          &attempt,
			FencingToken:     &fencing,
			Kind:             kind,
			PayloadJSON:      string(payload),
			CreatedAt:        createdAt,
		}
		if err := gdb.Create(&ev).Error; err != nil {
			if uniqueViolation(err) {
				return apperr.Conflict("Agent event 与其他 writer 冲突")
			}
			return err
		}
		out = EventReceipt{
			ID: ev.ID, ProjectionID: lease.ProjectionID, ExecutionID: executionID,
			Sequence: sequence, SchemaVersion: 1, Kind: kind, CreatedAt: createdAt,
		}
		return nil
	})
	return out, err
}

func requireLease(ctx context.Context, gdb *gorm.DB, conversationID, executionID, ownerID, leaseToken string) (ExecutionLeaseResponse, error) {
	var exec schema.AgentTurnExecutions
	err := gdb.WithContext(ctx).Model(&schema.AgentTurnExecutions{}).
		Clauses(pfdb.ForUpdateOf("agent_turn_executions")).
		Joins("JOIN agent_turn_projections t ON t.id = agent_turn_executions.turn_projection_id").
		Where("agent_turn_executions.id = ? AND t.conversation_id = ?", executionID, conversationID).
		Take(&exec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExecutionLeaseResponse{}, apperr.NotFound("Agent execution 不存在")
	}
	if err != nil {
		return ExecutionLeaseResponse{}, err
	}
	out := ExecutionLeaseResponse{
		ExecutionID: exec.ID, ProjectionID: exec.TurnProjectionID, HarnessTurnID: exec.HarnessTurnID,
		Attempt: exec.Attempt, FencingToken: exec.FencingToken, Phase: exec.Phase,
	}
	if exec.OwnerID != nil {
		out.OwnerID = *exec.OwnerID
	}
	if exec.LeaseToken != nil {
		out.LeaseToken = *exec.LeaseToken
	}
	if exec.LeaseExpiresAt != nil {
		out.LeaseExpiresAt = *exec.LeaseExpiresAt
	}
	if out.OwnerID != stringsTrim(ownerID) || out.LeaseToken != stringsTrim(leaseToken) || exec.LeaseExpiresAt == nil || !exec.LeaseExpiresAt.After(time.Now().UTC()) {
		return ExecutionLeaseResponse{}, apperr.Conflict("Agent execution lease 已失效")
	}
	return out, nil
}

func (s Service) ListEvents(ctx context.Context, projectionID string, after int) ([]eventRow, error) {
	if after < 0 || after > maxEventSequence {
		return nil, apperr.Validation("Agent event cursor 无效")
	}
	var out []eventRow
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var rows []schema.AgentTurnEvents
		if err := gdb.Where("turn_projection_id = ? AND sequence > ?", projectionID, after).
			Order("sequence").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		out = make([]eventRow, 0, len(rows))
		for _, item := range rows {
			out = append(out, eventRow{
				ID: item.ID, Sequence: item.Sequence, SchemaVersion: item.SchemaVersion,
				Kind: item.Kind, Payload: []byte(item.PayloadJSON), RunID: item.RunID,
				TurnID: item.TurnID, CreatedAt: item.CreatedAt,
			})
		}
		return nil
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
