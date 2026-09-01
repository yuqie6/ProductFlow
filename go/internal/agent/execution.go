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
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// ClaimExecution 按幂等键认领一轮 Turn 的 execution lease，供 agent-service 在写 journal 前证明自己是当前 writer。
//
// 由内部路由 POST /turn-executions/claim 调用。锁顺序必须是 projection → execution，与 Heartbeat / AppendEvents / 过期回收一致。写 agent_turn_executions（owner、lease_token、lease_expires_at、attempt、fencing_token、phase）以及 projection.harness_turn_id。
//
// 同一 owner 仍持未过期 lease 时原样回放，不递增 fencing_token。任何其他成功认领都会 attempt+1 且 fencing_token+1；旧 token 的 writer 再 AppendEvents 会 Conflict。
//
// 终态 Turn、已被其他 owner 持有的有效 lease、过期后尚未对账、或已越过安全重试边界，返回 Conflict。找不到 projection 返回 NotFound。
//
// 禁区：不要为了「重试方便」在回放路径递增 fencing_token；不要先锁 execution 再锁 projection（会与 live writer 死锁）；不要在这里改 agent_tasks 或把 Goal 标完成。
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
			if proj.Status != "requires_input" || exec.Phase == "terminal" {
				return apperr.Conflict("Agent Turn execution 已过期，必须先完成副作用对账")
			}
		}
		if exec.OwnerID == nil && exec.LeaseToken == nil && exec.Phase != "claimed" && exec.Phase != "waiting_input" {
			if proj.Status != "requires_input" || exec.Phase == "terminal" {
				return apperr.Conflict("Agent Turn execution 已越过安全重试边界")
			}
		}
		lease := newID()
		expiresAt := now.Add(leaseSeconds * time.Second)
		if err := gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", exec.ID).Updates(map[string]any{
			"owner_id":                 owner,
			"lease_token":              lease,
			"lease_expires_at":         expiresAt,
			"last_heartbeat_at":        now,
			"released_at":              nil,
			"attempt":                  gorm.Expr("attempt + 1"),
			"fencing_token":            gorm.Expr("fencing_token + 1"),
			"phase":                    "claimed",
			"harness_turn_id":          turnID,
			"last_checkpoint_sequence": gorm.Expr("0"),
			"updated_at":               now,
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

