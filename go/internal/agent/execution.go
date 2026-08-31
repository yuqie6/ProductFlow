package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/notify"
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
		if exec.OwnerID == nil && exec.LeaseToken == nil && exec.Phase != "claimed" && exec.Phase != "waiting_input" {
			return apperr.Conflict("Agent Turn execution 已越过安全重试边界")
		}
		lease := newID()
		expiresAt := now.Add(leaseSeconds * time.Second)
		if err := gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", exec.ID).Updates(map[string]any{
			"owner_id":          owner,
			"lease_token":       lease,
			"lease_expires_at":  expiresAt,
			"last_heartbeat_at": now,
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
		publishLeaseChanged(gdb, exec.ID, "claimed")
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
		previousPhase := lease.Phase
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
		if previousPhase != phase {
			publishLeaseChanged(gdb, executionID, phase)
		}
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
		if err := gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", executionID).Updates(map[string]any{
			"owner_id":         nil,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"released_at":      now,
			"phase":            phase,
			"updated_at":       now,
		}).Error; err != nil {
			return err
		}
		publishLeaseChanged(gdb, executionID, phase)
		return nil
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
			if existing.Kind != kind || existing.FencingToken != lease.FencingToken || !sameJSON(existing.PayloadJSON, payload) {
				return apperr.Conflict("Agent checkpoint sequence 已绑定不同内容")
			}
			out = CheckpointResponse{
				ID: existing.ID, ProjectionID: existing.TurnProjectionID, ExecutionID: existing.ExecutionID,
				Attempt: existing.Attempt, FencingToken: existing.FencingToken, Sequence: existing.Sequence,
				Kind: existing.Kind, CreatedAt: existing.CreatedAt,
			}
			if kind == "before_model_request" {
				if err := recordModelInvocationStart(gdb, lease, payload, existing.CreatedAt); err != nil {
					return err
				}
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
		if kind == "before_model_request" {
			if err := recordModelInvocationStart(gdb, lease, payload, now); err != nil {
				return err
			}
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

type EventAppendInput struct {
	Sequence      int
	SchemaVersion int
	RunID         string
	TurnID        string
	Kind          string
	Ignorable     bool
	Payload       json.RawMessage
	CreatedAt     time.Time
}

// AppendEvents persists one contiguous journal batch under one execution lease.
// Existing rows may be replayed idempotently; new rows must continue the sequence.
func (s Service) AppendEvents(ctx context.Context, conversationID, executionID, ownerID, leaseToken string, inputs []EventAppendInput) ([]EventReceipt, error) {
	if len(inputs) == 0 || len(inputs) > 250 {
		return nil, apperr.Validation("Agent event batch 大小无效")
	}
	for _, input := range inputs {
		if err := validateEventInput(input); err != nil {
			return nil, err
		}
	}
	var out []EventReceipt
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		if err := lockProjectionForExecution(ctx, gdb, conversationID, executionID); err != nil {
			return err
		}
		lease, err := requireLease(ctx, gdb, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		row, err := loadTurnByID(ctx, gdb, lease.ProjectionID)
		if err != nil {
			return err
		}
		expectedRunID := row.ConversationHarnessRunID
		if row.TaskHarnessRunID != nil && *row.TaskHarnessRunID != "" {
			expectedRunID = *row.TaskHarnessRunID
		}
		last := 0
		if err := gdb.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", lease.ProjectionID).
			Select("COALESCE(MAX(sequence), 0)").Scan(&last).Error; err != nil {
			return err
		}
		out = make([]EventReceipt, 0, len(inputs))
		for _, input := range inputs {
			runID := stringsTrim(input.RunID)
			turnID := stringsTrim(input.TurnID)
			kind := stringsTrim(input.Kind)
			if row.HarnessTurnID == nil || *row.HarnessTurnID != turnID {
				return apperr.Conflict("Agent event turn ID 与 projection 不匹配")
			}
			if expectedRunID != runID {
				return apperr.Conflict("Agent event run ID 与 Turn runtime 不匹配")
			}

			var existing schema.AgentTurnEvents
			scanErr := gdb.Where("turn_projection_id = ? AND sequence = ?", lease.ProjectionID, input.Sequence).Take(&existing).Error
			if scanErr == nil {
				if existing.SchemaVersion != input.SchemaVersion || existing.RunID != runID || existing.TurnID != turnID || existing.Kind != kind || existing.Ignorable != input.Ignorable || !sameJSON(existing.PayloadJSON, input.Payload) {
					return apperr.Conflict("Agent event sequence 已绑定不同内容")
				}
				out = append(out, EventReceipt{
					ID: existing.ID, ProjectionID: existing.TurnProjectionID, ExecutionID: executionID,
					Sequence: existing.Sequence, SchemaVersion: existing.SchemaVersion, Kind: existing.Kind,
					Ignorable: existing.Ignorable, CreatedAt: existing.CreatedAt,
				})
				if kind == "turn/end" {
					if err := s.projectTerminalEvent(ctx, gdb, row, lease, input.Sequence, input.Payload, existing.CreatedAt); err != nil {
						return err
					}
				}
				if kind == "assistant/message" {
					if err := finishModelInvocation(gdb, lease.ProjectionID, input.Payload, existing.CreatedAt); err != nil {
						return err
					}
				}
				if input.Sequence > last {
					last = input.Sequence
				}
				continue
			}
			if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
				return scanErr
			}
			if input.Sequence != last+1 {
				return apperr.Conflict("Agent event sequence 必须连续提交")
			}
			createdAt := input.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			attempt := lease.Attempt
			fencing := lease.FencingToken
			ev := schema.AgentTurnEvents{
				ID: newID(), TurnProjectionID: lease.ProjectionID, ExecutionID: &executionID,
				RunID: runID, TurnID: turnID, SchemaVersion: input.SchemaVersion, Sequence: input.Sequence,
				Attempt: &attempt, FencingToken: &fencing, Kind: kind, Ignorable: input.Ignorable,
				PayloadJSON: string(input.Payload), CreatedAt: createdAt,
			}
			if err := gdb.Create(&ev).Error; err != nil {
				if uniqueViolation(err) {
					return apperr.Conflict("Agent event 与其他 writer 冲突")
				}
				return err
			}
			last = input.Sequence
			out = append(out, EventReceipt{
				ID: ev.ID, ProjectionID: lease.ProjectionID, ExecutionID: executionID,
				Sequence: ev.Sequence, SchemaVersion: ev.SchemaVersion, Kind: ev.Kind,
				Ignorable: ev.Ignorable, CreatedAt: ev.CreatedAt,
			})
			if kind == "turn/end" {
				if err := s.projectTerminalEvent(ctx, gdb, row, lease, input.Sequence, input.Payload, createdAt); err != nil {
					return err
				}
			}
			if kind == "assistant/message" {
				if err := finishModelInvocation(gdb, lease.ProjectionID, input.Payload, createdAt); err != nil {
					return err
				}
			}
		}
		return notify.Publish(ctx, gdb, notify.ChannelTurn, lease.ProjectionID)
	})
	return out, err
}

