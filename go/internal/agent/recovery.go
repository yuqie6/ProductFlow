package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type RecoverySummary struct {
	PendingTurns       int
	EnqueuedTurns      int
	RecoveredTaskTurns int
	UnknownExecutions  int
}

func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, _ int) (RecoverySummary, error) {
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	return RecoverUnfinishedTurns(ctx, Service{DB: gdb})
}

func RecoverUnfinishedTurns(ctx context.Context, s Service) (RecoverySummary, error) {
	var out RecoverySummary
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		n, err := recoverExpiredExecutions(ctx, pgxTx)
		if err != nil {
			return err
		}
		out.UnknownExecutions = n
		var ids []string
		if err := pgxTx.Model(&schema.AgentTurnProjections{}).
			Where("resume_required = FALSE AND status IN ('queued','running','cancel_requested')").
			Order("created_at, id").
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		taskIDs, recovered, err := recoverQueuedTaskTurns(ctx, pgxTx)
		if err != nil {
			return err
		}
		ids = append(ids, taskIDs...)
		out.PendingTurns = len(ids)
		out.RecoveredTaskTurns = recovered
		for _, id := range ids {
			changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorAgentTurnSync, id, nil)
			if err != nil {
				return err
			}
			if changed {
				out.EnqueuedTurns++
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	_, _ = CompactExpiredTurnJournals(ctx, s, time.Now().UTC())
	return out, nil
}

func recoverQueuedTaskTurns(ctx context.Context, pgxTx *gorm.DB) ([]string, int, error) {
	type item struct {
		ID        string  `gorm:"column:id"`
		Goal      string  `gorm:"column:goal"`
		ConvID    string  `gorm:"column:conversation_id"`
		Scope     string  `gorm:"column:scope_type"`
		ProductID *string `gorm:"column:product_id"`
	}
	var tasks []item
	if err := pgxTx.Model(&schema.AgentTasks{}).
		Select("agent_tasks.id, agent_tasks.goal, agent_tasks.conversation_id, agent_conversations.scope_type, agent_conversations.product_id").
		Joins("JOIN agent_conversations ON agent_conversations.id = agent_tasks.conversation_id").
		Where("agent_tasks.status = ? AND agent_tasks.current_turn_id IS NULL AND agent_tasks.conversation_id IS NOT NULL", "queued").
		Order("agent_tasks.created_at, agent_tasks.id").
		Scan(&tasks).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	created := 0
	for _, task := range tasks {
		var productID *string
		if task.Scope == "product_workflow" {
			productID = task.ProductID
		}
		key := "initial:" + task.ConvID + ":" + task.ID
		row, wasCreated, err := reserveTurn(ctx, pgxTx, productID, task.ConvID, task.Goal, nil, key, &task.ID, "", nil)
		if err != nil {
			continue
		}
		ids = append(ids, row.ID)
		if wasCreated {
			created++
		}
	}
	return ids, created, nil
}

func recoverExpiredExecutions(ctx context.Context, pgxTx *gorm.DB) (int, error) {
	type exec struct {
		ID           string `gorm:"column:id"`
		ProjectionID string `gorm:"column:turn_projection_id"`
		Phase        string `gorm:"column:phase"`
		Status       string `gorm:"column:status"`
	}
	var items []exec
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentTurnExecutions{}).
		Select("agent_turn_executions.id, agent_turn_executions.turn_projection_id, agent_turn_executions.phase, t.status").
		Joins("JOIN agent_turn_projections t ON t.id = agent_turn_executions.turn_projection_id").
		Where("agent_turn_executions.owner_id IS NOT NULL AND agent_turn_executions.lease_expires_at IS NOT NULL AND agent_turn_executions.lease_expires_at <= NOW()").
		Clauses(pfdb.ForUpdateOfSkipLocked("agent_turn_executions")).
		Scan(&items).Error; err != nil {
		return 0, err
	}
	unknown := 0
	now := time.Now().UTC()
	for _, item := range items {
		if err := pgxTx.Model(&schema.AgentTurnExecutions{}).Where("id = ?", item.ID).Updates(map[string]any{
			"fencing_token":    gorm.Expr("fencing_token + 1"),
			"owner_id":         nil,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"released_at":      now,
		}).Error; err != nil {
			return 0, err
		}
		publishLeaseChanged(pgxTx, item.ID, "expired")
		if item.Status == "requires_input" && item.Phase == "waiting_input" {
			continue
		}
		if inSet(activeTurn, item.Status) && !(item.Status == "queued" && item.Phase == "claimed") {
			if err := appendInterruptedTurnEvents(ctx, pgxTx, item.ProjectionID, item.ID, now); err != nil {
				return 0, err
			}
			if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", item.ProjectionID).Updates(map[string]any{
				"status":               "unknown",
				"terminal_reason_code": "execution_interrupted",
				"error_text":           "Agent execution lease expired before this Turn reached a provable terminal state",
				"finished_at":          now,
				"updated_at":           now,
				"resume_required":      false,
			}).Error; err != nil {
				return 0, err
			}
			if err := pgxTx.Model(&schema.AgentModelInvocations{}).
				Where("turn_projection_id = ? AND status = ?", item.ProjectionID, "started").
				Updates(map[string]any{"status": "interrupted", "finished_at": now, "updated_at": now}).Error; err != nil {
				return 0, err
			}
			if err := pgxTx.Model(&schema.AgentTurnExecutions{}).Where("id = ?", item.ID).Updates(map[string]any{
				"phase": "terminal",
			}).Error; err != nil {
				return 0, err
			}
			unknown++
		}
	}
	return unknown, nil
}