// HeartbeatExecution 在持有有效 lease 时续期，并更新 execution phase（claimed / model / tool / waiting_input / external_job / terminal）。
//
// 由内部路由 heartbeat 调用。先按 projection → execution 加锁，再 requireLease 核对 owner 与 lease_token。只写 agent_turn_executions 的 phase、lease_expires_at、last_heartbeat_at。
//
// lease 过期、token 错配或 execution 不属于该 conversation 返回 Conflict / NotFound。phase 不在白名单返回 Validation。
//
// 禁区：不要在心跳里递增 fencing_token；不要把心跳当成 journal 写入；不要在这里改 Turn 投影或 Goal。
func (s Service) HeartbeatExecution(ctx context.Context, conversationID, executionID, ownerID, leaseToken, phase string) (ExecutionLeaseResponse, error) {
	if !inSet(executionPhases, phase) {
		return ExecutionLeaseResponse{}, apperr.Validation("Agent execution phase 不受支持")
	}
	var out ExecutionLeaseResponse
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		if err := lockProjectionForExecution(ctx, gdb, conversationID, executionID); err != nil {
			return err
		}
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

// ReleaseExecution 主动交还 execution lease：清空 owner / lease_token / 过期时间，并记下 released_at 与 phase。
//
// 由内部路由 release 调用。已是 terminal 且已释放过则幂等成功，不再要求 lease。其它情况必须持有有效 lease，否则 Conflict。
//
// 只写 agent_turn_executions。不写 journal，也不把 Turn 或 Goal 标终态；真正终态由 AppendEvents 的 turn/end 走 projectTerminalEvent。
//
// 禁区：不要在释放时伪造 turn/end；不要把「进程退出」当成可证明的 succeeded。
func (s Service) ReleaseExecution(ctx context.Context, conversationID, executionID, ownerID, leaseToken, phase string) (map[string]any, error) {
	if phase == "" {
		phase = "terminal"
	}
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		if err := lockProjectionForExecution(ctx, gdb, conversationID, executionID); err != nil {
			return err
		}
		var current schema.AgentTurnExecutions
		if err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", executionID).Take(&current).Error; err != nil {
			return err
		}
		if current.OwnerID == nil && current.LeaseToken == nil && current.ReleasedAt != nil && current.Phase == "terminal" {
			return nil
		}
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

// AppendCheckpoint 在有效 lease 下按连续 sequence 写入 agent_turn_checkpoints，供崩溃恢复对账 tool_effect 与模型调用。
//
// 由内部路由 checkpoints 调用。同 attempt + sequence 且 kind / fencing_token / payload 一致则回放；内容不同返回 Conflict。新行必须是 last_checkpoint_sequence+1。
//
// before_model_request 会幂等写入 agent_model_invocations，并把当前 fencing_token 钉在调用行上。tool_effect_intent 必须通过 parseToolEffectIntent（禁止密钥与图片 bytes）。
//
// 禁区：不要跳号；不要用 checkpoint 代替 journal；不要在这里改 Goal。lease 失效返回 Conflict。
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
	if kind == "tool_effect_intent" {
		if _, err := parseToolEffectIntent(payload); err != nil {
			return CheckpointResponse{}, err
		}
	}
	if kind == "tool_effect_result" {
		var doc map[string]any
		_ = json.Unmarshal(payload, &doc)
		result, _ := doc["result"].(string)
		if result != "applied" && result != "failed" && result != "unknown" {
			return CheckpointResponse{}, apperr.Validation("Agent effect result 必须是 applied、failed 或 unknown")
		}
	}
	if kind == "model_response_bound" || kind == "model_response_cursor" {
		var doc struct {
			ModelRequestID string `json:"model_request_id"`
		}
		if err := json.Unmarshal(payload, &doc); err != nil || stringsTrim(doc.ModelRequestID) == "" {
			return CheckpointResponse{}, apperr.Validation("Agent model response checkpoint 缺少 model_request_id")
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

// EventAppendInput 是准备写入 PostgreSQL agent_turn_events 的一条 journal 事件。
//
// Sequence 从 1 起连续；SchemaVersion 必须为 1。Kind 用 agent-service 的原始词表（text.chunk、turn/end 等），浏览器看到的是 sse.go 翻译后的 UI 词表。Payload 必须是 JSON object，且受大小上限约束。
type EventAppendInput struct {
	Sequence      int // 从 1 连续；已存在则幂等回放
	SchemaVersion int // 必须为 1
	RunID         string
	TurnID        string
	Kind          string          // agent-service 原始词表
	Ignorable     bool            // 未知 kind 且 false 则校验失败
	Payload       json.RawMessage // JSON object，受大小上限
	CreatedAt     time.Time
}

// AppendEvents 在有效 lease 下把一批连续事件写入 PostgreSQL agent_turn_events。该表是对话 journal 权威；Pi session files 不能证明事件顺序。
//
// 由内部路由 events/batch 调用。先锁 projection 再 requireLease。已存在的 sequence 若 schema / run / turn / kind / payload 一致则回放；不一致返回 EventSequenceConflict。新行必须紧接当前 MAX(sequence)。
//
// turn/end 必须是 batch 最后一条，并触发 projectTerminalEvent（重建 output、释放 lease、经 applyTurnState 投影；商品 Goal 保持 goal_loop）。assistant/message 收口对应的 model invocation。
//
// 成功后 NOTIFY ChannelTurn，SSE 从同一游标读。禁区：不要绕过 lease 直接 INSERT；不要在这里把 Goal 标 succeeded；过期 writer 的 fencing 由 requireLease 挡掉。
func (s Service) AppendEvents(ctx context.Context, conversationID, executionID, ownerID, leaseToken string, inputs []EventAppendInput) ([]EventReceipt, error) {
	if len(inputs) == 0 || len(inputs) > 250 {
		return nil, apperr.Validation("Agent event batch 大小无效")
	}
	for index, input := range inputs {
		if err := validateEventInput(input); err != nil {
			return nil, err
		}
		if stringsTrim(input.Kind) == "turn/end" && index != len(inputs)-1 {
			return nil, apperr.Validation("Agent turn/end 必须是 batch 最后一条事件")
		}
	}
	started := time.Now()
	defer func() { metrics.ObserveAgentEventBatch(time.Since(started)) }()
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
		sequences := make([]int, 0, len(inputs))
		for _, input := range inputs {
			sequences = append(sequences, input.Sequence)
		}
		var existingEvents []schema.AgentTurnEvents
		if err := gdb.Select("id, turn_projection_id, execution_id, run_id, turn_id, schema_version, sequence, attempt, fencing_token, kind, ignorable, payload_json, created_at").
			Where("turn_projection_id = ? AND sequence IN ?", lease.ProjectionID, sequences).
			Find(&existingEvents).Error; err != nil {
			return err
		}
		existingBySequence := make(map[int]schema.AgentTurnEvents, len(existingEvents))
		for _, existing := range existingEvents {
			existingBySequence[existing.Sequence] = existing
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

			existing, hasExisting := existingBySequence[input.Sequence]
			if hasExisting {
				if existing.SchemaVersion != input.SchemaVersion || existing.RunID != runID || existing.TurnID != turnID || existing.Kind != kind || existing.Ignorable != input.Ignorable || !sameJSON(existing.PayloadJSON, input.Payload) {
					metrics.AgentEventSequenceConflicts.Add(1)
					return apperr.ConflictCode(apperr.CodeEventSequenceConflict, "Agent event sequence 已绑定不同内容")
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
			if input.Sequence != last+1 {
				metrics.AgentEventSequenceConflicts.Add(1)
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
					metrics.AgentEventSequenceConflicts.Add(1)
					return apperr.Conflict("Agent event 与其他 writer 冲突")
				}
				return err
			}
			existingBySequence[input.Sequence] = ev
			last = input.Sequence
			out = append(out, EventReceipt{
				ID: ev.ID, ProjectionID: lease.ProjectionID, ExecutionID: executionID,
				Sequence: ev.Sequence, SchemaVersion: ev.SchemaVersion, Kind: ev.Kind,
				Ignorable: ev.Ignorable, CreatedAt: ev.CreatedAt,
			})
			if err := foldJournalProjection(gdb, lease.ProjectionID, kind, input.Payload, createdAt); err != nil {
				return err
			}
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

// foldJournalProjection 把一条新 journal event 增量投影到列表摘要；exact replay 不调用它，避免重复拼接 chunk。
func foldJournalProjection(gdb *gorm.DB, projectionID, kind string, payload json.RawMessage, eventAt time.Time) error {
	updates := map[string]any{"updated_at": gorm.Expr("GREATEST(updated_at, ?)", eventAt)}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil || doc == nil {
		return apperr.Validation("Agent journal projection payload 无效")
	}
	switch kind {
	case "turn/start":
		updates["status"] = "running"
	case "turn/cancel_requested":
		updates["status"] = "cancel_requested"
	case "question/requested":
		updates["status"] = "requires_input"
		updates["question_json"] = string(payload)
	case "text.chunk":
		if delta, _ := doc["delta"].(string); delta != "" {
			updates["output_text"] = gorm.Expr("COALESCE(output_text, '') || ?", delta)
		}
	case "thinking.chunk":
		if delta, _ := doc["delta"].(string); delta != "" {
			updates["thinking_text"] = gorm.Expr("COALESCE(thinking_text, '') || ?", delta)
		}
	case "assistant/message":
		if text, ok := doc["text"].(string); ok {
			updates["output_text"] = nullableString(text)
		}
	case "tool/call", "tool/result":
		stepID, _ := doc["step_id"].(string)
		if stringsTrim(stepID) != "" {
			var projection schema.AgentTurnProjections
			if err := gdb.Select("tool_steps_json").Where("id = ?", projectionID).Take(&projection).Error; err != nil {
				return err
			}
			var steps []map[string]any
			_ = json.Unmarshal([]byte(projection.ToolStepsJSON), &steps)
			replaced := false
			for index := range steps {
				if current, _ := steps[index]["step_id"].(string); current == stepID {
					steps[index] = doc
					replaced = true
					break
				}
			}
			if !replaced {
				steps = append(steps, doc)
			}
			encoded, err := json.Marshal(steps)
			if err != nil {
				return err
			}
			updates["tool_steps_json"] = string(encoded)
		}
	}
	if len(updates) == 1 {
		return nil
	}
	return gdb.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(updates).Error
}

// recordModelInvocationStart 在 before_model_request checkpoint 时幂等插入 agent_model_invocations。
//
// 由 AppendCheckpoint 调用。把当前 lease 的 execution_id、attempt、fencing_token 钉在 model_request_id 上。同一 ID 已存在且身份一致则回放；已绑定不同 execution / fence / 模型则 Conflict。
//
// 当前 adapter 只接受 execution_mode=foreground。禁区：不要在恢复路径用新 fence 覆盖旧调用行；不要把 Pi 日志当成 invocation 权威。
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
	if doc.ModelRequestID == "" || doc.Provider == "" || doc.Model == "" {
		return apperr.Validation("Agent model request checkpoint 缺少调用身份")
	}
	if doc.ExecutionMode != "foreground" {
		return apperr.Validation("当前 Agent adapter 不支持 background 模型调用")
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

// finishModelInvocation 在 assistant/message 入 journal 后，把对应 agent_model_invocations 从 started 收成 completed / interrupted / failed。
//
// 由 AppendEvents 调用。缺少 model_request_id 对应行是 Conflict（消息不能早于 before_model_request）。已终态行幂等忽略。全 0 usage 保持创建时的 unavailable，不能冒充 provider。
//
// provider_response_id 撞唯一约束返回 Conflict。禁区：不要用估算 usage 覆盖已有 provider 数字；不要在这里改 Turn 或 Goal。
func finishModelInvocation(gdb *gorm.DB, projectionID string, payload json.RawMessage, finishedAt time.Time) error {
	var doc struct {
		ModelRequestID     string  `json:"model_request_id"`
		Reason             string  `json:"reason"`
		Interrupted        bool    `json:"interrupted"`
		DurationMS         *int64  `json:"duration_ms"`
		ProviderResponseID *string `json:"provider_response_id"`
		ProviderCursor     *string `json:"provider_response_cursor"`
		ErrorCode          *string `json:"error_code"`
		UsageSource        *string `json:"usage_source"`
		Usage              *struct {
			Input       int64 `json:"input"`
			Output      int64 `json:"output"`
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil || stringsTrim(doc.ModelRequestID) == "" {
		return nil
	}
	requestID := stringsTrim(doc.ModelRequestID)
	var existing schema.AgentModelInvocations
	err := gdb.Where("turn_projection_id = ? AND model_request_id = ?", projectionID, requestID).Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.Conflict("Agent assistant message 缺少对应模型调用")
	}
	if err != nil {
		return err
	}
	if existing.Status != "started" {
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
	}
	if doc.DurationMS != nil && *doc.DurationMS >= 0 {
		updates["duration_ms"] = *doc.DurationMS
	} else {
		updates["duration_ms"] = gorm.Expr("GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz - started_at)) * 1000)::bigint", finishedAt)
	}
	if doc.Usage != nil {
		if doc.Usage.Input == 0 && doc.Usage.Output == 0 && doc.Usage.TotalTokens == 0 {
			// 全 0 不能冒充 provider usage；保持 invocation 创建时的 unavailable。
		} else {
			source := "provider"
			switch stringsTrim(ptrString(doc.UsageSource)) {
			case "", "provider":
				source = "provider"
			case "estimated":
				source = "estimated"
			default:
				return apperr.Validation("Agent usage_source 必须是 provider 或 estimated")
			}
			updates["input_tokens"] = doc.Usage.Input
			updates["output_tokens"] = doc.Usage.Output
			updates["total_tokens"] = doc.Usage.TotalTokens
			updates["usage_source"] = source
		}
	}
	if id := stringsTrim(ptrString(doc.ProviderResponseID)); id != "" {
		updates["provider_response_id"] = id
	}
	if cursor := stringsTrim(ptrString(doc.ProviderCursor)); cursor != "" {
		updates["provider_cursor"] = cursor
	}
	if code := boundedErrorCode(ptrString(doc.ErrorCode)); code != "" {
		updates["error_code"] = code
	}
	result := gdb.Model(&schema.AgentModelInvocations{}).
		Where("id = ? AND status = ?", existing.ID, "started").Updates(updates)
	if result.Error != nil {
		if uniqueViolation(result.Error) {
			return apperr.Conflict("Agent provider response ID 已绑定其他调用")
		}
		return result.Error
	}
	return nil
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boundedErrorCode(value string) string {
	trimmed := stringsTrim(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 80 {
		return trimmed[:80]
	}
	return trimmed
}

// projectTerminalEvent 在 journal 出现 turn/end 后，用 PG 事件重建 output_text，把投影写成终态，并释放 execution lease。
//
// 由 AppendEvents（含幂等回放 turn/end）和 recover 的 reprojectExistingTerminal 调用。写 agent_turn_projections（经 applyTurnState，另补 terminal_reason_code / output_text）和 agent_turn_executions（清空 owner/lease，phase=terminal）。
//
// fencing 交给 applyTurnState：过期 token 不能把活动状态写回去。商品工作流 Goal 在 updateTaskFromTurn 里停在 waiting_user / goal_loop，Turn 成功不自动完成 Goal。
//
// 禁区：不要用 Pi 文件重建 output；不要在这里直接 UPDATE agent_tasks 绕过 goal_loop；不要另写第二条 turn/end。
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
	row, err = loadTurnByID(ctx, gdb, row.ID)
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
	if err := gdb.WithContext(ctx).Model(&schema.AgentTurnExecutions{}).Where("id = ?", lease.ExecutionID).Updates(map[string]any{
		"owner_id":         nil,
		"lease_token":      nil,
		"lease_expires_at": nil,
		"released_at":      createdAt,
		"phase":            "terminal",
		"updated_at":       createdAt,
	}).Error; err != nil {
		return err
	}
	publishLeaseChanged(gdb, lease.ExecutionID, "terminal")
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

// parseTerminalEventPayload 校验 turn/end payload：status 必须是终态，reason 必须与 status 对齐（succeeded 对应 completed）。
//
// 由 validateEventInput 与 projectTerminalEvent 调用。reason_code 若出现，只能是 provider_failed、execution_interrupted、effect_reconciled、effect_conflict、effect_unknown、persistence_failed。
//
// 无法证明终态时不要发明 succeeded。禁区：不要放宽 reason_code 白名单来「好看一点」；unknown 必须带着可审计原因。
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

// rebuildJournalOutput 从 PostgreSQL journal 重建助手可见文本：以最近一条完整 assistant/message 为快照，再拼接其后的 text.chunk。
//
// 由 projectTerminalEvent 与 lease 过期恢复调用。只读 agent_turn_events（sequence < turn/end）。compacted 行跳过（正文已绑到 message 的 sourceEventSeqs）。payload 损坏返回 Validation，绝不静默截断。
//
// 禁区：不要读 Pi session files；不要把 thinking.chunk 并进 output_text；不要在 compact 之后假定 chunk 仍有 delta。
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

// validateEventInput 在落库前检查一条 journal 事件：schema_version=1、sequence 与 run/turn ID 有界、payload 是不超过上限的 JSON object。
//
// 由 AppendEvents 与 ConfirmEvents 调用。非 ignorable 的 kind 必须在 eventKinds；turn/end 还要能 parseTerminalEventPayload。
//
// 禁区：不要为了兼容旧 adapter 放宽 schema；不要把校验失败写成 unknown Turn。
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

// lockProjectionForExecution 先锁 agent_turn_projections，再让调用方锁 execution。这是全包统一的加锁顺序。
//
// Heartbeat、Release、AppendEvents、ConfirmEvents、requireLease、过期回收都必须先投影后 execution。反过来会与已持有投影锁、正在等 lease 行的 live writer 死锁。
//
// execution 不属于该 conversation 返回 NotFound。本函数不写表。
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

// requireLease 核对当前 writer 是否仍持有未过期的 owner + lease_token，并 FOR UPDATE 锁住 agent_turn_executions。
//
// 内部会再调 lockProjectionForExecution，因此调用方即使已锁投影也安全（同事务可重入）。owner / token 错配或 lease_expires_at 已过返回 Conflict；行不存在返回 NotFound。
//
// 返回值带当前 fencing_token / attempt，供 checkpoint 与 journal 钉身份。禁区：不要在校验失败时「顺手」续期；过期必须走 recovery，不能在这里复活 lease。
func requireLease(ctx context.Context, gdb *gorm.DB, conversationID, executionID, ownerID, leaseToken string) (ExecutionLeaseResponse, error) {
	if err := lockProjectionForExecution(ctx, gdb, conversationID, executionID); err != nil {
		return ExecutionLeaseResponse{}, err
	}
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

// ListEvents 从 PostgreSQL agent_turn_events 按 sequence 游标分页读取 journal。浏览器 SSE 与内部回放都走这里。
//
// 只读，不延长 lease，不读 Pi session files。after / limit 越界返回 Validation。游标语义是 sequence > after，断线用 Last-Event-ID 接同一位置。
//
// 禁区：不要改成读 agent-service 本地事件流；不要在列表路径投影 Goal 或改 Turn 状态。
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