func recordModelInvocationStart(gdb *gorm.DB, lease ExecutionLeaseResponse, payload json.RawMessage, now time.Time) error {
	var doc struct {
		ModelRequestID string `json:"model_request_id"`
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		ExecutionMode  string `json:"execution_mode"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return apperr.Validation("Agent model request checkpoint 无效")
	}
	doc.ModelRequestID, doc.Provider, doc.Model, doc.ExecutionMode = stringsTrim(doc.ModelRequestID), stringsTrim(doc.Provider), stringsTrim(doc.Model), stringsTrim(doc.ExecutionMode)
	if doc.ModelRequestID == "" || doc.Provider == "" || doc.Model == "" || (doc.ExecutionMode != "foreground" && doc.ExecutionMode != "background") {
		return apperr.Validation("Agent model request checkpoint 缺少调用身份")
	}
	row := schema.AgentModelInvocations{
		ID: newID(), TurnProjectionID: lease.ProjectionID, ExecutionID: lease.ExecutionID,
		ModelRequestID: doc.ModelRequestID, Attempt: lease.Attempt, FencingToken: lease.FencingToken,
		Provider: doc.Provider, Model: doc.Model, ExecutionMode: doc.ExecutionMode,
		Status: "started", UsageSource: "unavailable", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	var existing schema.AgentModelInvocations
	err := gdb.Where("turn_projection_id = ? AND model_request_id = ?", lease.ProjectionID, doc.ModelRequestID).Take(&existing).Error
	if err == nil {
		if existing.ExecutionID != lease.ExecutionID || existing.Attempt != lease.Attempt || existing.FencingToken != lease.FencingToken || existing.Provider != doc.Provider || existing.Model != doc.Model || existing.ExecutionMode != doc.ExecutionMode {
			return apperr.Conflict("Agent model request ID 已绑定不同调用")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return gdb.Create(&row).Error
}

func finishModelInvocation(gdb *gorm.DB, projectionID string, payload json.RawMessage, finishedAt time.Time) error {
	var doc struct {
		ModelRequestID string `json:"model_request_id"`
		Reason         string `json:"reason"`
		Interrupted    bool   `json:"interrupted"`
		Usage          *struct {
			Input       int64 `json:"input"`
			Output      int64 `json:"output"`
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil || stringsTrim(doc.ModelRequestID) == "" {
		return nil
	}
	status := "completed"
	if doc.Interrupted || doc.Reason == "aborted" {
		status = "interrupted"
	} else if doc.Reason == "error" {
		status = "failed"
	}
	updates := map[string]any{
		"status": status, "finished_at": finishedAt, "updated_at": finishedAt,
		"duration_ms": gorm.Expr("GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz - started_at)) * 1000)::bigint", finishedAt),
	}
	if doc.Usage != nil {
		updates["input_tokens"] = doc.Usage.Input
		updates["output_tokens"] = doc.Usage.Output
		updates["total_tokens"] = doc.Usage.TotalTokens
		updates["usage_source"] = "provider"
	}
	result := gdb.Model(&schema.AgentModelInvocations{}).
		Where("turn_projection_id = ? AND model_request_id = ?", projectionID, stringsTrim(doc.ModelRequestID)).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperr.Conflict("Agent assistant message 缺少对应模型调用")
	}
	return nil
}

func (s Service) projectTerminalEvent(
	ctx context.Context,
	gdb *gorm.DB,
	row turnRow,
	lease ExecutionLeaseResponse,
	terminalSequence int,
	payload json.RawMessage,
	createdAt time.Time,
) error {
	terminal, err := parseTerminalEventPayload(payload)
	if err != nil {
		return err
	}
	output, err := rebuildJournalOutput(gdb.WithContext(ctx), row.ID, terminalSequence)
	if err != nil {
		return err
	}
	var toolSteps []map[string]any
	if len(row.ToolStepsJSON) > 0 {
		_ = json.Unmarshal(row.ToolStepsJSON, &toolSteps)
	}
	attempt, fence := lease.Attempt, lease.FencingToken
	finishedAt := createdAt
	if err := s.applyTurnState(ctx, gdb, row.ConversationProductID, row.ConversationID, row.ID, TurnState{
		RunID:            leaseHarnessRunID(row),
		TurnID:           lease.HarnessTurnID,
		Status:           terminal.Status,
		ExecutionAttempt: &attempt,
		ExecutionFence:   &fence,
		Question:         terminal.Question,
		Artifact:         terminal.Artifact,
		ToolSteps:        toolSteps,
		Output:           output,
		Thinking:         stringValueOrEmpty(row.ThinkingText),
		Error:            terminal.Error,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        createdAt,
		FinishedAt:       &finishedAt,
	}); err != nil {
		return err
	}
	var reasonCode *string
	if terminal.ReasonCode != "" {
		reasonCode = &terminal.ReasonCode
	}
	return gdb.WithContext(ctx).Model(&schema.AgentTurnProjections{}).Where("id = ?", row.ID).Updates(map[string]any{
		"terminal_reason_code": reasonCode,
		"output_text":          nullableString(output),
	}).Error
}

type terminalEventPayload struct {
	Status     string          `json:"status"`
	Reason     string          `json:"reason"`
	ReasonCode string          `json:"reason_code"`
	Error      string          `json:"error"`
	Question   json.RawMessage `json:"question"`
	Artifact   *TurnArtifact   `json:"artifact"`
}

func parseTerminalEventPayload(payload json.RawMessage) (terminalEventPayload, error) {
	var terminal terminalEventPayload
	if err := json.Unmarshal(payload, &terminal); err != nil {
		return terminalEventPayload{}, apperr.Validation("Agent turn/end payload 无效")
	}
	terminal.Status = stringsTrim(terminal.Status)
	terminal.Reason = stringsTrim(terminal.Reason)
	if !inSet(terminalTurn, terminal.Status) {
		return terminalEventPayload{}, apperr.Validation("Agent turn/end status 不是终态")
	}
	expectedReason := terminal.Status
	if terminal.Status == "succeeded" {
		expectedReason = "completed"
	}
	if terminal.Reason != expectedReason {
		return terminalEventPayload{}, apperr.Validation("Agent turn/end status 与 reason 不一致")
	}
	terminal.ReasonCode = stringsTrim(terminal.ReasonCode)
	if terminal.ReasonCode != "" && !inSet(map[string]struct{}{
		"provider_failed": {}, "execution_interrupted": {}, "effect_reconciled": {},
		"effect_conflict": {}, "effect_unknown": {}, "persistence_failed": {},
	}, terminal.ReasonCode) {
		return terminalEventPayload{}, apperr.Validation("Agent turn/end reason_code 无效")
	}
	return terminal, nil
}

// rebuildJournalOutput uses the latest complete assistant message as a snapshot,
// then appends later chunks. Event payload and sequence limits bound the input;
// output is never silently truncated.
func rebuildJournalOutput(gdb *gorm.DB, projectionID string, terminalSequence int) (string, error) {
	var events []schema.AgentTurnEvents
	if err := gdb.Select("sequence", "kind", "payload_json").
		Where("turn_projection_id = ? AND sequence < ? AND kind IN ?", projectionID, terminalSequence, []string{"text.chunk", "assistant/message"}).
		Order("sequence").Find(&events).Error; err != nil {
		return "", err
	}
	var output strings.Builder
	for _, event := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil || payload == nil {
			return "", apperr.Validation("Agent assistant journal payload 无效")
		}
		if compacted, _ := payload["compacted"].(bool); compacted {
			continue
		}
		if event.Kind == "assistant/message" {
			value, present := payload["text"]
			if !present {
				continue
			}
			text, ok := value.(string)
			if !ok {
				return "", apperr.Validation("Agent assistant journal text 无效")
			}
			output.Reset()
			_, _ = output.WriteString(text)
			continue
		}
		text, ok := payload["delta"].(string)
		if !ok {
			return "", apperr.Validation("Agent text chunk 缺少 delta")
		}
		_, _ = output.WriteString(text)
	}
	return output.String(), nil
}

func leaseHarnessRunID(row turnRow) string {
	if row.TaskHarnessRunID != nil && stringsTrim(*row.TaskHarnessRunID) != "" {
		return *row.TaskHarnessRunID
	}
	return row.ConversationHarnessRunID
}

func stringValueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validateEventInput(input EventAppendInput) error {
	if input.SchemaVersion != 1 {
		return apperr.Validation("Agent event schema version 不受支持")
	}
	if input.Sequence < 1 || input.Sequence > maxEventSequence {
		return apperr.Validation("Agent event sequence 无效")
	}
	if runID := stringsTrim(input.RunID); runID == "" || len(runID) > 120 {
		return apperr.Validation("Agent event run ID 无效")
	}
	if turnID := stringsTrim(input.TurnID); turnID == "" || len(turnID) > 120 {
		return apperr.Validation("Agent event turn ID 无效")
	}
	if !inSet(eventKinds, stringsTrim(input.Kind)) && !input.Ignorable {
		return apperr.Validation("Agent event kind 不受支持")
	}
	if len(input.Payload) > maxEventPayloadBytes {
		return apperr.Validation("Agent event payload 超过大小限制")
	}
	var payloadObject map[string]any
	if err := json.Unmarshal(input.Payload, &payloadObject); err != nil || payloadObject == nil {
		return apperr.Validation("Agent event payload 必须是 JSON object")
	}
	if stringsTrim(input.Kind) == "turn/end" {
		_, err := parseTerminalEventPayload(input.Payload)
		return err
	}
	return nil
}

// lockProjectionForExecution establishes the shared projection -> execution
// lock order before requireLease takes the execution row lock.
func lockProjectionForExecution(ctx context.Context, gdb *gorm.DB, conversationID, executionID string) error {
	var projection schema.AgentTurnProjections
	err := gdb.WithContext(ctx).Model(&schema.AgentTurnProjections{}).
		Clauses(pfdb.ForUpdateOf("agent_turn_projections")).
		Joins("JOIN agent_turn_executions e ON e.turn_projection_id = agent_turn_projections.id").
		Where("e.id = ? AND agent_turn_projections.conversation_id = ?", executionID, conversationID).
		Take(&projection).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound("Agent execution 不存在")
	}
	return err
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

func (s Service) ListEvents(ctx context.Context, projectionID string, after, limit int) ([]eventRow, error) {
	if after < 0 || after > maxEventSequence {
		return nil, apperr.Validation("Agent event cursor 无效")
	}
	if limit < 1 || limit > 1000 {
		return nil, apperr.Validation("Agent event page size 无效")
	}
	var out []eventRow
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var rows []schema.AgentTurnEvents
		if err := gdb.Where("turn_projection_id = ? AND sequence > ?", projectionID, after).
			Order("sequence").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		out = make([]eventRow, 0, len(rows))
		for _, item := range rows {
			out = append(out, eventRow{
				ID: item.ID, Sequence: item.Sequence, SchemaVersion: item.SchemaVersion,
				Kind: item.Kind, Payload: []byte(item.PayloadJSON), RunID: item.RunID,
				TurnID: item.TurnID, Ignorable: item.Ignorable, CreatedAt: item.CreatedAt,
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
	Ignorable     bool
	Payload       []byte
	RunID         string
	TurnID        string
	CreatedAt     time.Time
}

func sameJSON(left string, right json.RawMessage) bool {
	var leftValue any
	if json.Unmarshal([]byte(left), &leftValue) != nil {
		return false
	}
	leftCanonical, err := canonjson.Compact(leftValue)
	if err != nil {
		return false
	}
	var rightValue any
	if json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	rightCanonical, err := canonjson.Compact(rightValue)
	return err == nil && string(leftCanonical) == string(rightCanonical)
}