func appendInterruptedTurnEvents(ctx context.Context, gdb *gorm.DB, projectionID, executionID string, now time.Time) error {
	row, err := loadTurnByID(ctx, gdb, projectionID)
	if err != nil {
		return err
	}
	if row.HarnessTurnID == nil {
		return nil
	}
	var existingTerminal int64
	if err := gdb.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ? AND kind = ?", projectionID, "turn/end").Count(&existingTerminal).Error; err != nil {
		return err
	}
	if existingTerminal > 0 {
		return nil
	}
	var events []schema.AgentTurnEvents
	if err := gdb.Where("turn_projection_id = ?", projectionID).Order("sequence").Find(&events).Error; err != nil {
		return err
	}
	last := 0
	text := ""
	sourceSeqs := make([]int, 0)
	hasAssistantMessage := false
	for _, event := range events {
		if event.Sequence > last {
			last = event.Sequence
		}
		if event.Kind == "assistant/message" {
			hasAssistantMessage = true
		}
		if event.Kind != "text.chunk" {
			continue
		}
		var payload struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil && payload.Delta != "" {
			text += payload.Delta
			sourceSeqs = append(sourceSeqs, event.Sequence)
		}
	}
	var exec schema.AgentTurnExecutions
	if err := gdb.Where("id = ?", executionID).Take(&exec).Error; err != nil {
		return err
	}
	appendEvent := func(kind string, payload map[string]any) error {
		last++
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		attempt, fence := exec.Attempt, exec.FencingToken
		return gdb.Create(&schema.AgentTurnEvents{
			ID: newID(), TurnProjectionID: projectionID, ExecutionID: &executionID,
			RunID: leaseHarnessRunID(row), TurnID: *row.HarnessTurnID, SchemaVersion: 1, Sequence: last,
			Attempt: &attempt, FencingToken: &fence, Kind: kind, PayloadJSON: string(raw), CreatedAt: now,
		}).Error
	}
	var checkpoints []schema.AgentTurnCheckpoints
	if err := gdb.Where("turn_projection_id = ? AND kind IN ?", projectionID, []string{"tool_effect_intent", "tool_effect_result"}).Order("sequence").Find(&checkpoints).Error; err != nil {
		return err
	}
	resolvedCalls := map[string]bool{}
	for _, checkpoint := range checkpoints {
		if checkpoint.Kind != "tool_effect_result" {
			continue
		}
		var payload struct {
			ToolCallID string `json:"tool_call_id"`
		}
		if json.Unmarshal([]byte(checkpoint.PayloadJSON), &payload) == nil && payload.ToolCallID != "" {
			resolvedCalls[payload.ToolCallID] = true
		}
	}
	for _, checkpoint := range checkpoints {
		if checkpoint.Kind != "tool_effect_intent" {
			continue
		}
		var intent struct {
			ToolName       string `json:"tool_name"`
			ToolCallID     string `json:"tool_call_id"`
			IdempotencyKey string `json:"idempotency_key"`
			RecoveryPolicy string `json:"recovery_policy"`
		}
		if json.Unmarshal([]byte(checkpoint.PayloadJSON), &intent) != nil || intent.ToolCallID == "" || resolvedCalls[intent.ToolCallID] {
			continue
		}
		policy := toolRecoveryPolicies[intent.ToolName]
		if intent.RecoveryPolicy != "" && intent.RecoveryPolicy != policy {
			continue
		}
		if policy == "" || policy == "none" {
			continue
		}
		var mutation schema.AgentToolMutations
		err := gdb.Where("conversation_id = ? AND tool_name = ? AND idempotency_key = ?", row.ConversationID, intent.ToolName, intent.IdempotencyKey).Take(&mutation).Error
		if err != nil || mutation.Status != "applied" || mutation.ResultJSON == nil {
			continue
		}
		nowResult := *mutation.ResultJSON
		reconciliation := schema.AgentTurnEffectReconciliations{
			ID: newID(), TurnProjectionID: projectionID, ToolCallID: intent.ToolCallID,
			ToolName: intent.ToolName, IdempotencyKey: intent.IdempotencyKey,
			EffectResult: "applied", ReconciliationState: "applied", ResultJSON: &nowResult,
			CreatedAt: now, UpdatedAt: now,
		}
		var existing schema.AgentTurnEffectReconciliations
		if scanErr := gdb.Where("turn_projection_id = ? AND tool_call_id = ?", projectionID, intent.ToolCallID).Take(&existing).Error; errors.Is(scanErr, gorm.ErrRecordNotFound) {
			if err := gdb.Create(&reconciliation).Error; err != nil {
				return err
			}
		} else if scanErr != nil {
			return scanErr
		}
		var result any
		_ = json.Unmarshal([]byte(nowResult), &result)
		if err := appendEvent("tool/result", map[string]any{
			"step_id": intent.ToolCallID, "tool_name": intent.ToolName, "status": "succeeded",
			"result": result, "reconciled": true,
		}); err != nil {
			return err
		}
	}
	if !hasAssistantMessage && (text != "" || len(sourceSeqs) > 0) {
		if err := appendEvent("assistant/message", map[string]any{
			"text": text, "interrupted": true, "reason": "aborted", "sourceEventSeqs": sourceSeqs,
		}); err != nil {
			return err
		}
	}
	return appendEvent("turn/end", map[string]any{
		"reason": "unknown", "reason_code": "execution_interrupted", "status": "unknown",
		"output": text, "error": "Agent execution lease expired before this Turn reached a provable terminal state",
	})
}
